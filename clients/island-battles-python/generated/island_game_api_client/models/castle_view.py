from __future__ import annotations

from collections.abc import Mapping
from typing import Any, TypeVar

from attrs import define as _attrs_define

T = TypeVar("T", bound="CastleView")


@_attrs_define
class CastleView:
    """
    Attributes:
        id (int):
        soldiers (int):
        team_color (int):
        x (float):
        y (float):
    """

    id: int
    soldiers: int
    team_color: int
    x: float
    y: float

    def to_dict(self) -> dict[str, Any]:
        id = self.id

        soldiers = self.soldiers

        team_color = self.team_color

        x = self.x

        y = self.y

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "id": id,
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
        id = d.pop("id")

        soldiers = d.pop("soldiers")

        team_color = d.pop("team_color")

        x = d.pop("x")

        y = d.pop("y")

        castle_view = cls(
            id=id,
            soldiers=soldiers,
            team_color=team_color,
            x=x,
            y=y,
        )

        return castle_view
