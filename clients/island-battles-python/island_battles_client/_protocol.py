"""Private wire envelopes; player callbacks receive only game data."""

from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field, StrictBool, ValidationError

from .errors import GameProtocolError
from .models import Castle, Generation, SoldierGroup

_PositiveInt = Annotated[int, Field(strict=True, gt=0)]
_Identifier = Annotated[str, Field(strict=True, min_length=1)]


class _GenerationContents(BaseModel):
    model_config = ConfigDict(frozen=True)

    castles: tuple[Castle, ...]
    soldier_groups: tuple[SoldierGroup, ...]


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
        return Generation(
            id=self.generation_id,
            castles=self.generation.castles,
            soldier_groups=self.generation.soldier_groups,
        )
