"""Game data exposed to player strategies."""

from enum import StrEnum
from math import hypot
from typing import Final

from pydantic import BaseModel, ConfigDict, Field

NEUTRAL_TEAM_COLOR: Final[int] = 0
"""Team color for castles and soldiers that belong to no player team."""


class _FrozenModel(BaseModel):
    model_config = ConfigDict(frozen=True)


class TeamType(StrEnum):
    """Another entity's allegiance relative to this entity."""

    friendly = "friendly"
    neutral = "neutral"
    enemy = "enemy"


class BoardEntity(_FrozenModel):
    """A positioned team entity, shared by castles and soldier groups."""

    model_config = ConfigDict(strict=True, allow_inf_nan=False)

    team_color: int
    x: float
    y: float

    def distance(self, other: "Castle | SoldierGroup") -> float:
        """Return straight-line distance in map units."""
        return hypot(other.x - self.x, other.y - self.y)

    def compare_team(self, other: "Castle | SoldierGroup") -> TeamType:
        """Classify other; a neutral target is neutral even when self is neutral."""
        if other.team_color == NEUTRAL_TEAM_COLOR:
            return TeamType.neutral
        if self.team_color == other.team_color:
            return TeamType.friendly
        return TeamType.enemy


class Castle(BoardEntity):
    """A castle and its current occupants."""

    id: int
    soldiers: int


class SoldierGroup(BoardEntity):
    """A travelling group of soldiers."""

    id: int
    soldiers: int
    origin_castle_id: int
    destination_castle_id: int
    clash_radius: float
    destination_castle: Castle

    def distance_left(self) -> float:
        """Return map units to the destination, not travel time or generation count."""
        return self.distance(self.destination_castle)


class ClashParticipant(_FrozenModel):
    """One group's contact position and casualties for a recorded clash."""

    model_config = ConfigDict(strict=True, allow_inf_nan=False)

    group_id: int = Field(ge=0)
    team_color: int = Field(ge=0)
    x: float
    y: float
    clash_radius: float = Field(gt=0)
    soldiers_before: int = Field(ge=0)
    soldiers_after: int = Field(ge=0)


class Clash(_FrozenModel):
    """A combat event, with an ID scoped to its generation and a turn fraction."""

    model_config = ConfigDict(strict=True, allow_inf_nan=False)

    id: int = Field(gt=0)
    time: float = Field(ge=0, le=1)
    participants: list[ClashParticipant]


class Generation(_FrozenModel):
    """One board snapshot, without authentication or transport metadata."""

    id: int
    castles: list[Castle]
    soldier_groups: list[SoldierGroup]
    clashes: list[Clash] = Field(default_factory=list)
