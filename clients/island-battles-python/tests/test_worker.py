"""``python -m island_battles_client.worker``: the runner's JSON Lines protocol."""

import json
import os
import subprocess
import sys
from pathlib import Path

from island_battles_client.fixture import fixture_turn_data

BOT = """
import logging
from island_battles_client import turn_result, log_line

logger = logging.getLogger("island_battles.player")

def play(turn):
    print("printing to stdout must not reach the protocol")
    logger.info("logged line")
    return turn_result(
        logs=[log_line("returned line")],
        memory={"turns": (turn.memory or {}).get("turns", 0) + 1},
    )

def explode(turn):
    raise RuntimeError("the bot is broken")

def big_memory(turn):
    return turn_result(memory={"blob": "x" * 70000})

def not_a_result(turn):
    return 5
"""


def start(directory: Path, entry: str) -> subprocess.Popen[str]:
    """Start a worker for ``entry`` in ``mybot.py`` of a directory."""
    (directory / "mybot.py").write_text(BOT)
    environment = {**os.environ, "PYTHONPATH": str(directory)}
    return subprocess.Popen(
        [sys.executable, "-m", "island_battles_client.worker", entry],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        env=environment,
    )


def ready(worker: subprocess.Popen[str]) -> None:
    """Read the ready line."""
    assert worker.stdout is not None
    assert json.loads(worker.stdout.readline()) == {"ready": True}


def exchange(worker: subprocess.Popen[str], line: str) -> dict:
    """Write one line and read the one-line answer."""
    assert worker.stdin is not None
    assert worker.stdout is not None
    worker.stdin.write(line + "\n")
    worker.stdin.flush()
    return json.loads(worker.stdout.readline())


def finish(worker: subprocess.Popen[str]) -> str:
    """Close stdin, wait for the exit and return stderr."""
    assert worker.stdin is not None
    assert worker.stdout is not None
    assert worker.stderr is not None
    worker.stdin.close()
    worker.wait(timeout=30)
    stderr = worker.stderr.read()
    worker.stdout.close()
    worker.stderr.close()
    return stderr


def turn_line(memory: dict | None = None) -> str:
    return json.dumps({"turn": {**fixture_turn_data(), "memory": memory}})


def test_worker_answers_turns_and_exits_on_eof(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:play")
    ready(worker)
    assert exchange(worker, turn_line())["result"]["memory"] == {"turns": 1}
    assert exchange(worker, turn_line({"turns": 1}))["result"]["memory"] == {"turns": 2}
    finish(worker)
    assert worker.returncode == 0


def test_logger_lines_come_first_in_the_result_logs(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:play")
    ready(worker)
    logs = exchange(worker, turn_line())["result"]["logs"]
    finish(worker)
    assert [line["message"] for line in logs] == ["logged line", "returned line"]


def test_a_print_in_the_bot_goes_to_stderr(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:play")
    ready(worker)
    answer = exchange(worker, turn_line())
    stderr = finish(worker)
    assert "result" in answer
    assert "printing to stdout" in stderr


def test_a_raising_bot_is_an_error_line_and_the_worker_goes_on(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:explode")
    ready(worker)
    answer = exchange(worker, turn_line())
    assert "RuntimeError: the bot is broken" in answer["error"]
    assert "result" not in answer
    assert "error" in exchange(worker, turn_line())
    finish(worker)
    assert worker.returncode == 0


def test_an_invalid_result_is_an_error_line(tmp_path: Path) -> None:
    for entry, text in (
        ("mybot:big_memory", "memory is"),
        ("mybot:not_a_result", "BotTurnResult"),
    ):
        worker = start(tmp_path, entry)
        ready(worker)
        assert text in exchange(worker, turn_line())["error"]
        finish(worker)


def test_bad_lines_are_error_lines(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:play")
    ready(worker)
    assert "not JSON" in exchange(worker, "not json")["error"]
    assert "turn" in exchange(worker, '{"nothing": 1}')["error"]
    assert "not a BotTurn" in exchange(worker, '{"turn": {}}')["error"]
    finish(worker)


def test_a_bot_that_does_not_load_writes_one_error_and_exits(tmp_path: Path) -> None:
    worker = start(tmp_path, "mybot:missing")
    assert worker.stdout is not None
    answer = json.loads(worker.stdout.readline())
    assert "has no function missing" in answer["error"]
    finish(worker)
    assert worker.returncode == 1
