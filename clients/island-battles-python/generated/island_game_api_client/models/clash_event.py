from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar

from attrs import define as _attrs_define

if TYPE_CHECKING:
    from ..models.clash_participant import ClashParticipant


T = TypeVar("T", bound="ClashEvent")


@_attrs_define
class ClashEvent:
    """
    Attributes:
        id (int):
        participants (list[ClashParticipant]):
        time (float):
    """

    id: int
    participants: list[ClashParticipant]
    time: float

    def to_dict(self) -> dict[str, Any]:
        id = self.id

        participants = []
        for participants_item_data in self.participants:
            participants_item = participants_item_data.to_dict()
            participants.append(participants_item)

        time = self.time

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "id": id,
                "participants": participants,
                "time": time,
            }
        )

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.clash_participant import ClashParticipant

        d = dict(src_dict)
        id = d.pop("id")

        participants = []
        _participants = d.pop("participants")
        for participants_item_data in _participants:
            participants_item = ClashParticipant.from_dict(participants_item_data)

            participants.append(participants_item)

        time = d.pop("time")

        clash_event = cls(
            id=id,
            participants=participants,
            time=time,
        )

        return clash_event
