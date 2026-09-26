from __future__ import annotations

from collections.abc import Mapping
from typing import TYPE_CHECKING, Any, TypeVar

from attrs import define as _attrs_define

from ..types import UNSET, Unset

if TYPE_CHECKING:
    from ..models.castle_view import CastleView
    from ..models.clash_event import ClashEvent
    from ..models.soldier_group_view import SoldierGroupView


T = TypeVar("T", bound="GenerationView")


@_attrs_define
class GenerationView:
    """
    Attributes:
        castles (list[CastleView]):
        soldier_groups (list[SoldierGroupView]):
        clashes (list[ClashEvent] | Unset):
    """

    castles: list[CastleView]
    soldier_groups: list[SoldierGroupView]
    clashes: list[ClashEvent] | Unset = UNSET

    def to_dict(self) -> dict[str, Any]:
        castles = []
        for castles_item_data in self.castles:
            castles_item = castles_item_data.to_dict()
            castles.append(castles_item)

        soldier_groups = []
        for soldier_groups_item_data in self.soldier_groups:
            soldier_groups_item = soldier_groups_item_data.to_dict()
            soldier_groups.append(soldier_groups_item)

        clashes: list[dict[str, Any]] | Unset = UNSET
        if not isinstance(self.clashes, Unset):
            clashes = []
            for clashes_item_data in self.clashes:
                clashes_item = clashes_item_data.to_dict()
                clashes.append(clashes_item)

        field_dict: dict[str, Any] = {}

        field_dict.update(
            {
                "castles": castles,
                "soldier_groups": soldier_groups,
            }
        )
        if clashes is not UNSET:
            field_dict["clashes"] = clashes

        return field_dict

    @classmethod
    def from_dict(cls: type[T], src_dict: Mapping[str, Any]) -> T:
        from ..models.castle_view import CastleView
        from ..models.clash_event import ClashEvent
        from ..models.soldier_group_view import SoldierGroupView

        d = dict(src_dict)
        castles = []
        _castles = d.pop("castles")
        for castles_item_data in _castles:
            castles_item = CastleView.from_dict(castles_item_data)

            castles.append(castles_item)

        soldier_groups = []
        _soldier_groups = d.pop("soldier_groups")
        for soldier_groups_item_data in _soldier_groups:
            soldier_groups_item = SoldierGroupView.from_dict(soldier_groups_item_data)

            soldier_groups.append(soldier_groups_item)

        _clashes = d.pop("clashes", UNSET)
        clashes: list[ClashEvent] | Unset = UNSET
        if _clashes is not UNSET:
            clashes = []
            for clashes_item_data in _clashes:
                clashes_item = ClashEvent.from_dict(clashes_item_data)

                clashes.append(clashes_item)

        generation_view = cls(
            castles=castles,
            soldier_groups=soldier_groups,
            clashes=clashes,
        )

        return generation_view
