"""Private wire envelopes; player callbacks receive only game data."""

from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, StrictBool, ValidationError

from .errors import GameProtocolError
from .models import Castle, Generation, SoldierGroup

_PositiveInt = Annotated[int, Field(strict=True, gt=0)]
_Identifier = Annotated[str, Field(strict=True, min_length=1)]


class _SoldierGroupContents(BaseModel):
    """Wire group data references its destination by ID only."""

    model_config = ConfigDict(frozen=True, strict=True, allow_inf_nan=False)

    id: int
    team_color: int
    soldiers: int
    x: float
    y: float
    origin_persistent_castle_id: int
    destination_persistent_castle_id: int
    clash_radius: float


class _GenerationContents(BaseModel):
    model_config = ConfigDict(frozen=True)

    castles: list[Castle]
    soldier_groups: list[_SoldierGroupContents]


class GenerationState(BaseModel):
    model_config = ConfigDict(frozen=True)

    game_id: _PositiveInt
    lobby_id: _Identifier
    generation_id: _PositiveInt
    player_id: _Identifier
    team_color: _PositiveInt
    channel: _Identifier
    submitted: StrictBool
    generation: _GenerationContents

    @classmethod
    def parse(cls, data: object) -> GenerationState:
        try:
            return cls.model_validate(data)
        except ValidationError as exc:
            raise GameProtocolError("invalid generation state") from exc

    def public_generation(self) -> Generation:
        castles_by_id = {castle.id: castle for castle in self.generation.castles}
        soldier_groups = []
        for group in self.generation.soldier_groups:
            destination = castles_by_id.get(group.destination_persistent_castle_id)
            if destination is None:
                raise GameProtocolError("soldier group destination castle is missing")
            soldier_groups.append(
                SoldierGroup(
                    id=group.id,
                    team_color=group.team_color,
                    soldiers=group.soldiers,
                    x=group.x,
                    y=group.y,
                    origin_castle_id=group.origin_persistent_castle_id,
                    destination_castle_id=group.destination_persistent_castle_id,
                    clash_radius=group.clash_radius,
                    destination_castle=destination,
                )
            )
        return Generation(
            id=self.generation_id,
            castles=self.generation.castles,
            soldier_groups=soldier_groups,
        )
