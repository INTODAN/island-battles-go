"""``run`` in a real-time game passes a turn that ended while the bot played,
and in a game that waits for every player stops on it as before."""

import asyncio
import json
import logging
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from island_battles_client import (
    IslandBattlesClient,
    StaleGenerationError,
    turn_result,
)
from island_battles_client.fixture import fixture_turn_data

LOBBY_ID = "11111111-1111-4111-8111-111111111111"
PLAYER_ID = "22222222-2222-4222-8222-222222222222"
STALE = {
    "type": "urn:island-battles:problem:stale-generation",
    "title": "Conflict",
    "status": 409,
    "detail": "generation is no longer current: in this real-time game each turn "
    "ends 1 s after the previous one",
}
OUTCOME = {
    "reason": "last_team_standing",
    "summary": "Red won.",
    "winner_team_color": 1,
}


def state(generation_id: int, pace: int | None, **fields: object) -> dict:
    generation = fixture_turn_data()["generation"]
    generation["id"] = generation_id
    return {
        "game_id": 7,
        "lobby_id": LOBBY_ID,
        "lobby_round": 1,
        "generation_id": generation_id,
        "player_id": PLAYER_ID,
        "team_color": 1,
        "channel": f"player:{PLAYER_ID}",
        "submitted": False,
        "generation": generation,
        "turn_number": generation_id,
        "max_generations": 300,
        "log_lines_stored": 0,
        "memory": None,
        "bot": {"kind": "localhost", "port": 41414},
        "relayed_bots": [],
        "rated": False,
        "departures": [],
        "turn_deadline_at": None,
        "turn_pace_milliseconds": pace,
        **fields,
    }


class Server:
    """A game whose first turn ends while the bot plays it: the bot's
    completing batch for generation 1 is the request that plays the turn, so
    it is refused as stale. The batch for generation 2 ends the game."""

    def __init__(self, pace: int | None):
        self.pace = pace
        self.current = 1
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
            return httpx.Response(200, json=state(self.current, self.pace))
        if path == "/generation/commands":
            batch = json.loads(request.content)
            self.batches.append(batch)
            if batch["generation_id"] < self.current or batch["generation_id"] == 1:
                self.current = 2
                return httpx.Response(409, json=STALE)
            return httpx.Response(
                200,
                json=state(self.current, self.pace, submitted=True, outcome=OUTCOME),
            )
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


def run_game(pace: int | None) -> tuple[Server, list[int], BaseException | None]:
    server = Server(pace)
    played: list[int] = []

    async def play(turn):
        played.append(turn.generation.id)
        return turn_result([])

    async def scenario() -> BaseException | None:
        client = IslandBattlesClient(
            "guest",
            LOBBY_ID,
            play=play,
            base_url="http://game.test",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(server.handle)),
            sdk_factory=FakeSDK,
            log_to_stdout=False,
        )
        try:
            await asyncio.wait_for(client.run(), 5)
        except StaleGenerationError as error:
            return error
        return None

    return server, played, asyncio.run(scenario())


def test_a_real_time_game_passes_a_turn_that_ended_while_the_bot_played(
    caplog: pytest.LogCaptureFixture,
) -> None:
    with caplog.at_level(logging.WARNING):
        server, played, error = run_game(1000)
    assert error is None
    assert played == [1, 2]
    # No failure-log batch for the passed turn: the next turn's answer follows.
    assert [
        (batch["generation_id"], batch["complete"]) for batch in server.batches
    ] == [
        (1, True),
        (2, True),
    ]
    passes = [
        record.getMessage()
        for record in caplog.records
        if "slower than the turn pace" in record.getMessage()
    ]
    assert passes == [
        (
            "the bot was slower than the turn pace of 1 s on generation 1, so "
            "the turn was played without its answer and it passed this turn"
        )
    ]


def test_a_game_that_waits_stops_on_a_stale_batch_as_before() -> None:
    server, played, error = run_game(None)
    assert isinstance(error, StaleGenerationError)
    assert error.status == 409
    assert played == [1]
    # The completing batch, then the failure-log batch, as for any rejection.
    assert [
        (batch["generation_id"], batch["complete"]) for batch in server.batches
    ] == [
        (1, True),
        (1, False),
    ]


class LateServer(Server):
    """A game whose first turn ends while the bot plays it, and whose server
    plays the bot's late answer in the open turn, generation 2 (#380): the
    answer completes it, so the bot has nothing more to play there."""

    def handle(self, request: httpx.Request) -> httpx.Response:
        if request.url.path == "/generation/commands":
            batch = json.loads(request.content)
            self.batches.append(batch)
            self.current = 2
            return httpx.Response(
                200, json=state(self.current, self.pace, submitted=True)
            )
        return super().handle(request)


def test_a_late_answer_that_joins_the_open_turn_completes_it() -> None:
    server = LateServer(1000)
    played: list[int] = []

    async def play(turn):
        played.append(turn.generation.id)
        return turn_result([])

    async def scenario() -> None:
        client = IslandBattlesClient(
            "guest",
            LOBBY_ID,
            play=play,
            base_url="http://game.test",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(server.handle)),
            sdk_factory=FakeSDK,
            log_to_stdout=False,
        )
        running = asyncio.create_task(client.run())
        await asyncio.sleep(0.5)
        await client.close()
        await asyncio.gather(running, return_exceptions=True)

    asyncio.run(scenario())
    # The answer for generation 1 is the bot's answer for generation 2, which
    # it doesn't play again.
    assert played == [1]
    assert [batch["generation_id"] for batch in server.batches] == [1]
