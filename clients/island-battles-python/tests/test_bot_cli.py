"""``island-battles-bot``: check, serve and the helpers a bot uses."""

import http.client
import json
import socket
import subprocess
import sys
import threading
import time
from collections.abc import Iterator
from datetime import UTC, datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from island_battles_client.cli import (
    DEFAULT_ORIGINS,
    BotServer,
    ReloadingPlay,
    submit_refusal,
)
from island_battles_client.fixture import fixture_turn_data

GAME_ORIGIN = "https://game.example.org"
GOOD_BOT = """
from island_battles_client import turn_result, log_line

def play(turn):
    return turn_result(logs=[log_line("answer {answer}")], memory={{"answer": {answer}}})
"""


def run_cli(*args: str, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    """Run ``island-battles-bot`` as a module."""
    return subprocess.run(
        [sys.executable, "-m", "island_battles_client.cli", *args],
        cwd=cwd,
        capture_output=True,
        text=True,
        timeout=30,
        check=False,
    )


def check(directory: Path, source: str) -> subprocess.CompletedProcess[str]:
    """Run ``island-battles-bot check`` on a bot written into a directory."""
    (directory / "mybot.py").write_text(source)
    return run_cli("check", "mybot:play", cwd=directory)


def test_check_passes_for_the_example_bot() -> None:
    result = run_cli("check", "examples.bot:play")
    assert result.returncode == 0, result.stderr
    assert result.stdout.startswith("ok: ")


def test_check_counts_logger_lines_with_the_lines_the_bot_returns(
    tmp_path: Path,
) -> None:
    source = (
        "import logging\n"
        "from island_battles_client import turn_result, log_line\n"
        "logger = logging.getLogger('island_battles.player')\n"
        "def play(turn):\n"
        "    logger.info('first')\n"
        "    logger.warning('second')\n"
        "    return turn_result(logs=[log_line('returned')])\n"
    )
    result = check(tmp_path, source)
    assert result.returncode == 0, result.stderr
    assert ", 3 log lines, " in result.stdout


def test_check_counts_no_log_lines_for_a_silent_bot(tmp_path: Path) -> None:
    result = check(
        tmp_path,
        "from island_battles_client import turn_result\n"
        "def play(turn):\n    return turn_result()\n",
    )
    assert result.returncode == 0, result.stderr
    assert ", 0 log lines, " in result.stdout


def test_the_help_gives_the_lobbys_timeouts() -> None:
    result = run_cli("serve", "--help")
    assert result.returncode == 0, result.stderr
    text = " ".join(result.stdout.split())
    assert "shortest turn timeout" not in text
    assert "of 10" not in text
    assert "30 s by default" in text
    assert "from 5 to 600 s" in text


def test_check_passes_for_a_bot_and_an_async_bot(tmp_path: Path) -> None:
    assert check(tmp_path, GOOD_BOT.format(answer=1)).returncode == 0
    asynchronous = GOOD_BOT.replace("def play", "async def play").format(answer=1)
    assert check(tmp_path, asynchronous).returncode == 0


@pytest.mark.parametrize(
    ("source", "message"),
    [
        (
            "def play(turn):\n    raise RuntimeError('boom')\n",
            "the bot raised an exception",
        ),
        ("def play(turn):\n    return 42\n", "the bot returned an invalid result"),
        (
            (
                "from island_battles_client import turn_result\n"
                "def play(turn):\n    return turn_result(memory={'blob': 'x' * 70000})\n"
            ),
            "memory is too large",
        ),
        (
            "import time\ndef play(turn):\n    time.sleep(5)\n",
            "took longer than 2 s",
        ),
        ("x = (\n", "could not load mybot:play"),
    ],
    ids=["raises", "invalid-result", "memory", "slow", "syntax-error"],
)
def test_check_fails_with_its_own_message(
    tmp_path: Path, source: str, message: str
) -> None:
    result = check(tmp_path, source)
    assert result.returncode == 1
    assert message in result.stderr
    if message == "the bot raised an exception":
        assert "boom" in result.stderr


class FakeAPI(ThreadingHTTPServer):
    """A game server's POST /generation/commands that records each batch."""

    daemon_threads = True

    def __init__(self) -> None:
        super().__init__(("127.0.0.1", 0), FakeAPIHandler)
        self.batches: list[tuple[str, str, dict]] = []
        # Statuses and bodies to answer with, in order; then 200.
        self.answers: list[tuple[int, dict]] = []
        self.received = threading.Condition()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.server_address[1]}/generation/commands"

    def wait(self, count: int, seconds: float = 10.0) -> list[dict]:
        """The first count batches' bodies, once they arrived."""
        with self.received:
            assert self.received.wait_for(
                lambda: len(self.batches) >= count, seconds
            ), f"{len(self.batches)} of {count} batches arrived"
            return [body for _, _, body in self.batches[:count]]


class FakeAPIHandler(BaseHTTPRequestHandler):
    server: FakeAPI

    def log_message(self, format: str, *args: object) -> None:
        pass

    def do_POST(self) -> None:
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        with self.server.received:
            self.server.batches.append(
                (self.path, self.headers.get("Authorization", ""), body)
            )
            status, answer = (
                self.server.answers.pop(0) if self.server.answers else (200, {})
            )
            self.server.received.notify_all()
        data = json.dumps(answer).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


@pytest.fixture
def api() -> Iterator[FakeAPI]:
    fake = FakeAPI()
    thread = threading.Thread(target=fake.serve_forever, daemon=True)
    thread.start()
    yield fake
    fake.shutdown()
    fake.server_close()


@pytest.fixture
def server(tmp_path: Path) -> Iterator[tuple[BotServer, Path]]:
    """A server for ``serve_bot:play``, started on a free port."""
    file = tmp_path / "serve_bot.py"
    file.write_text(GOOD_BOT.format(answer=1))
    sys.path.insert(0, str(tmp_path))
    bot = ReloadingPlay("serve_bot:play")
    bot.get()
    bot_server = BotServer(
        ("127.0.0.1", 0), bot, ["http://localhost:8888", GAME_ORIGIN]
    )
    thread = threading.Thread(target=bot_server.serve_forever, daemon=True)
    thread.start()
    yield bot_server, file
    bot_server.shutdown()
    bot_server.server_close()
    sys.path.remove(str(tmp_path))
    sys.modules.pop("serve_bot", None)


def request(
    bot_server: BotServer,
    method: str,
    path: str,
    body: bytes | None = None,
    headers: dict[str, str] | None = None,
) -> tuple[int, http.client.HTTPMessage, bytes]:
    connection = http.client.HTTPConnection("127.0.0.1", bot_server.server_address[1])
    sent = dict(headers or {})
    if method == "POST":
        sent.setdefault("Content-Type", "application/json")
    connection.request(method, path, body, sent)
    response = connection.getresponse()
    data = response.read()
    connection.close()
    return response.status, response.headers, data


def delivery(
    url: str, deadline_at: str | None = None, memory: dict | None = None
) -> dict:
    """A TurnDelivery of the fixture turn, to be submitted to url."""
    turn = fixture_turn_data()
    turn["memory"] = memory
    turn["turn_deadline_at"] = deadline_at
    return {
        "turn": turn,
        "submit": {"url": url, "token": "ibst1.test", "deadline_at": deadline_at},
    }


def post_turn(
    bot_server: BotServer,
    url: str,
    headers: dict[str, str] | None = None,
    **options: object,
) -> tuple[int, http.client.HTTPMessage, bytes]:
    body = json.dumps(delivery(url, **options)).encode()  # type: ignore[arg-type]
    return request(bot_server, "POST", "/turn", body, headers)


def test_a_turn_is_accepted_at_once_and_its_result_submitted(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    started = time.monotonic()
    status, _, body = post_turn(bot_server, api.url)
    assert (status, json.loads(body)) == (202, {"accepted": True})
    assert time.monotonic() - started < 1
    [batch] = api.wait(1)
    path, authorization, _ = api.batches[0]
    assert (path, authorization) == ("/generation/commands", "Bearer ibst1.test")
    assert batch["game_id"] == 1
    assert batch["generation_id"] == 12
    assert batch["complete"] is True
    assert batch["commands"] == []
    assert batch["memory"] == {"answer": 1}
    assert [line["message"] for line in batch["logs"]] == ["answer 1"]
    assert len(batch["request_id"]) == 36
    status, _, body = request(bot_server, "GET", "/health")
    assert (status, json.loads(body)) == (200, {"name": "serve_bot:play", "ok": True})


def test_a_slow_bot_still_answers_202_at_once(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, file = server
    file.write_text(
        "import time\nfrom island_battles_client import turn_result\n"
        "def play(turn):\n    time.sleep(1.5)\n    return turn_result(memory={'slow': 1})\n"
    )
    started = time.monotonic()
    assert post_turn(bot_server, api.url)[0] == 202
    assert time.monotonic() - started < 1
    assert api.wait(1)[0]["memory"] == {"slow": 1}


def pass_line(batch: dict) -> str:
    """The one line of a batch that passes the turn."""
    assert batch["commands"] == []
    assert batch["complete"] is True
    [line] = batch["logs"]
    assert (line["level"], line["logger"]) == ("error", "island_battles.bots")
    return line["message"]


def test_a_bot_that_raises_passes_its_turn_with_an_error_line(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, file = server
    file.write_text("def play(turn):\n    raise ValueError('no plan')\n")
    assert post_turn(bot_server, api.url, memory={"kept": True})[0] == 202
    [batch] = api.wait(1)
    assert batch["memory"] == {"kept": True}
    message = pass_line(batch)
    assert "failed on generation 12, so it passes this turn" in message
    assert "ValueError: no plan" in message


def test_a_bot_that_does_not_load_passes_until_it_is_fixed(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, file = server
    file.write_text("def play(turn:\n")
    assert post_turn(bot_server, api.url)[0] == 202
    assert "could not load serve_bot:play" in pass_line(api.wait(1)[0])
    file.write_text(GOOD_BOT.format(answer=333))
    assert post_turn(bot_server, api.url)[0] == 202
    assert api.wait(2)[1]["memory"] == {"answer": 333}


def test_editing_the_file_changes_the_next_turn_without_a_restart(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, file = server
    assert post_turn(bot_server, api.url)[0] == 202
    assert api.wait(1)[0]["memory"] == {"answer": 1}
    file.write_text(GOOD_BOT.format(answer=22))
    assert post_turn(bot_server, api.url)[0] == 202
    assert api.wait(2)[1]["memory"] == {"answer": 22}


def test_a_looping_bot_passes_its_turn_and_the_server_keeps_serving(
    server: tuple[BotServer, Path], api: FakeAPI, tmp_path: Path
) -> None:
    bot_server, file = server
    bot_server.turn_timeout = 0.5
    stop = tmp_path / "stop"
    # Loops until the test ends, so no spinning thread outlives the test run.
    file.write_text(
        "import os\n"
        "def play(turn):\n"
        f"    while not os.path.exists({str(stop)!r}):\n"
        "        pass\n"
    )
    try:
        assert post_turn(bot_server, api.url)[0] == 202
        message = pass_line(api.wait(1)[0])
        assert "took longer than 0.5 s on generation 12" in message
        file.write_text(GOOD_BOT.format(answer=7))
        # Another generation of the same game: the first is still looping.
        body = delivery(api.url)
        body["turn"]["generation"]["id"] = 13
        assert request(bot_server, "POST", "/turn", json.dumps(body).encode())[0] == 202
        assert api.wait(2)[1]["memory"] == {"answer": 7}
    finally:
        stop.touch()


def test_the_turns_deadline_cuts_the_bots_time(
    server: tuple[BotServer, Path], api: FakeAPI, tmp_path: Path
) -> None:
    bot_server, file = server
    stop = tmp_path / "stop"
    file.write_text(
        "import os\n"
        "def play(turn):\n"
        f"    while not os.path.exists({str(stop)!r}):\n"
        "        pass\n"
    )
    deadline = datetime.now(UTC) + timedelta(seconds=2)
    started = time.monotonic()
    try:
        assert (
            post_turn(bot_server, api.url, deadline_at=deadline.isoformat())[0] == 202
        )
        message = pass_line(api.wait(1)[0])
        # 8 s by default, but the deadline leaves 2 s less the 1 s margin.
        assert time.monotonic() - started < 4
        assert "so it passes this turn" in message
    finally:
        stop.touch()


def test_a_rejected_answer_passes_the_turn(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    api.answers.append(
        (
            409,
            {
                "type": "urn:island-battles:problem:insufficient-soldiers",
                "detail": "castle 0 has 1 soldier left",
            },
        )
    )
    assert post_turn(bot_server, api.url)[0] == 202
    first, second = api.wait(2)
    assert first["memory"] == {"answer": 1}
    assert second["memory"] is None
    assert second["request_id"] != first["request_id"]
    message = pass_line(second)
    assert "rejected the bot's answer" in message
    assert "castle 0 has 1 soldier left" in message


def test_a_server_error_is_retried_and_a_conflict_is_not(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    api.answers.extend(
        [(503, {}), (409, {"detail": "player turn is already complete"})]
    )
    assert post_turn(bot_server, api.url)[0] == 202
    first, second = api.wait(2)
    assert first == second
    time.sleep(0.5)
    assert len(api.batches) == 2


@pytest.mark.parametrize(
    "url",
    [
        "https://evil.example.com/generation/commands",
        "http://island-battles.elidan.org/generation/commands",
        "http://192.168.1.2:8888/generation/commands",
        "https://user:secret@island-battles.elidan.org/generation/commands",
        "https://game.example.org.evil.com/generation/commands",
    ],
)
def test_a_submit_host_that_is_not_allowed_is_a_400_and_nothing_is_sent(
    server: tuple[BotServer, Path], api: FakeAPI, url: str, tmp_path: Path
) -> None:
    bot_server, file = server
    marker = tmp_path / "ran"
    file.write_text(
        f"from pathlib import Path\ndef play(turn):\n    Path({str(marker)!r}).touch()\n"
    )
    status, _, body = post_turn(bot_server, url)
    assert status == 400
    assert b"not an allowed host" in body
    assert b"--allow-submit-host" in body
    time.sleep(0.2)
    assert not marker.exists()
    assert api.batches == []


def test_the_allowed_submit_hosts() -> None:
    allowed = [
        "https://island-battles.elidan.org/generation/commands",
        "http://localhost:8888/generation/commands",
        "http://127.0.0.1:3000/generation/commands",
        "http://localhost/generation/commands",
    ]
    for url in allowed:
        assert submit_refusal(url) is None, url
    assert submit_refusal("https://game.example.org/generation/commands") is not None
    extra = frozenset({"game.example.org", "other.example.org:8443"})
    assert submit_refusal("https://game.example.org/generation/commands", extra) is None
    assert submit_refusal("https://game.example.org:444/x", extra) is None
    assert submit_refusal("https://other.example.org:8443/x", extra) is None
    assert submit_refusal("https://other.example.org/x", extra) is not None
    assert submit_refusal("http://game.example.org/x", extra) is not None


def test_serve_adds_submit_hosts() -> None:
    result = run_cli("serve", "--help")
    assert "--allow-submit-host" in result.stdout


def test_a_body_that_is_not_a_delivery_is_a_422(
    server: tuple[BotServer, Path],
) -> None:
    # An old game client's BotTurn.
    turn = json.dumps(fixture_turn_data()).encode()
    status, _, body = request(server[0], "POST", "/turn", turn)
    assert status == 422
    assert b"TurnDelivery" in body


def test_preflight_from_an_allowed_origin_succeeds_with_private_network_access(
    server: tuple[BotServer, Path],
) -> None:
    for origin in (GAME_ORIGIN, "http://localhost:8888"):
        status, headers, _ = request(
            server[0],
            "OPTIONS",
            "/turn",
            headers={
                "Origin": origin,
                "Access-Control-Request-Method": "POST",
                "Access-Control-Request-Headers": "content-type",
                "Access-Control-Request-Private-Network": "true",
            },
        )
        assert status == 204
        assert headers["Access-Control-Allow-Origin"] == origin
        assert headers["Access-Control-Allow-Private-Network"] == "true"
        assert "POST" in headers["Access-Control-Allow-Methods"]
        assert headers["Access-Control-Allow-Headers"] == "content-type"


def test_preflight_and_requests_from_another_origin_are_refused(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    evil = {"Origin": "https://evil.example.com"}
    status, headers, _ = request(
        bot_server,
        "OPTIONS",
        "/turn",
        headers={
            **evil,
            "Access-Control-Request-Method": "POST",
            "Access-Control-Request-Private-Network": "true",
        },
    )
    assert status == 403
    assert "Access-Control-Allow-Origin" not in headers
    assert "Access-Control-Allow-Private-Network" not in headers
    # A page can send a simple POST without a preflight; the bot must not run.
    status, headers, body = post_turn(bot_server, api.url, evil)
    assert status == 403
    assert b"not allowed" in body
    assert "Access-Control-Allow-Origin" not in headers
    assert request(bot_server, "GET", "/health", headers=evil)[0] == 403
    time.sleep(0.2)
    assert api.batches == []


def test_an_allowed_origin_can_read_the_answer(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    status, headers, _ = post_turn(server[0], api.url, {"Origin": GAME_ORIGIN})
    assert status == 202
    assert headers["Access-Control-Allow-Origin"] == GAME_ORIGIN
    api.wait(1)


def raw_exchange(bot_server: BotServer, payload: bytes) -> bytes:
    """Send bytes over one socket and read until the server closes it."""
    with socket.create_connection(
        ("127.0.0.1", bot_server.server_address[1]), timeout=5
    ) as connection:
        connection.sendall(payload)
        answer = b""
        while chunk := connection.recv(65536):
            answer += chunk
    return answer


def test_a_foreign_origin_cannot_smuggle_a_second_request_in_its_body(
    server: tuple[BotServer, Path], api: FakeAPI, tmp_path: Path
) -> None:
    bot_server, file = server
    marker = tmp_path / "ran"
    file.write_text(
        f"from pathlib import Path\ndef play(turn):\n    Path({str(marker)!r}).touch()\n"
    )
    port = bot_server.server_address[1]
    turn = json.dumps(delivery(api.url)).encode()
    inner = (
        f"POST /turn HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\n"
        f"Content-Type: application/json\r\nContent-Length: {len(turn)}\r\n\r\n"
    ).encode() + turn
    outer = (
        f"POST /turn HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\n"
        f"Origin: https://evil.example.com\r\nContent-Type: text/plain\r\n"
        f"Content-Length: {len(inner)}\r\n\r\n"
    ).encode() + inner
    answer = raw_exchange(bot_server, outer)  # returns only when the server closes
    time.sleep(0.3)
    assert answer.count(b"HTTP/1.") == 1
    assert b" 403 " in answer.split(b"\r\n", 1)[0]
    assert b"Connection: close" in answer
    assert not marker.exists()
    assert api.batches == []


def test_a_post_that_is_not_json_is_a_415_and_the_connection_closes(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    status, headers, _ = post_turn(bot_server, api.url, {"Content-Type": "text/plain"})
    assert status == 415
    assert headers["Connection"] == "close"
    status, _, _ = post_turn(
        bot_server, api.url, {"Content-Type": "application/json; charset=utf-8"}
    )
    assert status == 202
    api.wait(1)


def test_another_host_header_is_a_403_against_dns_rebinding(
    server: tuple[BotServer, Path], api: FakeAPI
) -> None:
    bot_server, _ = server
    port = bot_server.server_address[1]
    for host in ("evil.example.com", f"evil.example.com:{port}", "127.0.0.1"):
        assert post_turn(bot_server, api.url, {"Host": host})[0] == 403
        assert request(bot_server, "GET", "/health", headers={"Host": host})[0] == 403
    assert (
        request(bot_server, "GET", "/health", headers={"Host": f"localhost:{port}"})[0]
        == 200
    )


def test_a_body_over_the_limit_is_a_413(server: tuple[BotServer, Path]) -> None:
    bot_server, _ = server
    answer = raw_exchange(
        bot_server,
        f"POST /turn HTTP/1.1\r\nHost: 127.0.0.1:{bot_server.server_address[1]}\r\n"
        "Content-Type: application/json\r\nContent-Length: 99999999\r\n\r\n".encode(),
    )
    assert b" 413 " in answer.split(b"\r\n", 1)[0]


def test_check_exits_at_once_for_a_bot_that_never_returns(tmp_path: Path) -> None:
    for source in (
        "def play(turn):\n    while True:\n        pass\n",
        "async def play(turn):\n    while True:\n        pass\n",
    ):
        (tmp_path / "mybot.py").write_text(source)
        started = time.monotonic()
        result = subprocess.run(
            [sys.executable, "-m", "island_battles_client.cli", "check", "mybot:play"],
            cwd=tmp_path,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )
        assert result.returncode == 1
        assert "took longer than 2 s" in result.stderr
        assert time.monotonic() - started < 5


def test_the_desktop_apps_origins_may_relay_by_default() -> None:
    """The desktop app (#105) sends its web view's origin through Tauri's HTTP plugin."""
    bot_server = BotServer(("127.0.0.1", 0), ReloadingPlay("x:y"), [*DEFAULT_ORIGINS])
    try:
        assert "tauri://localhost" in bot_server.origins
        assert "http://tauri.localhost" in bot_server.origins
        assert "https://evil.example.com" not in bot_server.origins
    finally:
        bot_server.server_close()
