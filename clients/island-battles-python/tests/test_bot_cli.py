"""``island-battles-bot``: check, serve and the helpers a bot uses."""

import http.client
import json
import subprocess
import sys
import threading
from collections.abc import Iterator
from pathlib import Path

import pytest

from island_battles_client import BotTurnResult
from island_battles_client.cli import BotServer, ReloadingPlay
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
    connection.request(method, path, body, headers or {})
    response = connection.getresponse()
    data = response.read()
    connection.close()
    return response.status, response.headers, data


def post_turn(
    bot_server: BotServer, headers: dict[str, str] | None = None
) -> tuple[int, http.client.HTTPMessage, bytes]:
    return request(
        bot_server, "POST", "/turn", json.dumps(fixture_turn_data()).encode(), headers
    )


def test_turn_answers_a_valid_result_and_health_says_ok(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, _ = server
    status, _, body = post_turn(bot_server)
    assert status == 200
    result = BotTurnResult.model_validate_json(body)
    assert result.memory == {"answer": 1}
    assert [line.message for line in result.logs] == ["answer 1"]
    status, _, body = request(bot_server, "GET", "/health")
    assert (status, json.loads(body)) == (200, {"name": "serve_bot:play", "ok": True})


def test_editing_the_file_changes_the_next_answer_without_a_restart(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, file = server
    assert BotTurnResult.model_validate_json(post_turn(bot_server)[2]).memory == {
        "answer": 1
    }
    file.write_text(GOOD_BOT.format(answer=22))
    result = BotTurnResult.model_validate_json(post_turn(bot_server)[2])
    assert result.memory == {"answer": 22}
    # A file that no longer loads answers 500 until it is fixed.
    file.write_text("def play(turn:\n")
    status, _, body = post_turn(bot_server)
    assert status == 500
    assert b"SyntaxError" in body
    file.write_text(GOOD_BOT.format(answer=333))
    assert BotTurnResult.model_validate_json(post_turn(bot_server)[2]).memory == {
        "answer": 333
    }


def test_an_exception_is_a_500_with_the_traceback(
    server: tuple[BotServer, Path],
) -> None:
    bot_server, file = server
    file.write_text("def play(turn):\n    raise ValueError('no plan')\n")
    status, _, body = post_turn(bot_server)
    assert status == 500
    assert b"Traceback" in body
    assert b"ValueError: no plan" in body


def test_a_body_that_is_not_a_turn_is_a_422(server: tuple[BotServer, Path]) -> None:
    status, _, body = request(server[0], "POST", "/turn", b'{"game_id": 1}')
    assert status == 422
    assert b"BotTurn" in body


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
    server: tuple[BotServer, Path],
) -> None:
    bot_server, file = server
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
    file.write_text("def play(turn):\n    raise AssertionError('ran')\n")
    status, headers, body = post_turn(bot_server, evil)
    assert status == 403
    assert b"not allowed" in body
    assert "Access-Control-Allow-Origin" not in headers
    assert request(bot_server, "GET", "/health", headers=evil)[0] == 403


def test_an_allowed_origin_can_read_the_answer(server: tuple[BotServer, Path]) -> None:
    _, headers, _ = post_turn(server[0], {"Origin": GAME_ORIGIN})
    assert headers["Access-Control-Allow-Origin"] == GAME_ORIGIN
