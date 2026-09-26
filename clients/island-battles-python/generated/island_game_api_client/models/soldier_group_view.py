from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar

from attrs import define as _attrs_define

T = TypeVar("T", bound="SoldierGroupView")


@_attrs_define
class SoldierGroupView:
    """
    Attributes:
        clash_radius (float):
        destination_persistent_castle_id (int):
        id (int):
        origin_persistent_castle_id (int):
        soldiers (int):
        team_color (int):
        x (float):
        y (float):
    """

    clash_radius: float
    destination_persistent_castle_id: int
    id: int
    origin_persistent_castle_id: int
    soldiers: int
    team_color: int
    x: float
    y: float

    def to_dict(self) -> dict[str, Any]:
        clash_radius = self.clash_radius

        destination_persistent_castle_id = self.destination_persistent_castle_id

        id = self.id

        origin_persistent_castle_id = self.origin_persistent_castle_id

        soldiers = self.soldiers

        team_color = self.team_color

        x = self.x

        y = self.y

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "clash_radius": clash_radius,
                "destination_persistent_castle_id": destination_persistent_castle_id,
                "id": id,
                "origin_persistent_castle_id": origin_persistent_castle_id,
                "soldiers": soldiers,
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

        destination_persistent_castle_id = d.pop("destination_persistent_castle_id")

        id = d.pop("id")

        origin_persistent_castle_id = d.pop("origin_persistent_castle_id")

        soldiers = d.pop("soldiers")

        team_color = d.pop("team_color")

        x = d.pop("x")

        y = d.pop("y")

        soldier_group_view = cls(
            clash_radius=clash_radius,
            destination_persistent_castle_id=destination_persistent_castle_id,
            id=id,
            origin_persistent_castle_id=origin_persistent_castle_id,
            soldiers=soldiers,
            team_color=team_color,
            x=x,
            y=y,
        )

        return soldier_group_view
