from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar
from uuid import UUID

from attrs import define as _attrs_define

from ..types import UNSET, Unset

if TYPE_CHECKING:
    from ..models.public_generation_command import PublicGenerationCommand


T = TypeVar("T", bound="RegisterGenerationCommands")


@_attrs_define
class RegisterGenerationCommands:
    """
    Attributes:
        commands (list[PublicGenerationCommand]):
        game_id (int):
        generation_id (int):
        request_id (UUID):
        schema (str | Unset): A URL to the JSON Schema for this object.
        complete (bool | Unset):
    """

    commands: list[PublicGenerationCommand]
    game_id: int
    generation_id: int
    request_id: UUID
    schema: str | Unset = UNSET
    complete: bool | Unset = UNSET

    def to_dict(self) -> dict[str, Any]:
        commands = []
        for commands_item_data in self.commands:
            commands_item = commands_item_data.to_dict()
            commands.append(commands_item)

        game_id = self.game_id

        generation_id = self.generation_id

        request_id = str(self.request_id)

        schema = self.schema

        complete = self.complete

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "commands": commands,
                "game_id": game_id,
                "generation_id": generation_id,
                "request_id": request_id,
            }
        )
        if schema is not UNSET:
            field_dict["$schema"] = schema
        if complete is not UNSET:
            field_dict["complete"] = complete

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.public_generation_command import PublicGenerationCommand

        d = dict(src_dict)
        commands = []
        _commands = d.pop("commands")
        for commands_item_data in _commands:
            commands_item = PublicGenerationCommand.from_dict(commands_item_data)

            commands.append(commands_item)

        game_id = d.pop("game_id")

        generation_id = d.pop("generation_id")

        request_id = UUID(d.pop("request_id"))

        schema = d.pop("$schema", UNSET)

        complete = d.pop("complete", UNSET)

        register_generation_commands = cls(
            commands=commands,
            game_id=game_id,
            generation_id=generation_id,
            request_id=request_id,
            schema=schema,
            complete=complete,
        )

        return register_generation_commands
