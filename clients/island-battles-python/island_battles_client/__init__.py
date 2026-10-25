"""Async Island Battles runtime and its player-facing interfaces."""

from .client import IslandBattlesClient
from .commands import CommandReceiver
from .errors import (
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameProtocolError,
    GameReceiverClosedError,
    GameTransportError,
)
from .models import (
    NEUTRAL_TEAM_COLOR,
    BoardEntity,
    Castle,
    Clash,
    ClashParticipant,
    GameOutcome,
    Generation,
    SoldierGroup,
    TeamType,
)
from .player import Player

__all__ = [
    "NEUTRAL_TEAM_COLOR",
    "BoardEntity",
    "Castle",
    "Clash",
    "ClashParticipant",
    "CommandReceiver",
    "GameAuthenticationError",
    "GameClientClosedError",
    "GameError",
    "GameOutcome",
    "GameProtocolError",
    "GameReceiverClosedError",
    "GameTransportError",
    "Generation",
    "IslandBattlesClient",
    "Player",
    "SoldierGroup",
    "TeamType",
]
