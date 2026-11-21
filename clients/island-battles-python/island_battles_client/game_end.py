"""The end of a game: a bot's optional ``game_ended`` function and its demo.

When a game gets its outcome, however it ended, the bot's ``GameEndedDelivery``
says how: the outcome, the final board as the bot's team saw it, the bot's
last memory, and a link to download the game's demo (#413). A bot gets it by
defining a function named ``game_ended`` next to ``play`` in the module that
``module:function`` names::

    def game_ended(delivery: GameEndedDelivery) -> None:
        Path(f"game-{delivery.game_id}.json").write_bytes(download_demo(delivery))

``serve``, the worker that the server's runners start, and ``run`` call it
alike. It is optional: a bot without one plays as before.
"""

from __future__ import annotations

import asyncio
import gzip
import inspect
from collections.abc import Awaitable, Callable
from os import PathLike
from pathlib import Path
from types import ModuleType

import httpx

from ._generated import GameEndedDelivery
from .errors import GameError, GameTransportError

GameEnded = Callable[[GameEndedDelivery], Awaitable[None] | None]
"""A bot's game-end function: ``def game_ended(delivery)`` or ``async def``."""

GAME_ENDED_FUNCTION = "game_ended"
"""The name of the game-end function in a bot's module."""

DEMO_TIMEOUT_SECONDS = 60.0
"""How long ``download_demo`` waits for the demo by default."""

_GZIP_MAGIC = b"\x1f\x8b"


def module_game_ended(module: ModuleType) -> GameEnded | None:
    """The module's ``game_ended`` function, or None when it has none.

    Anything named ``game_ended`` that isn't callable, such as a flag, is
    not a game-end function.
    """
    function = getattr(module, GAME_ENDED_FUNCTION, None)
    return function if callable(function) else None


async def call_game_ended(game_ended: GameEnded, delivery: GameEndedDelivery) -> None:
    """Call a bot's game-end function once.

    A sync function runs in a worker thread, so it can't block the event
    loop; an async one is awaited. Exceptions from the bot pass through.
    """
    if inspect.iscoroutinefunction(game_ended):
        await game_ended(delivery)
        return
    returned = await asyncio.to_thread(game_ended, delivery)
    if inspect.isawaitable(returned):
        await returned


def download_demo(
    delivery: GameEndedDelivery,
    path: str | PathLike[str] | None = None,
    *,
    timeout: float = DEMO_TIMEOUT_SECONDS,
) -> bytes:
    """Download the game's demo, the ``Demo`` JSON file, from ``delivery.demo.url``.

    The link carries its own read-only token, so the request is a plain GET
    with no credentials. A gzip response is unpacked. When ``path`` is given
    the file is also written there. Returns the JSON bytes, which
    ``Demo.model_validate_json`` reads. The link works until
    ``delivery.demo.expires_at``.

    This call blocks: from an ``async def game_ended``, run it with
    ``await asyncio.to_thread(download_demo, delivery)``. Raises ``GameError``
    with the HTTP status when the game refuses the download, and
    ``GameTransportError`` when the game doesn't answer.
    """
    try:
        response = httpx.get(
            str(delivery.demo.url), timeout=timeout, follow_redirects=False
        )
    except httpx.HTTPError as error:
        raise GameTransportError(f"could not download the demo: {error}") from error
    if response.status_code != 200:
        try:
            problem = response.json()
        except ValueError:
            problem = {}
        detail = problem.get("detail") if isinstance(problem, dict) else None
        if not isinstance(detail, str) or not detail:
            detail = f"the demo download failed ({response.status_code})"
        error_class = GameTransportError if response.status_code >= 500 else GameError
        raise error_class(detail, response.status_code)
    # httpx unpacks Content-Encoding: gzip itself; a gzip file sent as it is
    # is unpacked here. JSON never starts with the gzip magic bytes.
    data = response.content
    if data.startswith(_GZIP_MAGIC):
        data = gzip.decompress(data)
    if path is not None:
        Path(path).write_bytes(data)
    return data
