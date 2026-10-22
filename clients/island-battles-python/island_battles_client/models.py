"""Game data exposed to player strategies.

Fields and validation are generated from ``backend/api/openapi.json``; the
distance and team utilities are inherited from ``_entities``.
"""

from ._entities import NEUTRAL_TEAM_COLOR, BoardEntity, TeamType
from ._generated import Castle, Clash, ClashParticipant, Generation, SoldierGroup

__all__ = [
    "NEUTRAL_TEAM_COLOR",
    "BoardEntity",
    "Castle",
    "Clash",
    "ClashParticipant",
    "Generation",
    "SoldierGroup",
    "TeamType",
]
