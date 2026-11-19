"""``run`` stops cleanly when the player's team resigned or left a real-time
game (#347): the server refuses its batch with team-out, and the client logs
one line instead of failing."""

import asyncio
import json
import logging
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from island_battles_client import IslandBattlesClient, TeamOutError, turn_result
from island_battles_client.errors import PROBLEM_TYPE_ERRORS
from island_battles_client.fixture import fixture_turn_data

LOBBY_ID = "11111111-1111-4111-8111-111111111111"
PLAYER_ID = "22222222-2222-4222-8222-222222222222"
TEAM_OUT = {
    "type": "urn:island-battles:problem:team-out",
    "title": "Conflict",
    "status": 409,
    "detail": "your team resigned, so it is out of the game",
}


def state() -> dict:
    generation = fixture_turn_data()["generation"]
    generation["id"] = 1
    return {
        "game_id": 7,
        "lobby_id": LOBBY_ID,
        "lobby_round": 1,
        "generation_id": 1,
        "player_id": PLAYER_ID,
        "team_color": 1,
        "channel": f"player:{PLAYER_ID}",
        "submitted": False,
        "generation": generation,
        "turn_number": 1,
        "max_generations": 300,
        "log_lines_stored": 0,
        "memory": None,
        "bot": {"kind": "localhost", "port": 41414},
        "relayed_bots": [],
        "departures": [],
        "turn_deadline_at": None,
        "turn_pace_milliseconds": 1000,
    }


class Server:
    def __init__(self) -> None:
        self.batches: list[dict] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path
        if path == "/lobby/v1/player-tokens":
            expires = datetime.now(UTC) + timedelta(minutes=15)
            return httpx.Response(
                200, json={"token": "pt", "expires_at": expires.isoformat()}
            )
        if path == "/lobby/v1/connection-token":
            expires = datetime.now(UTC) + timedelta(hours=1)
            return httpx.Response(
                200,
                json={
                    "token": "t",
                    "expires_at": expires.isoformat(),
                    "websocket_url": "ws://127.0.0.1:1/connection/websocket",
                },
            )
        if path == "/game/upgrades":
            return httpx.Response(
                200, json={"upgrades": fixture_turn_data()["upgrades"]}
            )
        if path.endswith("/generation"):
            return httpx.Response(200, json=state())
        if path == "/generation/commands":
            self.batches.append(json.loads(request.content))
            return httpx.Response(409, json=TEAM_OUT)
        return httpx.Response(404, json={"detail": "no route"})


class FakeSubscription:
    def __init__(self, events):
        self.events = events

    async def subscribe(self):
        await self.events.on_subscribed(None)

    async def ready(self):
        return None

    async def unsubscribe(self):
        return None


class FakeSDK:
    def __init__(self, address, token=None, get_token=None, events=None):
        pass

    async def connect(self):
        return None

    def new_subscription(self, channel, events=None, **_):
        return FakeSubscription(events)

    def remove_subscription(self, subscription):
        return None

    async def disconnect(self):
        return None


def test_run_stops_cleanly_when_the_team_is_out(
    caplog: pytest.LogCaptureFixture,
) -> None:
    server = Server()

    async def scenario() -> None:
        client = IslandBattlesClient(
            "guest",
            LOBBY_ID,
            play=lambda turn: turn_result([]),
            base_url="http://game.test",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(server.handle)),
            sdk_factory=FakeSDK,
            log_to_stdout=False,
        )
        await asyncio.wait_for(client.run(), 5)

    with caplog.at_level(logging.INFO):
        asyncio.run(scenario())
    # One completing batch, and no failure-log batch after it.
    assert [
        (batch["generation_id"], batch["complete"]) for batch in server.batches
    ] == [(1, True)]
    lines = [
        record.getMessage()
        for record in caplog.records
        if "team is out" in record.getMessage()
    ]
    assert len(lines) == 1 and "your team resigned" in lines[0]
    assert PROBLEM_TYPE_ERRORS["urn:island-battles:problem:team-out"] is TeamOutError
