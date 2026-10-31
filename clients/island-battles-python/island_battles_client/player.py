"""The strategy interface, independent of the SDK's transport and lifecycle."""

import logging
from abc import ABC, abstractmethod
from typing import ClassVar

from .commands import CommandReceiver
from .game_log import PLAYER_LOGGER
from .models import GameOutcome, Generation


class Player(ABC):
    """Base for strategies, with shared team-color initialization."""

    logger: ClassVar[logging.Logger] = logging.getLogger(PLAYER_LOGGER)
    """The bot's logger. Lines logged during ``on_generation`` also go to the game log."""

    def __init__(self, team_color: int) -> None:
        self.team_color = team_color

    @abstractmethod
    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None: ...

    async def on_game_ended(self, outcome: GameOutcome, generation: Generation) -> None:
        """Called once with the outcome and the final board; the default ignores it."""
