"""The bot's logger: stdout, an optional file, and the game log.

A strategy logs with ``self.logger``, the standard ``logging.Logger`` named
``island_battles.player``. Every client attaches its handlers to the
``island_battles`` logger, so the SDK's own ``island_battles.sdk`` records
reach stdout and the file too. The game-log handler files each record under
the generation whose callback is running, through a context variable that the
client sets around its callbacks, so two clients in one process never mix
their lines. Records logged outside a callback go to stdout and the file only.
"""

from __future__ import annotations

import logging
import re
import sys
import threading
import traceback
from collections.abc import Iterator
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import dataclass
from os import PathLike
from typing import Literal

from ._generated import GenerationLogLine

PLAYER_LOGGER = "island_battles.player"
SDK_LOGGER = "island_battles.sdk"
# The parent of both loggers, which the clients attach their handlers to.
ROOT_LOGGER = "island_battles"

# The server's limits, per player per generation.
MAX_LINES = 100
MAX_MESSAGE_LENGTH = 500
MAX_LOGGER_LENGTH = 100
CUT_NOTE = f"… (cut at {MAX_MESSAGE_LENGTH} characters)"
# NUL and the surrogate code points, which stored text can't hold.
_UNSTORABLE = re.compile("[\x00\ud800-\udfff]")

# Marks the SDK's own reports about the game log, which go to stdout and the
# file but never into the game log itself.
_NOT_FOR_GAME_LOG = "island_battles_not_for_game_log"
_FORMAT = "%(asctime)s %(levelname)s %(name)s: %(message)s"

_sdk_logger = logging.getLogger(SDK_LOGGER)

LogLevel = Literal["debug", "info", "warning", "error"]


@dataclass(frozen=True)
class _LogScope:
    """The client whose code is running, and the generation it plays, if any."""

    owner: object
    game_log: GameLog | None


_scope: ContextVar[_LogScope | None] = ContextVar(
    "island_battles_log_scope", default=None
)


def cut(message: str) -> str:
    """Fit a message into one stored line, saying where it was cut."""
    if len(message) <= MAX_MESSAGE_LENGTH:
        return message
    return message[: MAX_MESSAGE_LENGTH - len(CUT_NOTE)] + CUT_NOTE


def _clean(text: str) -> str:
    """Make text storable, replacing what UTF-8 text can't hold with U+FFFD.

    PostgreSQL text holds no NUL, and a surrogate code point, which Python
    strings may carry alone (for example from ``errors="surrogateescape"``),
    has no UTF-8 encoding.
    """
    return _UNSTORABLE.sub("\ufffd", text)


def _level(record: logging.LogRecord) -> LogLevel:
    if record.levelno >= logging.ERROR:
        return "error"
    if record.levelno >= logging.WARNING:
        return "warning"
    if record.levelno >= logging.INFO:
        return "info"
    return "debug"


class GameLog:
    """One generation's game log lines that the client has not sent yet.

    Lines are sent as they come until the player has 99 stored or pending,
    counting the ``stored`` lines an earlier run already stored. The next line
    is held back until the turn's last request: it is sent if no more lines
    came, and otherwise replaced by an error that counts every dropped line,
    so the player never passes the server's limit. With 100 lines already
    stored, nothing more is sent. The handler adds lines from any thread, and
    the client takes them under its command lock.
    """

    def __init__(self, generation_id: int, stored: int = 0) -> None:
        self.generation_id = generation_id
        # Reentrant, so commit can seal under it.
        self._lock = threading.RLock()
        self._pending: list[GenerationLogLine] = []
        self._accepted = stored
        self._held: GenerationLogLine | None = None
        self._dropped = 0
        self._sealed = False

    def add(self, line: GenerationLogLine) -> bool:
        """Keep a line for the next request; True when it is the first one dropped."""
        with self._lock:
            if self._sealed:
                return False
            if self._accepted < MAX_LINES - 1:
                self._pending.append(line)
                self._accepted += 1
                return False
            if (
                self._accepted == MAX_LINES - 1
                and self._held is None
                and not self._dropped
            ):
                self._held = line
                return False
            first = not self._dropped
            # The held line is dropped with the first line past the limit.
            self._dropped += 2 if self._held is not None else 1
            self._held = None
            return first

    def batch(self, last: bool) -> tuple[list[GenerationLogLine], int]:
        """The lines for the next request, and how many pending lines they include.

        The turn's last request also carries the held line, or the limit error
        in its place when there is room for it. Nothing is removed until
        ``commit``, so a rejected request leaves the lines for the next one.
        """
        with self._lock:
            if self._sealed:
                return [], 0
            lines = list(self._pending)
            count = len(lines)
            if last and self._held is not None:
                lines.append(self._held)
            elif last and self._dropped and self._accepted < MAX_LINES:
                lines.append(
                    GenerationLogLine(
                        level="error",
                        logger=SDK_LOGGER,
                        message=limit_error(self.generation_id, self._dropped),
                    )
                )
            return lines, count

    def commit(self, count: int, last: bool) -> None:
        """Forget the lines that a request sent; after the last one, take no more."""
        with self._lock:
            del self._pending[:count]
            if last:
                self.seal()

    def seal(self) -> None:
        """Send nothing more for this generation."""
        with self._lock:
            self._sealed = True
            self._pending.clear()
            self._held = None
            self._dropped = 0


def limit_error(generation_id: int, dropped: int | None = None) -> str:
    """The limit error, with the final count of dropped lines once it is known."""
    rest = (
        f"{dropped} lines were not stored"
        if dropped is not None
        else "the rest of this generation's lines are not stored"
    )
    return (
        f"game log limit reached for generation {generation_id}: "
        f"{MAX_LINES} lines per generation; {rest}"
    )


def report_server_limit(generation_id: int, detail: str) -> None:
    """Write to stdout and the file that the server refused a batch's lines."""
    _sdk_logger.error(
        "%s; the server refused the lines: %s",
        limit_error(generation_id),
        detail,
        extra={_NOT_FOR_GAME_LOG: True},
    )


class _OwnerFilter(logging.Filter):
    """Pass records of one client's code, and records logged outside any client."""

    def __init__(self, owner: object) -> None:
        super().__init__()
        self.owner = owner

    def filter(self, record: logging.LogRecord) -> bool:
        scope = _scope.get()
        return scope is None or scope.owner is self.owner


class _GameLogFormatter(logging.Formatter):
    """Formats a stored line so that cutting it keeps what matters most.

    An exception comes right after the message, followed by its frames from
    the innermost out, so a cut traceback still names the error and where it
    was raised.
    """

    def format(self, record: logging.LogRecord) -> str:
        # Unlike logging.Formatter, never reuse record.exc_text: the stdout
        # and file handlers cache their own traceback layout there.
        message = record.getMessage()
        if record.exc_info and record.exc_info[1] is not None:
            message += "\n" + self.formatException(record.exc_info)
        if record.stack_info:
            message += "\n" + self.formatStack(record.stack_info)
        return message

    def formatException(self, ei) -> str:
        exception = traceback.TracebackException(*ei)
        summary = "".join(exception.format_exception_only()).rstrip("\n")
        frames = "".join(reversed(exception.stack.format())).rstrip("\n")
        if not frames:
            return summary
        return f"{summary}\nTraceback (most recent call first):\n{frames}"


class _GameLogHandler(logging.Handler):
    """Files a client's records under the generation of the running callback."""

    def __init__(self, owner: object, level: int) -> None:
        super().__init__(level)
        self.owner = owner
        self.setFormatter(_GameLogFormatter())

    def emit(self, record: logging.LogRecord) -> None:
        scope = _scope.get()
        if (
            scope is None
            or scope.owner is not self.owner
            or scope.game_log is None
            or getattr(record, _NOT_FOR_GAME_LOG, False)
        ):
            return
        try:
            message = cut(_clean(self.format(record)))
            if not message:
                return
            line = GenerationLogLine(
                level=_level(record),
                logger=_clean(record.name[:MAX_LOGGER_LENGTH]),
                message=message,
            )
            if scope.game_log.add(line):
                # Once per generation, as soon as a line is dropped. The final
                # count is stored with the turn's last request.
                _sdk_logger.error(
                    limit_error(scope.game_log.generation_id),
                    extra={_NOT_FOR_GAME_LOG: True},
                )
        except Exception:  # noqa: BLE001 - a logging call never raises into the bot
            self.handleError(record)


# The clients attached to the island_battles logger, and its level before the
# first of them lowered it.
_attached: set[ClientLogging] = set()
_level_before_clients = logging.NOTSET
_attach_lock = threading.Lock()


class ClientLogging:
    """One client's handlers on the ``island_battles`` logger."""

    def __init__(
        self,
        owner: object,
        level: int,
        to_stdout: bool,
        file: str | PathLike[str] | None,
    ) -> None:
        self.owner = owner
        self.level = level
        self.to_stdout = to_stdout
        self.file = file
        self._handlers: list[logging.Handler] = []

    def attach(self) -> None:
        """Attach stdout and file handlers as configured, and always the game log."""
        if self._handlers:
            return
        handlers: list[logging.Handler] = []
        if self.to_stdout:
            handlers.append(logging.StreamHandler(sys.stdout))
        if self.file is not None:
            handlers.append(logging.FileHandler(self.file, encoding="utf-8"))
        for handler in handlers:
            handler.setLevel(self.level)
            handler.setFormatter(logging.Formatter(_FORMAT))
        handlers.append(_GameLogHandler(self.owner, self.level))
        owner_filter = _OwnerFilter(self.owner)
        root = logging.getLogger(ROOT_LOGGER)
        global _level_before_clients
        with _attach_lock:
            if not _attached:
                _level_before_clients = root.level
            _attached.add(self)
            for handler in handlers:
                handler.addFilter(owner_filter)
                root.addHandler(handler)
            # Each handler applies its client's level; the logger passes the
            # lowest level any client asked for.
            if root.level == logging.NOTSET or root.level > self.level:
                root.setLevel(self.level)
        self._handlers = handlers

    def detach(self) -> None:
        """Remove and close this client's handlers."""
        root = logging.getLogger(ROOT_LOGGER)
        with _attach_lock:
            for handler in self._handlers:
                root.removeHandler(handler)
                handler.close()
            if self in _attached:
                _attached.discard(self)
                if not _attached:
                    root.setLevel(_level_before_clients)
        self._handlers = []

    @contextmanager
    def scope(self, game_log: GameLog | None = None) -> Iterator[None]:
        """Route records to this client, and into game_log when it is given."""
        token = _scope.set(_LogScope(self.owner, game_log))
        try:
            yield
        finally:
            _scope.reset(token)
