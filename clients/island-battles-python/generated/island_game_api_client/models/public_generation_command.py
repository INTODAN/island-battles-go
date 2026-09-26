from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar

from attrs import define as _attrs_define

from ..models.public_generation_command_command_type import PublicGenerationCommandCommandType

if TYPE_CHECKING:
    from ..models.send_soldier_group_parameters import SendSoldierGroupParameters


T = TypeVar("T", bound="PublicGenerationCommand")


@_attrs_define
class PublicGenerationCommand:
    """
    Attributes:
        command_type (PublicGenerationCommandCommandType):
        parameters (SendSoldierGroupParameters):
    """

    command_type: PublicGenerationCommandCommandType
    parameters: SendSoldierGroupParameters

    def to_dict(self) -> dict[str, Any]:
        command_type = self.command_type.value

        parameters = self.parameters.to_dict()

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "command_type": command_type,
                "parameters": parameters,
            }
        )

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.send_soldier_group_parameters import SendSoldierGroupParameters

        d = dict(src_dict)
        command_type = PublicGenerationCommandCommandType(d.pop("command_type"))

        parameters = SendSoldierGroupParameters.from_dict(d.pop("parameters"))

        public_generation_command = cls(
            command_type=command_type,
            parameters=parameters,
        )

        return public_generation_command
