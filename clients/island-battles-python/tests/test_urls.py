"""The lobby and the game are separate sites (#228): the client sends each
route to its own, and run reads both addresses."""

import argparse
import asyncio
from datetime import UTC, datetime, timedelta

import httpx
import pytest

from island_battles_client import IslandBattlesClient, turn_result
from island_battles_client.cli import build_parser, seat_urls

LOBBY_ID = "4f7c0a2e-58c4-4a51-9d2e-3c1f0f1d2a3b"


def recording_client(**urls: str) -> tuple[IslandBattlesClient, list[str]]:
    sent: list[str] = []

    def handle(request: httpx.Request) -> httpx.Response:
        sent.append(f"{request.url.scheme}://{request.url.netloc.decode()}{request.url.path}")
        if request.url.path == "/lobby/v1/player-tokens":
            expires = datetime.now(UTC) + timedelta(minutes=15)
            return httpx.Response(
                200,
                json={"token": "token", "expires_at": expires.isoformat().replace("+00:00", "Z")},
            )
        return httpx.Response(200, json={})

    client = IslandBattlesClient(
        "guest",
        LOBBY_ID,
        play=lambda turn: turn_result(),
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(handle)),
        log_to_stdout=False,
        **urls,
    )
    return client, sent


def test_lobby_routes_go_to_the_lobby_and_game_routes_to_the_game() -> None:
    async def scenario():
        client, sent = recording_client(
            game_url="https://game.example.org/", lobby_url="https://lobby.example.org"
        )
        await client._request("GET", f"/game/from-lobby/{LOBBY_ID}/generation")
        await client._request("POST", "/generation/commands", payload={})
        await client._request("POST", "/lobby/v1/connection-token", payload={})
        await client._http.aclose()
        return sent

    assert asyncio.run(scenario()) == [
        "https://lobby.example.org/lobby/v1/player-tokens",
        f"https://game.example.org/game/from-lobby/{LOBBY_ID}/generation",
        "https://game.example.org/generation/commands",
        "https://lobby.example.org/lobby/v1/connection-token",
    ]


def test_base_url_is_the_shorthand_for_both() -> None:
    client, _ = recording_client(base_url="http://localhost:8888/")
    assert (client.game_url, client.lobby_url) == ("http://localhost:8888", "http://localhost:8888")


@pytest.mark.parametrize(
    "urls",
    [
        {},
        {"game_url": "https://game.example.org"},
        {"lobby_url": "https://lobby.example.org"},
        {"base_url": "http://localhost:8888", "game_url": "https://game.example.org"},
    ],
)
def test_the_client_needs_both_urls(urls: dict[str, str]) -> None:
    with pytest.raises(ValueError):
        recording_client(**urls)


def run_args(*arguments: str) -> argparse.Namespace:
    return build_parser().parse_args(["run", "bot:play", "--token=guest", "--lobby-id", LOBBY_ID, *arguments])


def test_run_reads_both_addresses_from_flags_the_shorthand_or_the_environment(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("ISLAND_BATTLES_GAME_URL", raising=False)
    monkeypatch.delenv("ISLAND_BATTLES_LOBBY_URL", raising=False)
    game, lobby = "https://game.example.org", "https://lobby.example.org"
    assert seat_urls(run_args("--game-url", game, "--lobby-url", lobby)) == (game, lobby)
    assert seat_urls(run_args("--base-url", "http://localhost:8888")) == ("http://localhost:8888",) * 2
    for arguments in (["--game-url", game], ["--lobby-url", lobby], ["--base-url", game, "--lobby-url", lobby]):
        with pytest.raises(SystemExit):
            seat_urls(run_args(*arguments))
    monkeypatch.setenv("ISLAND_BATTLES_GAME_URL", game)
    monkeypatch.setenv("ISLAND_BATTLES_LOBBY_URL", "https://other.example.org")
    assert seat_urls(run_args()) == (game, "https://other.example.org")
    # A flag wins over the environment.
    assert seat_urls(run_args("--lobby-url", lobby)) == (game, lobby)
