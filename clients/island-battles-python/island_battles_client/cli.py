"""``island-battles-bot``: serve, check and run a bot function.

``serve`` is the bot's local HTTP server for a game client in the browser,
``check`` calls the bot once on a fixture turn, and ``run`` plays a seat
without a browser.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import importlib
import importlib.util
import json
import logging
import os
import sys
import time
import traceback
from collections.abc import Sequence
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from ._generated import BotTurn, GameOutcome, Generation
from .bot import MEMORY_LIMIT_BYTES, Play, call_play, run_turn
from .client import IslandBattlesClient
from .errors import BotMemoryTooLargeError, GameError, InvalidBotResultError
from .fixture import fixture_turn, fixture_turn_data
from .game_log import ClientLogging

DEFAULT_PORT = 41414
HOST = "127.0.0.1"
# The game clients that may relay turns by default: the local backend's lobby
# page and the web client's dev server. Add more, such as the deployed game's
# origin, with --allow-origin.
DEFAULT_ORIGINS = ("http://localhost:8888", "http://localhost:3000")
CHECK_SECONDS = 2.0
MAX_BODY_BYTES = 16 * 1024 * 1024


def load_play(spec: str) -> Play:
    """Import ``module:function`` and return the function."""
    module_name, separator, function_name = spec.partition(":")
    if not separator or not module_name or not function_name:
        raise ValueError(f"expected module:function, not {spec!r}")
    if os.getcwd() not in sys.path:
        sys.path.insert(0, os.getcwd())
    module = importlib.import_module(module_name)
    play = getattr(module, function_name, None)
    if not callable(play):
        raise TypeError(f"module {module_name} has no function {function_name}")
    return play


class ReloadingPlay:
    """A bot that is imported again when a ``.py`` file in its directory changes.

    The check runs on each turn, so the next turn after a save uses the new
    code and nothing runs while the server is idle.
    """

    def __init__(self, spec: str) -> None:
        self.spec = spec
        self._module_name = spec.partition(":")[0]
        self._signature: tuple | None = None
        self._play: Play | None = None
        self._error: str | None = None

    def _directory(self) -> Path | None:
        module = sys.modules.get(self._module_name)
        file = getattr(module, "__file__", None)
        if file is None:
            try:
                found = importlib.util.find_spec(self._module_name)
            except ImportError, ValueError:
                return None
            file = found.origin if found else None
        return Path(file).resolve().parent if file else None

    def _current_signature(self) -> tuple:
        directory = self._directory()
        if directory is None:
            return ()
        signature = []
        for path in sorted(directory.glob("*.py")):
            stat = path.stat()
            signature.append((path.name, stat.st_mtime_ns, stat.st_size))
        return tuple(signature)

    def _forget_modules(self) -> None:
        directory = self._directory()
        for name, module in list(sys.modules.items()):
            file = getattr(module, "__file__", None)
            if name == self._module_name or (
                directory is not None
                and file is not None
                and Path(file).resolve().parent == directory
            ):
                del sys.modules[name]

    def get(self) -> Play:
        """The bot as of the files now; raises what importing it raised."""
        signature = self._current_signature()
        first = self._signature is None
        if first or signature != self._signature:
            importlib.invalidate_caches()
            if not first:
                self._forget_modules()
            self._signature = signature
            try:
                self._play = load_play(self.spec)
                self._error = None
                if not first:
                    print(f"reloaded {self.spec}", flush=True)
            except Exception:  # noqa: BLE001 - reported to the caller of /turn
                self._play = None
                self._error = traceback.format_exc()
                print(f"could not load {self.spec}\n{self._error}", flush=True)
        if self._play is None:
            raise ImportError(f"could not load {self.spec}\n{self._error}")
        return self._play


class BotServer(ThreadingHTTPServer):
    """The bot's HTTP server, with its bot and the origins it answers."""

    daemon_threads = True

    def __init__(
        self, address: tuple[str, int], bot: ReloadingPlay, origins: Sequence[str]
    ) -> None:
        super().__init__(address, BotRequestHandler)
        self.bot = bot
        self.origins = frozenset(origin.rstrip("/") for origin in origins)
        self.client_logging = ClientLogging(self, logging.INFO, True, None)
        self.client_logging.attach()

    def server_close(self) -> None:
        super().server_close()
        self.client_logging.detach()


class BotRequestHandler(BaseHTTPRequestHandler):
    """``POST /turn``, ``GET /health`` and the CORS preflights for them."""

    server: BotServer
    protocol_version = "HTTP/1.1"

    def log_message(self, format: str, *args: Any) -> None:
        print(f"{self.address_string()} {format % args}", flush=True)

    def _origin_allowed(self) -> bool | None:
        """None without an Origin header (curl), else whether the origin may relay."""
        origin = self.headers.get("Origin")
        if origin is None:
            return None
        return origin.rstrip("/") in self.server.origins

    def _send(self, status: int, body: bytes, content_type: str) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Vary", "Origin")
        if self._origin_allowed() is True:
            self.send_header("Access-Control-Allow-Origin", self.headers["Origin"])
        self.end_headers()
        self.wfile.write(body)

    def _text(self, status: int, text: str) -> None:
        self._send(status, text.encode("utf-8", "replace"), "text/plain; charset=utf-8")

    def _refuse(self) -> bool:
        """Refuse a request from an origin that may not relay turns."""
        if self._origin_allowed() is False:
            self._text(
                403,
                f"origin {self.headers['Origin']} is not allowed; "
                "start the server with --allow-origin to add it\n",
            )
            return True
        return False

    def do_OPTIONS(self) -> None:
        """Answer a preflight for an allowed origin, with Private Network Access."""
        if self._origin_allowed() is not True:
            self._text(403, "origin not allowed\n")
            return
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", self.headers["Origin"])
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header(
            "Access-Control-Allow-Headers",
            self.headers.get("Access-Control-Request-Headers", "content-type"),
        )
        self.send_header("Access-Control-Allow-Private-Network", "true")
        self.send_header("Access-Control-Max-Age", "600")
        self.send_header("Vary", "Origin")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self) -> None:
        """``/health`` says the bot is there."""
        if self._refuse():
            return
        if self.path != "/health":
            self._text(404, "not found\n")
            return
        body = json.dumps({"name": self.server.bot.spec, "ok": True}).encode()
        self._send(200, body, "application/json")

    def do_POST(self) -> None:
        """``/turn`` plays a ``BotTurn`` and answers its ``BotTurnResult``."""
        if self._refuse():
            return
        if self.path != "/turn":
            self._text(404, "not found\n")
            return
        try:
            length = int(self.headers.get("Content-Length", ""))
        except ValueError:
            self._text(411, "Content-Length is required\n")
            return
        if not 0 <= length <= MAX_BODY_BYTES:
            self._text(413, "request body is too large\n")
            return
        raw = self.rfile.read(length)
        try:
            turn = BotTurn.model_validate_json(raw)
        except ValidationError as error:
            self._text(422, f"the body is not a BotTurn:\n{error}\n")
            return
        try:
            play = self.server.bot.get()
            result = asyncio.run(run_turn(play, turn, self.server.client_logging))
        except Exception:  # noqa: BLE001 - the relaying client shows the traceback
            self._text(500, traceback.format_exc())
            return
        body = result.model_dump_json(by_alias=True).encode()
        self._send(200, body, "application/json")


def serve(args: argparse.Namespace) -> int:
    """Run the local bot server until interrupted."""
    bot = ReloadingPlay(args.bot)
    # A broken bot is reported on load and on each turn; fix it while serving.
    with contextlib.suppress(Exception):
        bot.get()
    server = BotServer((HOST, args.port), bot, [*DEFAULT_ORIGINS, *args.allow_origin])
    print(
        f"serving {args.bot} on http://{HOST}:{args.port} "
        f"for {', '.join(sorted(server.origins))}",
        flush=True,
    )
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


def fail(message: str) -> int:
    """Print a failure and return the exit status."""
    print(f"FAIL: {message}", file=sys.stderr)
    return 1


def check(args: argparse.Namespace) -> int:
    """Call the bot once on a fixture turn and report what is wrong with it."""
    turn = (
        BotTurn.model_validate_json(Path(args.turn).read_text())
        if args.turn
        else fixture_turn()
    )
    try:
        play = load_play(args.bot)
    except Exception:  # noqa: BLE001
        return fail(f"could not load {args.bot}:\n{traceback.format_exc()}")
    started = time.monotonic()
    try:
        result = asyncio.run(
            asyncio.wait_for(call_play(play, turn), timeout=CHECK_SECONDS)
        )
    except TimeoutError:
        # A sync bot runs in a thread that can't be stopped; don't wait for it.
        sys.stderr.write(
            f"FAIL: the bot took longer than {CHECK_SECONDS:g} s on one turn\n"
        )
        sys.stderr.flush()
        os._exit(1)
    except BotMemoryTooLargeError as error:
        return fail(f"memory is too large: {error.detail}")
    except InvalidBotResultError as error:
        return fail(f"the bot returned an invalid result: {error.detail}")
    except Exception:  # noqa: BLE001
        return fail(f"the bot raised an exception:\n{traceback.format_exc()}")
    elapsed = time.monotonic() - started
    memory_size = len(json.dumps(result.memory, separators=(",", ":")).encode())
    print(
        f"ok: {len(result.commands)} commands, {len(result.logs)} log lines, "
        f"memory {memory_size} of {MEMORY_LIMIT_BYTES} bytes, {elapsed:.2f} s"
    )
    return 0


def run(args: argparse.Namespace) -> int:
    """Play one seat of a lobby's game to its end, without a browser."""
    play = load_play(args.bot)

    def ended(outcome: GameOutcome, generation: Generation) -> None:
        print(f"game ended at generation {generation.id}: {outcome.summary}")

    async def main() -> None:
        client = IslandBattlesClient(
            args.base_url,
            args.token,
            args.lobby_id,
            play=play,
            on_game_ended=ended,
            log_level=getattr(logging, args.log_level.upper()),
            log_file=args.log_file,
        )
        await client.run()

    try:
        asyncio.run(main())
    except GameError as error:
        return fail(str(error))
    return 0


def print_fixture(_: argparse.Namespace) -> int:
    """Print the fixture turn that ``check`` uses, for ``curl -d @file``."""
    print(json.dumps(fixture_turn_data(), indent=2))
    return 0


def build_parser() -> argparse.ArgumentParser:
    """The command line of ``island-battles-bot``."""
    parser = argparse.ArgumentParser(prog="island-battles-bot", description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)

    serving = commands.add_parser(
        "serve", help=f"serve the bot on {HOST}:{DEFAULT_PORT} for a game client"
    )
    serving.add_argument("bot", help="module:function, for example mybot:play")
    serving.add_argument("--port", type=int, default=DEFAULT_PORT)
    serving.add_argument(
        "--allow-origin",
        action="append",
        default=[],
        metavar="ORIGIN",
        help="also answer this browser origin, such as https://game.example.org "
        f"(repeatable; always allowed: {', '.join(DEFAULT_ORIGINS)})",
    )
    serving.set_defaults(handler=serve)

    checking = commands.add_parser("check", help="call the bot once on a fixture turn")
    checking.add_argument("bot", help="module:function")
    checking.add_argument("--turn", help="a BotTurn JSON file instead of the fixture")
    checking.set_defaults(handler=check)

    fixture = commands.add_parser(
        "fixture", help="print the fixture BotTurn as JSON, for curl"
    )
    fixture.set_defaults(handler=print_fixture)

    running = commands.add_parser("run", help="play a seat without a browser")
    running.add_argument("bot", help="module:function")
    running.add_argument("--base-url", required=True)
    running.add_argument("--token", required=True, help="the guest bearer credential")
    running.add_argument("--lobby-id", required=True)
    running.add_argument("--log-file")
    running.add_argument(
        "--log-level", default="info", choices=("debug", "info", "warning", "error")
    )
    running.set_defaults(handler=run)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    """Entry point of the ``island-battles-bot`` script."""
    args = build_parser().parse_args(argv)
    return args.handler(args)


if __name__ == "__main__":
    sys.exit(main())
