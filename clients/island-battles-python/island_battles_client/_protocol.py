"""Validate the exported contract and adapt generated wire models for strategies."""

import json
import math
from dataclasses import dataclass
from functools import lru_cache
from importlib.resources import files
from typing import Self

from island_game_api_client.models import GameNextGeneration
from island_game_api_client.models import GenerationState as WireGenerationState
from island_game_api_client.types import Unset
from jsonschema import Draft202012Validator, FormatChecker, ValidationError, validators

from .errors import GameProtocolError
from .models import Castle, Clash, Generation, SoldierGroup

# The generator's attrs models deserialize but do not enforce JSON Schema bounds.
# Use the exported contract, bundled by generation, instead of duplicating DTOs.
_CONTRACT = json.loads(
    files("island_game_api_client").joinpath("openapi.json").read_text()
)
_StrictValidator = validators.extend(
    Draft202012Validator,
    type_checker=Draft202012Validator.TYPE_CHECKER.redefine_many(
        {
            "integer": lambda checker, value: type(value) is int,
            "number": lambda checker, value: (
                type(value) is int or (type(value) is float and math.isfinite(value))
            ),
        }
    ),
)


@lru_cache
def _validator(schema_name: str):
    return _StrictValidator(
        {
            "$ref": f"#/components/schemas/{schema_name}",
            "components": _CONTRACT["components"],
        },
        format_checker=FormatChecker(),
    )


def validate_wire(data: object, schema_name: str) -> None:
    try:
        _validator(schema_name).validate(data)
    except ValidationError as exc:
        raise GameProtocolError(f"invalid {schema_name} response") from exc


@dataclass(frozen=True)
class GenerationState:
    """Private adapter around the generated state; no handwritten wire fields."""

    wire: WireGenerationState

    @classmethod
    def parse(cls, data: object) -> Self:
        validate_wire(data, "GenerationState")
        assert isinstance(data, dict)
        return cls(WireGenerationState.from_dict(data))

    @classmethod
    def parse_event(cls, data: object) -> Self:
        validate_wire(data, "GameNextGeneration")
        assert isinstance(data, dict)
        return cls(GameNextGeneration.from_dict(data).state)

    @property
    def game_id(self) -> int:
        return self.wire.game_id

    @property
    def lobby_id(self) -> str:
        return str(self.wire.lobby_id)

    @property
    def generation_id(self) -> int:
        return self.wire.generation_id

    @property
    def player_id(self) -> str:
        return str(self.wire.player_id)

    @property
    def team_color(self) -> int:
        return self.wire.team_color

    @property
    def channel(self) -> str:
        return self.wire.channel

    @property
    def submitted(self) -> bool:
        return self.wire.submitted

    def public_generation(self) -> Generation:
        castles = [
            Castle.model_validate(castle.to_dict())
            for castle in self.wire.generation.castles
        ]
        castles_by_id = {castle.id: castle for castle in castles}
        soldier_groups = []
        for group in self.wire.generation.soldier_groups:
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
        clashes = self.wire.generation.clashes
        return Generation(
            id=self.generation_id,
            castles=castles,
            soldier_groups=soldier_groups,
            clashes=[]
            if isinstance(clashes, Unset)
            else [Clash.model_validate(clash.to_dict()) for clash in clashes],
        )
