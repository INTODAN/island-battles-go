"""Behavior for the generated models; their fields come from the OpenAPI document.

``scripts/generate.py`` makes every generated model extend ``GameModel`` and maps
castles, soldier groups and generations onto the classes below. Keep this module
free of fields so the schema stays the only source of wire data.
"""

from __future__ import annotations

from collections.abc import Sequence
from enum import StrEnum
from math import hypot
from typing import TYPE_CHECKING, Any, Final, Self

from pydantic import (
    BaseModel,
    ConfigDict,
    PrivateAttr,
    field_validator,
    model_validator,
)

from .errors import GameError

if TYPE_CHECKING:
    from ._generated import Castle, SoldierGroup

NEUTRAL_TEAM_COLOR: Final[int] = 0
"""Team color for castles and soldiers that belong to no player team."""


class TeamType(StrEnum):
    """Another entity's allegiance relative to this entity."""

    friendly = "friendly"
    neutral = "neutral"
    enemy = "enemy"


class GameModel(BaseModel):
    """Immutable snapshot; nested sequences are stored as tuples."""

    model_config = ConfigDict(frozen=True, allow_inf_nan=False)

    @field_validator("*", mode="after")
    @classmethod
    def _freeze_sequences(cls, value: Any) -> Any:
        return tuple(value) if isinstance(value, list) else value


class BoardEntity(GameModel):
    """A positioned team entity, shared by castles and soldier groups."""

    if TYPE_CHECKING:
        team_color: int
        x: float
        y: float

    def distance(self, other: BoardEntity) -> float:
        """Return straight-line distance in map units."""
        return hypot(other.x - self.x, other.y - self.y)

    def compare_team(self, other: BoardEntity) -> TeamType:
        """Classify other; a neutral target is neutral even when self is neutral."""
        if other.team_color == NEUTRAL_TEAM_COLOR:
            return TeamType.neutral
        if self.team_color == other.team_color:
            return TeamType.friendly
        return TeamType.enemy


class SoldierGroupBase(BoardEntity):
    """Links a group to its destination when its generation is validated."""

    _destination_castle: Castle | None = PrivateAttr(default=None)

    if TYPE_CHECKING:
        destination_castle_id: int

    @property
    def destination_castle(self) -> Castle:
        """The castle this group is marching to, from the same generation."""
        if self._destination_castle is None:
            raise GameError("soldier group was not loaded as part of a generation")
        return self._destination_castle

    def distance_left(self) -> float:
        """Return map units to the destination, not travel time or generation count."""
        return self.distance(self.destination_castle)


class GenerationBase(GameModel):
    """Rejects groups whose destination castle is not in the same snapshot."""

    if TYPE_CHECKING:
        castles: Sequence[Castle]
        soldier_groups: Sequence[SoldierGroup]

    @model_validator(mode="after")
    def _link_destinations(self) -> Self:
        castles = {castle.id: castle for castle in self.castles}
        for group in self.soldier_groups:
            destination = castles.get(group.destination_castle_id)
            if destination is None:
                raise ValueError("soldier group destination castle is missing")
            group._destination_castle = destination
        return self
