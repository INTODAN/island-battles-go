"""Immutable game data exposed to player strategies."""

from typing import Annotated, Final

from pydantic import BaseModel, ConfigDict, Field

NEUTRAL_TEAM_COLOR: Final[int] = 0
"""Team color for castles and soldiers that belong to no player team."""

_NonNegativeInt = Annotated[int, Field(strict=True, ge=0)]
_PositiveInt = Annotated[int, Field(strict=True, gt=0)]
_Coordinate = Annotated[float, Field(strict=True, allow_inf_nan=False)]


class _FrozenModel(BaseModel):
    model_config = ConfigDict(frozen=True)


class Castle(_FrozenModel):
    """A castle and its current occupants."""

    id: _NonNegativeInt
    team_color: _NonNegativeInt
    soldiers: _NonNegativeInt
    x: _Coordinate
    y: _Coordinate


class SoldierGroup(_FrozenModel):
    """A travelling group of soldiers."""

    id: _NonNegativeInt
    team_color: _NonNegativeInt
    soldiers: _NonNegativeInt
    origin_logical_castle_id: _NonNegativeInt | None = None
    destination_logical_castle_id: _NonNegativeInt | None = None
    clash_radius: (
        Annotated[float, Field(strict=True, ge=0, allow_inf_nan=False)] | None
    ) = None
    x: _Coordinate | None = None
    y: _Coordinate | None = None


class Generation(_FrozenModel):
    """One board snapshot, without authentication or transport metadata."""

    id: _PositiveInt
    castles: tuple[Castle, ...]
    soldier_groups: tuple[SoldierGroup, ...]


class SoldierGroupOrder(_FrozenModel):
    """Send soldiers between logical castle IDs; castle ID zero is valid."""

    origin_logical_castle_id: _NonNegativeInt
    destination_logical_castle_id: _NonNegativeInt
    soldier_amount: _PositiveInt
