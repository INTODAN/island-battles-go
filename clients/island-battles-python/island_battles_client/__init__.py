"""Async Island Battles runtime and its player-facing interfaces."""

from .client import IslandBattlesClient
from .commands import CommandReceiver
from .errors import (
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameProtocolError,
    GameTransportError,
)
from .models import (
    NEUTRAL_TEAM_COLOR,
    BoardEntity,
    Castle,
    Generation,
    SoldierGroup,
    TeamType,
)
from .player import Player

__all__ = [
    "NEUTRAL_TEAM_COLOR",
    "BoardEntity",
    "Castle",
    "CommandReceiver",
    "GameAuthenticationError",
    "GameClientClosedError",
    "GameError",
    "GameProtocolError",
    "GameTransportError",
    "Generation",
    "IslandBattlesClient",
    "Player",
    "SoldierGroup",
    "TeamType",
]
