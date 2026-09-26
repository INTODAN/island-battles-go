from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar

from attrs import define as _attrs_define

from ..models.game_next_generation_schema_version import GameNextGenerationSchemaVersion
from ..models.game_next_generation_type import GameNextGenerationType

if TYPE_CHECKING:
    from ..models.generation_state import GenerationState


T = TypeVar("T", bound="GameNextGeneration")


@_attrs_define
class GameNextGeneration:
    """
    Attributes:
        schema_version (GameNextGenerationSchemaVersion):
        state (GenerationState):
        type_ (GameNextGenerationType):
    """

    schema_version: GameNextGenerationSchemaVersion
    state: GenerationState
    type_: GameNextGenerationType

    def to_dict(self) -> dict[str, Any]:
        schema_version = self.schema_version.value

        state = self.state.to_dict()

        type_ = self.type_.value

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "schema_version": schema_version,
                "state": state,
                "type": type_,
            }
        )

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.generation_state import GenerationState

        d = dict(src_dict)
        schema_version = GameNextGenerationSchemaVersion(d.pop("schema_version"))

        state = GenerationState.from_dict(d.pop("state"))

        type_ = GameNextGenerationType(d.pop("type"))

        game_next_generation = cls(
            schema_version=schema_version,
            state=state,
            type_=type_,
        )

        return game_next_generation
