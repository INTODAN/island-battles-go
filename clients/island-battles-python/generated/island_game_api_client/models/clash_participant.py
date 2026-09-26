from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar

from attrs import define as _attrs_define

T = TypeVar("T", bound="ClashParticipant")


@_attrs_define
class ClashParticipant:
    """
    Attributes:
        clash_radius (float):
        group_id (int):
        soldiers_after (int):
        soldiers_before (int):
        team_color (int):
        x (float):
        y (float):
    """

    clash_radius: float
    group_id: int
    soldiers_after: int
    soldiers_before: int
    team_color: int
    x: float
    y: float

    def to_dict(self) -> dict[str, Any]:
        clash_radius = self.clash_radius

        group_id = self.group_id

        soldiers_after = self.soldiers_after

        soldiers_before = self.soldiers_before

        team_color = self.team_color

        x = self.x

        y = self.y

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "clash_radius": clash_radius,
                "group_id": group_id,
                "soldiers_after": soldiers_after,
                "soldiers_before": soldiers_before,
                "team_color": team_color,
                "x": x,
                "y": y,
            }
        )

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        d = dict(src_dict)
        clash_radius = d.pop("clash_radius")

        group_id = d.pop("group_id")

        soldiers_after = d.pop("soldiers_after")

        soldiers_before = d.pop("soldiers_before")

        team_color = d.pop("team_color")

        x = d.pop("x")

        y = d.pop("y")

        clash_participant = cls(
            clash_radius=clash_radius,
            group_id=group_id,
            soldiers_after=soldiers_after,
            soldiers_before=soldiers_before,
            team_color=team_color,
            x=x,
            y=y,
        )

        return clash_participant
