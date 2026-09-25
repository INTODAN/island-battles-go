"""The strategy interface, independent of the SDK's transport and lifecycle."""

from abc import ABC, abstractmethod

from .commands import CommandReceiver
from .models import Generation


class Player(ABC):
    """Base for strategies, with shared team-color initialization."""

    def __init__(self, team_color: int) -> None:
        self.team_color = team_color

    @abstractmethod
    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None: ...
