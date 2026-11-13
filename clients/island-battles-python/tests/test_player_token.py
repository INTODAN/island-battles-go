"""The client sends a player token to the game's routes, and renews it."""

import asyncio
import json
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from island_battles_client import (
    GameAuthenticationError,
    IslandBattlesClient,
    turn_result,
)

LOBBY_ID = "4f7c0a2e-58c4-4a51-9d2e-3c1f0f1d2a3b"
START = datetime(2026, 11, 12, 10, 0, tzinfo=UTC)


class Server:
    """A fake backend: the lobby issues numbered tokens that last 15 minutes,
    and the game accepts only the tokens in ``valid``."""

    def __init__(self, now):
        self.now = now
        self.issued = 0
        self.valid: set[str] = set()
        # Whether the game refuses every token, as for a player it doesn't know.
        self.refuse_all = False
        self.requests: list[tuple[str, str, str]] = []

    def handle(self, request: httpx.Request) -> httpx.Response:
        authorization = request.headers["Authorization"]
        self.requests.append((request.method, request.url.path, authorization))
        if request.url.path == "/lobby/v1/player-tokens":
            if authorization != "Bearer guest":
                return httpx.Response(
                    401, json={"status": 401, "detail": "authentication required"}
                )
            assert json.loads(request.content) == {"game_key": "island-battles"}
            self.issued += 1
            token = f"token-{self.issued}"
            self.valid.add(token)
            expires = self.now() + timedelta(minutes=15)
            return httpx.Response(
                200,
                json={
                    "token": token,
                    "expires_at": expires.isoformat().replace("+00:00", "Z"),
                },
            )
        if request.url.path.startswith("/lobby/"):
            # The lobby's other routes take the guest credential only.
            status = 200 if authorization == "Bearer guest" else 401
            return httpx.Response(status, json={"path": request.url.path})
        if self.refuse_all or authorization.removeprefix("Bearer ") not in self.valid:
            return httpx.Response(
                401, json={"status": 401, "detail": "authentication required"}
            )
        return httpx.Response(200, json={"path": request.url.path})

    def game_tokens(self) -> list[str]:
        return [
            authorization
            for _, path, authorization in self.requests
            if not path.startswith("/lobby/")
        ]


def client_for(credential: str = "guest"):
    now = [START]
    server = Server(lambda: now[0])
    http = httpx.AsyncClient(
        transport=httpx.MockTransport(server.handle), base_url="http://game.test"
    )
    client = IslandBattlesClient(
        "http://game.test",
        credential,
        LOBBY_ID,
        play=lambda turn: turn_result(),
        http_client=http,
        clock=lambda: now[0],
        log_to_stdout=False,
    )
    return client, server, now


def test_game_requests_carry_a_player_token_and_lobby_requests_the_guest_credential() -> (
    None
):
    async def scenario():
        client, server, _ = client_for()
        await client._request("GET", f"/game/from-lobby/{LOBBY_ID}/generation")
        await client._request("POST", "/generation/commands", payload={})
        await client._request("POST", "/lobby/v1/connection-token", payload={})
        await client._http.aclose()
        return server

    server = asyncio.run(scenario())
    assert server.requests == [
        ("POST", "/lobby/v1/player-tokens", "Bearer guest"),
        ("GET", f"/game/from-lobby/{LOBBY_ID}/generation", "Bearer token-1"),
        ("POST", "/generation/commands", "Bearer token-1"),
        ("POST", "/lobby/v1/connection-token", "Bearer guest"),
    ]


def test_the_token_is_renewed_after_eighty_percent_of_its_life() -> None:
    async def scenario():
        client, server, now = client_for()
        path = f"/game/from-lobby/{LOBBY_ID}/generation"
        await client._request("GET", path)
        now[0] = START + timedelta(minutes=11, seconds=59)
        await client._request("GET", path)
        now[0] = START + timedelta(minutes=12)
        await client._request("GET", path)
        await client._request("GET", path)
        await client._http.aclose()
        return server

    server = asyncio.run(scenario())
    assert server.issued == 2
    assert server.game_tokens() == [
        "Bearer token-1",
        "Bearer token-1",
        "Bearer token-2",
        "Bearer token-2",
    ]


def test_a_refused_token_is_renewed_once() -> None:
    async def scenario():
        client, server, _ = client_for()
        path = f"/game/from-lobby/{LOBBY_ID}/generation"
        await client._request("GET", path)
        # The game stops accepting the token early, as after a key rotation.
        server.valid.clear()
        await client._request("GET", path)
        assert server.game_tokens() == [
            "Bearer token-1",
            "Bearer token-1",
            "Bearer token-2",
        ]
        # A new token that is refused too stops the client.
        server.refuse_all = True
        with pytest.raises(GameAuthenticationError):
            await client._request("GET", path)
        with pytest.raises(GameAuthenticationError):
            await client._request("GET", "/game/upgrades")
        await client._http.aclose()
        return server

    server = asyncio.run(scenario())
    assert server.game_tokens()[-2:] == ["Bearer token-2", "Bearer token-3"]
    assert server.issued == 3


def test_a_session_the_lobby_refuses_stops_the_client() -> None:
    async def scenario():
        client, server, _ = client_for("not-a-session")
        with pytest.raises(GameAuthenticationError) as refused:
            await client._request("GET", f"/game/from-lobby/{LOBBY_ID}/generation")
        await client._http.aclose()
        return server, refused.value

    server, error = asyncio.run(scenario())
    assert error.status == 401
    assert server.requests == [
        ("POST", "/lobby/v1/player-tokens", "Bearer not-a-session")
    ]
