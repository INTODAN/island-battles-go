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
import sysconfig
import threading
import time
import traceback
from collections.abc import Callable, Coroutine, Sequence
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
# page and the web client's dev server, the deployed game, and the desktop app
# (#105), whose web view sends its own origin: `tauri://localhost` on macOS and
# Linux, `http://tauri.localhost` on Windows. Add more with --allow-origin.
DEFAULT_ORIGINS = (
    "http://localhost:8888",
    "http://localhost:3000",
    "https://island-battles.elidan.org",
    "tauri://localhost",
    "http://tauri.localhost",
)
CHECK_SECONDS = 2.0
# The lobby's shortest turn timeout is 10 s; answer before the game moves on.
DEFAULT_TURN_TIMEOUT = 8.0
# A BotTurn of a busy 6-team generation with 64 KiB of memory is a few hundred
# KiB; 2 MiB leaves headroom.
MAX_BODY_BYTES = 2 * 1024 * 1024
# A refused request's body is read (up to this much) so closing the socket
# doesn't reset the connection before the client reads the refusal.
DRAIN_BYTES = 64 * 1024


def run_with_timeout[T](
    make: Callable[[], Coroutine[Any, Any, T]], seconds: float
) -> T:
    """Run a coroutine to the end in a daemon thread, or raise ``TimeoutError``.

    A bot that loops forever can't be stopped, but as the thread is a daemon
    it never blocks the process from exiting.
    """
    outcome: list[tuple[bool, Any]] = []

    def target() -> None:
        try:
            outcome.append((True, asyncio.run(make())))
        except BaseException as error:  # noqa: BLE001 - re-raised in the caller
            outcome.append((False, error))

    worker = threading.Thread(target=target, daemon=True, name="bot-turn")
    worker.start()
    worker.join(seconds)
    if not outcome:
        raise TimeoutError
    succeeded, value = outcome[0]
    if not succeeded:
        raise value
    return value


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


def _is_installed(file: Path) -> bool:
    """Whether a module file is in the standard library or site-packages."""
    resolved = file.resolve()
    if {"site-packages", "dist-packages"} & set(resolved.parts):
        return True
    paths = sysconfig.get_paths()
    return any(
        resolved.is_relative_to(Path(paths[key]).resolve())
        for key in ("stdlib", "platstdlib")
    )


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
        self._lock = threading.Lock()

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
        """Drop the bot module and the modules that sit beside it.

        Only modules whose file is directly in the bot's directory are
        dropped (never the standard library or site-packages, unless the bot
        itself lives there), so the next import reads the changed files. If
        the directory is a shared one, every module in it is imported again.
        """
        directory = self._directory()
        for name, module in list(sys.modules.items()):
            file = getattr(module, "__file__", None)
            if name == self._module_name or (
                directory is not None
                and file is not None
                and not _is_installed(Path(file))
                and Path(file).resolve().parent == directory
            ):
                del sys.modules[name]

    def get(self) -> Play:
        """The bot as of the files now; raises what importing it raised."""
        with self._lock:
            return self._get()

    def _get(self) -> Play:
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
        self,
        address: tuple[str, int],
        bot: ReloadingPlay,
        origins: Sequence[str],
        turn_timeout: float = DEFAULT_TURN_TIMEOUT,
    ) -> None:
        super().__init__(address, BotRequestHandler)
        self.bot = bot
        self.turn_timeout = turn_timeout
        self.origins = frozenset(origin.rstrip("/") for origin in origins)
        self.client_logging = ClientLogging(self, logging.INFO, True, None)
        self.client_logging.attach()

    def server_close(self) -> None:
        super().server_close()
        self.client_logging.detach()


class BotRequestHandler(BaseHTTPRequestHandler):
    """``POST /turn``, ``GET /health`` and the CORS preflights for them.

    One request per connection: HTTP/1.0 closes after each response, and every
    refusal also sends ``Connection: close``. Never keep a connection after a
    refusal, or the unread body of a cross-origin POST could be parsed as the
    next request, without an Origin header.
    """

    server: BotServer
    protocol_version = "HTTP/1.0"
    timeout = 10
    _body_read = False

    def log_message(self, format: str, *args: Any) -> None:
        print(f"{self.address_string()} {format % args}", flush=True)

    def _origin_allowed(self) -> bool | None:
        """None without an Origin header (curl), else whether the origin may relay."""
        origin = self.headers.get("Origin")
        if origin is None:
            return None
        return origin.rstrip("/") in self.server.origins

    def _host_allowed(self) -> bool:
        """Only the bot's own address as the Host, against DNS rebinding."""
        port = self.server.server_address[1]
        return self.headers.get("Host") in (f"127.0.0.1:{port}", f"localhost:{port}")

    def _send(self, status: int, body: bytes, content_type: str) -> None:
        if status >= 400:
            self.close_connection = True
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.send_header("Vary", "Origin")
        if self._origin_allowed() is True:
            self.send_header("Access-Control-Allow-Origin", self.headers["Origin"])
        self.end_headers()
        self.wfile.write(body)
        self.close_connection = True

    def _text(self, status: int, text: str) -> None:
        if status >= 400:
            self._drain()
        self._send(status, text.encode("utf-8", "replace"), "text/plain; charset=utf-8")

    def _drain(self) -> None:
        """Discard a small request body, so closing doesn't reset the connection."""
        self.close_connection = True
        if self._body_read:
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if 0 < length <= DRAIN_BYTES:
                self.rfile.read(length)
        except ValueError, OSError:
            pass

    def _refuse(self) -> bool:
        """Refuse a request for another Host or from an origin that may not relay."""
        if not self._host_allowed():
            self._text(
                403,
                f"Host {self.headers.get('Host')} is not allowed; "
                f"use 127.0.0.1:{self.server.server_address[1]} or "
                f"localhost:{self.server.server_address[1]}\n",
            )
            return True
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
        if not self._host_allowed() or self._origin_allowed() is not True:
            self._text(403, "origin or host not allowed\n")
            return
        self.close_connection = True
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
        self.send_header("Connection", "close")
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
        media_type = self.headers.get("Content-Type", "").partition(";")[0]
        if media_type.strip().lower() != "application/json":
            self._text(415, "Content-Type must be application/json\n")
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
        self._body_read = True
        try:
            turn = BotTurn.model_validate_json(raw)
        except ValidationError as error:
            self._text(422, f"the body is not a BotTurn:\n{error}\n")
            return
        try:
            play = self.server.bot.get()
            result = run_with_timeout(
                lambda: run_turn(play, turn, self.server.client_logging),
                self.server.turn_timeout,
            )
        except TimeoutError:
            self._text(
                504,
                f"the bot took longer than {self.server.turn_timeout:g} s on this "
                "turn; it may be in an endless loop. Fix it and save; the "
                "server keeps serving.\n",
            )
            return
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
    server = BotServer(
        (HOST, args.port),
        bot,
        [*DEFAULT_ORIGINS, *args.allow_origin],
        args.turn_timeout,
    )
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
        result = run_with_timeout(lambda: call_play(play, turn), CHECK_SECONDS)
    except TimeoutError:
        # The bot's thread can't be stopped, and a daemon thread would still
        # be joined by a normal exit path in some cases; leave at once.
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
    memory_size = len(
        json.dumps(result.memory, separators=(",", ":"), ensure_ascii=False).encode(
            "utf-8", "surrogatepass"
        )
    )
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
    serving.add_argument(
        "--turn-timeout",
        type=float,
        default=DEFAULT_TURN_TIMEOUT,
        metavar="SECONDS",
        help="answer 504 when a turn takes longer than this (default "
        f"{DEFAULT_TURN_TIMEOUT:g}, under the lobby's shortest turn timeout of 10)",
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
