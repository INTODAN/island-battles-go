"""The bot entry point: a function from ``BotTurn`` to ``BotTurnResult``.

A bot is stateless. It gets the turn's board and the ``memory`` it returned
last turn, and returns commands, game log lines and its new memory. The
helpers here build the pieces of a result; ``call_play`` and ``run_turn`` run
a bot the way the client, ``island-battles-bot serve`` and ``check`` do.
"""

from __future__ import annotations

import asyncio
import inspect
import json
import logging
from collections.abc import Awaitable, Callable, Iterable, Mapping
from typing import Any

from pydantic import ValidationError

from ._generated import (
    BotTurn,
    BotTurnResult,
    GenerationLogLine,
    PublicGenerationCommand,
    SendSoldierGroupCommand,
    SendSoldierGroupParameters,
    UpgradeCastleCommand,
    UpgradeCastleParameters,
    UpgradeType,
)
from .errors import BotMemoryTooLargeError, InvalidBotResultError
from .game_log import (
    PLAYER_LOGGER,
    ClientLogging,
    GameLog,
    LogLevel,
    clean,
    cut,
)

Play = Callable[[BotTurn], BotTurnResult | Awaitable[BotTurnResult]]
"""A bot: ``def play(turn)`` or ``async def play(turn)``."""

MEMORY_LIMIT_BYTES = 64 * 1024
"""The most memory a turn may return, as compact JSON."""

logger = logging.getLogger(PLAYER_LOGGER)
"""The bot's logger. Lines logged while a bot plays also go to the game log."""


def send_soldier_group(
    origin_castle_id: int, destination_castle_id: int, soldier_amount: int
) -> PublicGenerationCommand:
    """Build a ``send_soldier_group`` command; raises ``ValidationError`` if invalid."""
    return PublicGenerationCommand(
        SendSoldierGroupCommand(
            command_type="send_soldier_group",
            parameters=SendSoldierGroupParameters(
                origin_castle_id=origin_castle_id,
                destination_castle_id=destination_castle_id,
                soldier_amount=soldier_amount,
            ),
        )
    )


def upgrade_castle(
    castle_id: int, upgrade_type: UpgradeType | str
) -> PublicGenerationCommand:
    """Build an ``upgrade_castle`` command; raises ``ValidationError`` if invalid."""
    return PublicGenerationCommand(
        UpgradeCastleCommand(
            command_type="upgrade_castle",
            parameters=UpgradeCastleParameters(
                castle_id=castle_id, upgrade_type=upgrade_type
            ),
        )
    )


def log_line(
    message: str, level: LogLevel = "info", logger_name: str = PLAYER_LOGGER
) -> GenerationLogLine:
    """Build a game log line, cut to the server's limit and made storable."""
    return GenerationLogLine(
        level=level, logger=clean(logger_name[:100]), message=cut(clean(message))
    )


def turn_result(
    commands: Iterable[PublicGenerationCommand] = (),
    logs: Iterable[GenerationLogLine] = (),
    memory: Mapping[str, Any] | None = None,
) -> BotTurnResult:
    """Build a ``BotTurnResult``; every part is optional."""
    return BotTurnResult(commands=tuple(commands), logs=tuple(logs), memory=memory)


def check_memory(memory: Mapping[str, Any] | None) -> None:
    """Raise ``BotMemoryTooLargeError`` when memory is over 64 KiB as compact JSON."""
    if memory is None:
        return
    try:
        size = len(
            json.dumps(memory, separators=(",", ":"), ensure_ascii=False).encode(
                "utf-8", "surrogatepass"
            )
        )
    except (TypeError, ValueError) as error:
        raise InvalidBotResultError(f"memory is not JSON: {error}") from error
    if size > MEMORY_LIMIT_BYTES:
        raise BotMemoryTooLargeError(
            f"memory is {size} bytes as compact JSON; the limit is "
            f"{MEMORY_LIMIT_BYTES} (64 KiB)"
        )


async def call_play(play: Play, turn: BotTurn) -> BotTurnResult:
    """Call a bot once and return its valid result.

    A sync bot runs in a worker thread, so it can't block the event loop. The
    result must be a ``BotTurnResult`` (or a mapping that validates as one)
    with memory of at most 64 KiB; otherwise ``InvalidBotResultError`` or
    ``BotMemoryTooLargeError`` is raised. Exceptions from the bot pass through.
    """
    if inspect.iscoroutinefunction(play):
        result = await play(turn)
    else:
        result = await asyncio.to_thread(play, turn)
        if inspect.isawaitable(result):
            result = await result
    if isinstance(result, Mapping):
        try:
            result = BotTurnResult.model_validate(result)
        except ValidationError as error:
            raise InvalidBotResultError(f"invalid result: {error}") from error
    if not isinstance(result, BotTurnResult):
        raise InvalidBotResultError(
            f"play must return a BotTurnResult, not {type(result).__name__}"
        )
    check_memory(result.memory)
    return result


def add_result_lines(game_log: GameLog, result: BotTurnResult) -> None:
    """File the lines a result returns after those the bot logged while playing."""
    for line in result.logs:
        game_log.add(log_line(line.message, line.level, line.logger))


async def run_turn(
    play: Play, turn: BotTurn, client_logging: ClientLogging, stored: int = 0
) -> BotTurnResult:
    """Play one turn for a relaying caller, and return the result with its logs.

    The bot's logged lines and the lines it returned become the result's
    ``logs``, within the server's limits, counting ``stored`` lines already
    stored for the generation. Exceptions from the bot pass through.
    """
    game_log = GameLog(turn.generation.id, stored)
    with client_logging.scope(game_log):
        result = await call_play(play, turn)
        add_result_lines(game_log, result)
    lines, _ = game_log.batch(True)
    return result.model_copy(update={"logs": tuple(lines)})
