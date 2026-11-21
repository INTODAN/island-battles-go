"""The end of a game (#413): a bot's ``game_ended`` function under the worker,
``serve``'s ``POST /game-ended`` and ``run``, and ``download_demo``."""

import asyncio
import gzip
import json
import logging
import os
import subprocess
import sys
import threading
import time
from collections.abc import Iterator
from datetime import UTC, datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import httpx
import pytest

from island_battles_client import (
    GameEndedDelivery,
    GameError,
    IslandBattlesClient,
    download_demo,
    turn_result,
)
from island_battles_client.cli import BotServer, ReloadingPlay, demo_refusal
from island_battles_client.fixture import fixture_turn_data

LOBBY_ID = "11111111-1111-4111-8111-111111111111"
PLAYER_ID = "22222222-2222-4222-8222-222222222222"
OUTCOME = {
    "reason": "last_team_standing",
    "winner_team_color": 1,
    "summary": "Red won: the last team standing",
    "departures": [],
}
GAME_ORIGIN = "https://game.example.org"


def game_ended_data(
    demo_url: str = "http://127.0.0.1:8888/game/7/demo?token=ibdt1.x",
) -> dict:
    """A GameEndedDelivery of the fixture board."""
    return {
        "game_id": 7,
        "team_color": 1,
        "turn_number": 91,
        "outcome": OUTCOME,
        "generation": fixture_turn_data()["generation"],
        "memory": {"sent": 4},
        "demo": {
            "url": demo_url,
            "expires_at": (datetime.now(UTC) + timedelta(days=7)).isoformat(),
        },
    }


# The worker ---------------------------------------------------------------------

BOT = """
import json, os
from island_battles_client import turn_result

def play(turn):
    return turn_result()

def game_ended(delivery):
    print("printing at the end must not reach the protocol")
    with open(os.environ["ENDED_FILE"], "a") as file:
        file.write(json.dumps([delivery.game_id, delivery.outcome.reason]) + "\\n")
"""
FAILING_BOT = """
from island_battles_client import turn_result

def play(turn):
    return turn_result()

async def game_ended(delivery):
    raise RuntimeError("no demo for me")
"""
PLAIN_BOT = """
from island_battles_client import turn_result

game_ended = True  # a flag, not a game-end function

def play(turn):
    return turn_result()
"""


def worker_exchange(
    directory: Path, source: str, lines: list[str]
) -> tuple[list[dict], str]:
    """Start a worker for ``mybot:play``, write the lines, and return the answers
    after the ready line, and stderr."""
    (directory / "mybot.py").write_text(source)
    environment = {
        **os.environ,
        "PYTHONPATH": str(directory),
        "ENDED_FILE": str(directory / "ended.jsonl"),
    }
    done = subprocess.run(
        [sys.executable, "-m", "island_battles_client.worker", "mybot:play"],
        input="".join(line + "\n" for line in lines),
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=environment,
        timeout=30,
        check=False,
    )
    assert done.returncode == 0, done.stderr
    answers = [json.loads(line) for line in done.stdout.splitlines()]
    assert answers[0] == {"ready": True}
    return answers[1:], done.stderr


def test_the_worker_calls_game_ended_and_answers_ended(tmp_path: Path) -> None:
    turn = json.dumps({"turn": fixture_turn_data()})
    ended = json.dumps({"game_ended": game_ended_data()})
    answers, stderr = worker_exchange(tmp_path, BOT, [ended, turn])
    assert answers[0] == {"ended": True}
    assert "result" in answers[1], "the worker goes on after the end"
    assert (tmp_path / "ended.jsonl").read_text() == '[7, "last_team_standing"]\n'
    assert "printing at the end" in stderr


def test_a_failing_game_ended_is_an_error_line(tmp_path: Path) -> None:
    ended = json.dumps({"game_ended": game_ended_data()})
    [answer], _ = worker_exchange(tmp_path, FAILING_BOT, [ended])
    assert "RuntimeError: no demo for me" in answer["error"]


def test_a_bot_without_game_ended_answers_ended_at_once(tmp_path: Path) -> None:
    ended = json.dumps({"game_ended": game_ended_data()})
    [answer, invalid], _ = worker_exchange(
        tmp_path, PLAIN_BOT, [ended, '{"game_ended": {"game_id": 7}}']
    )
    assert answer == {"ended": True}
    assert "not a GameEndedDelivery" in invalid["error"]


# serve ----------------------------------------------------------------------------

SERVED_BOT = """
import json, time
from pathlib import Path
from island_battles_client import turn_result

def play(turn):
    return turn_result()

def game_ended(delivery):
    time.sleep(0.5)
    with open(Path(__file__).with_name("ended.jsonl"), "a") as file:
        file.write(json.dumps([delivery.game_id, delivery.team_color]) + "\\n")
"""


@pytest.fixture
def server(tmp_path: Path) -> Iterator[tuple[BotServer, Path]]:
    """A server for ``ended_bot:play``, started on a free port."""
    file = tmp_path / "ended_bot.py"
    file.write_text(SERVED_BOT)
    sys.path.insert(0, str(tmp_path))
    bot = ReloadingPlay("ended_bot:play")
    bot.get()
    bot_server = BotServer(
        ("127.0.0.1", 0), bot, ["http://localhost:8888", GAME_ORIGIN]
    )
    thread = threading.Thread(target=bot_server.serve_forever, daemon=True)
    thread.start()
    yield bot_server, tmp_path / "ended.jsonl"
    bot_server.shutdown()
    bot_server.server_close()
    sys.path.remove(str(tmp_path))
    sys.modules.pop("ended_bot", None)


def post_game_ended(
    bot_server: BotServer, body: bytes, headers: dict[str, str] | None = None
) -> httpx.Response:
    return httpx.post(
        f"http://127.0.0.1:{bot_server.server_address[1]}/game-ended",
        content=body,
        headers={"Content-Type": "application/json", **(headers or {})},
    )


def wait_for_lines(file: Path, count: int, seconds: float = 10.0) -> list[str]:
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if file.exists() and len(lines := file.read_text().splitlines()) >= count:
            return lines
        time.sleep(0.05)
    raise AssertionError(f"{file} has fewer than {count} lines")


def test_a_game_end_is_accepted_at_once_and_taken_once(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, ended = server
    body = json.dumps(game_ended_data()).encode()
    started = time.monotonic()
    response = post_game_ended(bot_server, body, {"Origin": GAME_ORIGIN})
    assert (response.status_code, response.json()) == (202, {"accepted": True})
    assert time.monotonic() - started < 0.5, "nothing waits for game_ended"
    assert response.headers["Access-Control-Allow-Origin"] == GAME_ORIGIN
    # Delivered again, as by a reloaded tab: accepted, and taken once.
    assert post_game_ended(bot_server, body).status_code == 202
    assert wait_for_lines(ended, 1) == ["[7, 1]"]
    time.sleep(0.8)
    assert ended.read_text().splitlines() == ["[7, 1]"]


def test_a_body_that_is_not_a_game_end_is_a_422(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, ended = server
    response = post_game_ended(bot_server, json.dumps(fixture_turn_data()).encode())
    assert response.status_code == 422
    assert "GameEndedDelivery" in response.text
    time.sleep(0.7)
    assert not ended.exists()


def test_a_demo_link_elsewhere_is_a_400_and_nothing_runs(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, ended = server
    body = json.dumps(game_ended_data("https://evil.example.com/demo")).encode()
    response = post_game_ended(bot_server, body)
    assert response.status_code == 400
    assert "not an allowed host" in response.text
    time.sleep(0.7)
    assert not ended.exists()


def test_a_game_end_from_another_origin_is_refused(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, _ = server
    body = json.dumps(game_ended_data()).encode()
    response = post_game_ended(bot_server, body, {"Origin": "https://evil.example.com"})
    assert response.status_code == 403
    assert "Access-Control-Allow-Origin" not in response.headers


def test_demo_links_follow_the_submit_hosts() -> None:
    assert demo_refusal("https://island-battles.elidan.org/game/1/demo?token=x") is None
    assert demo_refusal("http://localhost:8888/game/1/demo") is None
    assert demo_refusal("http://island-battles.elidan.org/game/1/demo") is not None
    assert demo_refusal("https://game.example.org/demo") is not None
    assert (
        demo_refusal("https://game.example.org/demo", frozenset({"game.example.org"}))
        is None
    )


# run ------------------------------------------------------------------------------


def ended_state() -> dict:
    generation = fixture_turn_data()["generation"]
    return {
        "game_id": 7,
        "lobby_id": LOBBY_ID,
        "lobby_round": 1,
        "generation_id": generation["id"],
        "player_id": PLAYER_ID,
        "team_color": 1,
        "channel": f"player:{PLAYER_ID}",
        "submitted": False,
        "generation": generation,
        "turn_number": 91,
        "max_generations": 300,
        "log_lines_stored": 0,
        "memory": None,
        "bot": {"kind": "localhost", "port": 41414},
        "relayed_bots": [],
        "rated": False,
        "departures": [],
        "turn_deadline_at": None,
        "turn_pace_milliseconds": None,
        "outcome": OUTCOME,
    }


class Game:
    """The lobby's and the game's routes for a game that has ended."""

    def __init__(self, delivery_status: int = 200) -> None:
        self.delivery_status = delivery_status
        self.delivery_authorizations: list[str] = []

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
        if path == f"/game/from-lobby/{LOBBY_ID}/generation":
            return httpx.Response(200, json=ended_state())
        if path == f"/game/from-lobby/{LOBBY_ID}/game-ended-delivery":
            self.delivery_authorizations.append(request.headers["Authorization"])
            if self.delivery_status != 200:
                return httpx.Response(
                    self.delivery_status, json={"detail": "no such route"}
                )
            return httpx.Response(200, json=game_ended_data())
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


def run_ended_game(game: Game, game_ended) -> list[str]:
    """Run a client on the ended game; returns the order of the callbacks."""
    calls: list[str] = []

    async def scenario() -> None:
        client = IslandBattlesClient(
            "guest",
            LOBBY_ID,
            play=lambda turn: turn_result([]),
            game_ended=game_ended,
            base_url="http://game.test",
            on_game_ended=lambda outcome, generation: calls.append("on_game_ended"),
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(game.handle)),
            sdk_factory=FakeSDK,
            log_to_stdout=False,
        )
        await asyncio.wait_for(client.run(), 5)

    asyncio.run(scenario())
    return calls


def test_run_gives_the_bot_the_end_of_its_game() -> None:
    game = Game()
    received: list[GameEndedDelivery] = []

    async def game_ended(delivery: GameEndedDelivery) -> None:
        received.append(delivery)

    calls = run_ended_game(game, game_ended)
    assert calls == ["on_game_ended"]
    [delivery] = received
    assert (delivery.game_id, delivery.team_color, delivery.turn_number) == (7, 1, 91)
    assert delivery.outcome.summary == "Red won: the last team standing"
    assert delivery.memory == {"sent": 4}
    assert game.delivery_authorizations == ["Bearer pt"], "with the player token"


@pytest.mark.parametrize("status", [404, 401, 409])
def test_run_logs_a_game_end_it_cannot_read_and_still_ends(
    status: int, caplog: pytest.LogCaptureFixture
) -> None:
    game = Game(delivery_status=status)
    received: list[GameEndedDelivery] = []
    with caplog.at_level(logging.WARNING):
        calls = run_ended_game(game, received.append)
    assert calls == ["on_game_ended"]
    assert received == []
    assert any(
        "could not read the end of the game" in record.getMessage()
        for record in caplog.records
    )


def test_run_logs_a_failing_game_ended_and_still_ends(
    caplog: pytest.LogCaptureFixture,
) -> None:
    def game_ended(delivery: GameEndedDelivery) -> None:
        raise RuntimeError("no demo for me")

    with caplog.at_level(logging.WARNING):
        run_ended_game(Game(), game_ended)
    [record] = [r for r in caplog.records if "game_ended failed" in r.getMessage()]
    assert record.exc_info is not None
    assert "no demo for me" in str(record.exc_info[1])


def test_run_without_game_ended_does_not_read_the_end() -> None:
    game = Game()
    assert run_ended_game(game, None) == ["on_game_ended"]
    assert game.delivery_authorizations == []


# download_demo ------------------------------------------------------------------


class DemoServer(ThreadingHTTPServer):
    """Serves a demo file at /game/7/demo, the way the game does."""

    daemon_threads = True

    def __init__(self, body: bytes, headers: dict[str, str], status: int = 200):
        super().__init__(("127.0.0.1", 0), DemoHandler)
        self.body = body
        self.response_headers = headers
        self.status = status
        self.requests: list[dict[str, str]] = []

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.server_address[1]}/game/7/demo?token=ibdt1.x"


class DemoHandler(BaseHTTPRequestHandler):
    server: DemoServer

    def log_message(self, format: str, *args: object) -> None:
        pass

    def do_GET(self) -> None:
        self.server.requests.append({"path": self.path, **dict(self.headers)})
        self.send_response(self.server.status)
        for name, value in self.server.response_headers.items():
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(self.server.body)))
        self.end_headers()
        self.wfile.write(self.server.body)


@pytest.fixture
def demo_file() -> bytes:
    """Bytes that stand for a Demo file: download_demo doesn't parse it."""
    return json.dumps({"game_id": 7, "generations": []}).encode()


def serve_demo(body: bytes, headers: dict[str, str], status: int = 200) -> DemoServer:
    server = DemoServer(body, headers, status)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


@pytest.mark.parametrize(
    ("packing", "headers"),
    [
        ("plain", {"Content-Type": "application/json"}),
        (
            "content-encoding",
            {"Content-Type": "application/json", "Content-Encoding": "gzip"},
        ),
        ("gzip-file", {"Content-Type": "application/gzip"}),
    ],
)
def test_download_demo_gets_the_json_with_a_plain_get(
    packing: str, headers: dict[str, str], demo_file: bytes, tmp_path: Path
) -> None:
    body = demo_file if packing == "plain" else gzip.compress(demo_file)
    server = serve_demo(body, headers)
    try:
        delivery = GameEndedDelivery.model_validate(game_ended_data(server.url))
        saved = tmp_path / "demo.json"
        assert download_demo(delivery, saved) == demo_file
        assert saved.read_bytes() == demo_file
        [sent] = server.requests
        assert sent["path"] == "/game/7/demo?token=ibdt1.x"
        assert "Authorization" not in sent, "the link carries its own token"
    finally:
        server.shutdown()
        server.server_close()


def test_download_demo_raises_the_games_refusal() -> None:
    server = serve_demo(
        json.dumps({"detail": "the demo token has expired"}).encode(),
        {"Content-Type": "application/problem+json"},
        status=401,
    )
    try:
        delivery = GameEndedDelivery.model_validate(game_ended_data(server.url))
        with pytest.raises(GameError) as raised:
            download_demo(delivery)
        assert raised.value.status == 401
        assert raised.value.detail == "the demo token has expired"
    finally:
        server.shutdown()
        server.server_close()
