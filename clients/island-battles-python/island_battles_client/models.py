"""Typed views of the public generation wire format."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping

from .errors import GameProtocolError


def _integer(value: Any, name: str) -> int:
    if type(value) is not int:
        raise GameProtocolError(f"invalid {name} in generation state")
    return value


def _string(value: Any, name: str) -> str:
    if not isinstance(value, str) or not value:
        raise GameProtocolError(f"invalid {name} in generation state")
    return value


@dataclass(frozen=True)
class Castle:
    id: int
    team_color: int
    soldiers: int
    x: float
    y: float

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> Castle:
        try:
            return cls(_integer(data["id"], "castle.id"),
                       _integer(data["team_color"], "castle.team_color"),
                       _integer(data["soldiers"], "castle.soldiers"),
                       float(data["x"]), float(data["y"]))
        except (KeyError, TypeError, ValueError, OverflowError) as exc:
            raise GameProtocolError("invalid castle in generation state") from exc


@dataclass(frozen=True)
class SoldierGroup:
    id: int
    team_color: int
    soldiers: int
    origin_logical_castle_id: int | None
    destination_logical_castle_id: int | None
    clash_radius: float | None
    x: float | None
    y: float | None

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> SoldierGroup:
        try:
            def optional_int(key: str) -> int | None:
                value = data.get(key)
                return None if value is None else _integer(value, f"soldier_group.{key}")

            def optional_float(key: str) -> float | None:
                value = data.get(key)
                return None if value is None else float(value)

            return cls(_integer(data["id"], "soldier_group.id"),
                       _integer(data["team_color"], "soldier_group.team_color"),
                       _integer(data["soldiers"], "soldier_group.soldiers"),
                       optional_int("origin_logical_castle_id"),
                       optional_int("destination_logical_castle_id"),
                       optional_float("clash_radius"), optional_float("x"), optional_float("y"))
        except (KeyError, TypeError, ValueError, OverflowError) as exc:
            raise GameProtocolError("invalid soldier group in generation state") from exc


@dataclass(frozen=True)
class Generation:
    castles: tuple[Castle, ...]
    soldier_groups: tuple[SoldierGroup, ...]

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> Generation:
        try:
            castles = data["castles"]
            groups = data["soldier_groups"]
            if not isinstance(castles, list) or not isinstance(groups, list):
                raise TypeError("generation lists required")
            return cls(tuple(Castle.from_dict(item) for item in castles),
                       tuple(SoldierGroup.from_dict(item) for item in groups))
        except (KeyError, TypeError, ValueError) as exc:
            raise GameProtocolError("invalid generation in generation state") from exc


@dataclass(frozen=True)
class GenerationState:
    game_id: int
    lobby_id: str
    generation_id: int
    player_id: str
    team_color: int
    channel: str
    submitted: bool
    generation: Generation

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> GenerationState:
        try:
            submitted = data["submitted"]
            if type(submitted) is not bool:
                raise TypeError("submitted must be a boolean")
            return cls(_integer(data["game_id"], "game_id"),
                       _string(data["lobby_id"], "lobby_id"),
                       _integer(data["generation_id"], "generation_id"),
                       _string(data["player_id"], "player_id"),
                       _integer(data["team_color"], "team_color"),
                       _string(data["channel"], "channel"), submitted,
                       Generation.from_dict(data["generation"]))
        except (KeyError, TypeError, ValueError) as exc:
            raise GameProtocolError("invalid generation state") from exc


@dataclass(frozen=True)
class SoldierGroupOrder:
    origin_logical_castle_id: int
    destination_logical_castle_id: int
    soldier_amount: int

    def to_command(self) -> dict[str, Any]:
        return {"command_type": "send_soldier_group", "parameters": {
            "origin_logical_castle_id": self.origin_logical_castle_id,
            "destination_logical_castle_id": self.destination_logical_castle_id,
            "soldier_amount": self.soldier_amount,
        }}
