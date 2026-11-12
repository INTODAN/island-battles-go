"""The worker: the process that the server's runners feed turns to.

``python -m island_battles_client.worker module:function`` loads a bot and
talks JSON Lines over stdin and stdout (UTF-8):

1. After loading the bot, the worker writes ``{"ready": true}``.
2. For each turn the runner writes one line ``{"turn": <BotTurn>}``. The
   worker answers one line, ``{"result": <BotTurnResult>}`` or
   ``{"error": "<message>"}``.
3. The result is checked as ``check`` and ``serve`` check it (memory size,
   commands, log lines), and the lines the bot logged become its ``logs``.
4. The bot's own stdout goes to stderr, so prints never corrupt the protocol.
   Stderr is free text.
5. The worker exits when stdin closes.

The module is imported from ``sys.path`` as it is; a package's runner sets
``PYTHONPATH=<unpacked>/site``. A bot that fails to load makes the worker
write one ``{"error": ...}`` line and exit with status 1.
"""

from __future__ import annotations

import asyncio
import importlib
import io
import json
import logging
import os
import sys
import traceback
from collections.abc import Iterable
from typing import IO

from pydantic import ValidationError

from ._generated import BotTurn
from .bot import Play, run_turn
from .game_log import ClientLogging


def import_play(spec: str) -> Play:
    """Import ``module:function`` from ``sys.path`` and return the function."""
    module_name, separator, function_name = spec.partition(":")
    if not separator or not module_name or not function_name:
        raise ValueError(f"expected module:function, not {spec!r}")
    module = importlib.import_module(module_name)
    play = getattr(module, function_name, None)
    if not callable(play):
        raise TypeError(f"module {module_name} has no function {function_name}")
    return play


def send(protocol: IO[str], line: str) -> None:
    """Write one protocol line."""
    protocol.write(line + "\n")
    protocol.flush()


def error_line(message: str) -> str:
    """The protocol line for a failure."""
    return json.dumps({"error": message}, ensure_ascii=False)


async def answer(play: Play, line: str, client_logging: ClientLogging) -> str:
    """The protocol line that answers one request line."""
    try:
        request = json.loads(line)
    except ValueError as error:
        return error_line(f"the line is not JSON: {error}")
    if not isinstance(request, dict) or "turn" not in request:
        return error_line('the line must be an object like {"turn": ...}')
    try:
        turn = BotTurn.model_validate(request["turn"])
    except ValidationError as error:
        return error_line(f"the turn is not a BotTurn:\n{error}")
    try:
        result = await run_turn(play, turn, client_logging)
    except Exception:  # noqa: BLE001 - reported to the runner
        return error_line(traceback.format_exc())
    return '{"result": ' + result.model_dump_json(by_alias=True) + "}"


def serve_lines(play: Play, requests: Iterable[str], protocol: IO[str]) -> None:
    """Answer each request line until ``requests`` ends."""
    # The same log scope that serve gives a turn, with nothing printed.
    client_logging = ClientLogging(object(), logging.INFO, False, None)
    client_logging.attach()
    try:
        with asyncio.Runner() as runner:
            for raw in requests:
                line = raw.strip()
                if line:
                    send(protocol, runner.run(answer(play, line, client_logging)))
    finally:
        client_logging.detach()


def main(argv: list[str] | None = None) -> int:
    """Entry point of ``python -m island_battles_client.worker``."""
    arguments = sys.argv[1:] if argv is None else argv
    if len(arguments) != 1:
        print(
            "usage: python -m island_battles_client.worker module:function",
            file=sys.stderr,
        )
        return 2
    # The protocol keeps the real stdout. File descriptor 1 then points at
    # stderr, so prints, C extensions and child processes can't write to it.
    sys.stdout.flush()
    protocol = os.fdopen(os.dup(1), "w", encoding="utf-8", newline="\n")
    os.dup2(2, 1)
    sys.stdout = sys.stderr
    requests = io.TextIOWrapper(sys.stdin.buffer, encoding="utf-8")
    try:
        play = import_play(arguments[0])
    except Exception:  # noqa: BLE001
        send(
            protocol,
            error_line(f"could not load {arguments[0]}:\n{traceback.format_exc()}"),
        )
        return 1
    send(protocol, '{"ready": true}')
    serve_lines(play, requests, protocol)
    return 0


if __name__ == "__main__":
    sys.exit(main())
