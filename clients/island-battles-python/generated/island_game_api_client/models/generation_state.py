from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar
from uuid import UUID

from attrs import define as _attrs_define

from ..types import UNSET, Unset

if TYPE_CHECKING:
    from ..models.generation_view import GenerationView


T = TypeVar("T", bound="GenerationState")


@_attrs_define
class GenerationState:
    """
    Attributes:
        channel (str):
        game_id (int):
        generation (GenerationView):
        generation_id (int):
        lobby_id (UUID):
        player_id (UUID):
        submitted (bool):
        team_color (int):
        schema (str | Unset): A URL to the JSON Schema for this object.
    """

    channel: str
    game_id: int
    generation: GenerationView
    generation_id: int
    lobby_id: UUID
    player_id: UUID
    submitted: bool
    team_color: int
    schema: str | Unset = UNSET

    def to_dict(self) -> dict[str, Any]:
        channel = self.channel

        game_id = self.game_id

        generation = self.generation.to_dict()

        generation_id = self.generation_id

        lobby_id = str(self.lobby_id)

        player_id = str(self.player_id)

        submitted = self.submitted

        team_color = self.team_color

        schema = self.schema

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "channel": channel,
                "game_id": game_id,
                "generation": generation,
                "generation_id": generation_id,
                "lobby_id": lobby_id,
                "player_id": player_id,
                "submitted": submitted,
                "team_color": team_color,
            }
        )
        if schema is not UNSET:
            field_dict["$schema"] = schema

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.generation_view import GenerationView

        d = dict(src_dict)
        channel = d.pop("channel")

        game_id = d.pop("game_id")

        generation = GenerationView.from_dict(d.pop("generation"))

        generation_id = d.pop("generation_id")

        lobby_id = UUID(d.pop("lobby_id"))

        player_id = UUID(d.pop("player_id"))

        submitted = d.pop("submitted")

        team_color = d.pop("team_color")

        schema = d.pop("$schema", UNSET)

        generation_state = cls(
            channel=channel,
            game_id=game_id,
            generation=generation,
            generation_id=generation_id,
            lobby_id=lobby_id,
            player_id=player_id,
            submitted=submitted,
            team_color=team_color,
            schema=schema,
        )

        return generation_state
