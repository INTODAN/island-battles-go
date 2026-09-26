from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar

from attrs import define as _attrs_define

T = TypeVar("T", bound="SendSoldierGroupParameters")


@_attrs_define
class SendSoldierGroupParameters:
    """
    Attributes:
        destination_persistent_castle_id (int):
        origin_persistent_castle_id (int):
        soldier_amount (int):
    """

    destination_persistent_castle_id: int
    origin_persistent_castle_id: int
    soldier_amount: int

    def to_dict(self) -> dict[str, Any]:
        destination_persistent_castle_id = self.destination_persistent_castle_id

        origin_persistent_castle_id = self.origin_persistent_castle_id

        soldier_amount = self.soldier_amount

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "destination_persistent_castle_id": destination_persistent_castle_id,
                "origin_persistent_castle_id": origin_persistent_castle_id,
                "soldier_amount": soldier_amount,
            }
        )

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        destination_persistent_castle_id = d.pop("destination_persistent_castle_id")

        origin_persistent_castle_id = d.pop("origin_persistent_castle_id")

        soldier_amount = d.pop("soldier_amount")

        send_soldier_group_parameters = cls(
            destination_persistent_castle_id=destination_persistent_castle_id,
            origin_persistent_castle_id=origin_persistent_castle_id,
            soldier_amount=soldier_amount,
        )

        return send_soldier_group_parameters
