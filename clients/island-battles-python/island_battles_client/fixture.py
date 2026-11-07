"""A small board for ``island-battles-bot check`` and for trying ``serve`` with curl."""

from __future__ import annotations

from copy import deepcopy
from typing import Any

from ._generated import BotTurn

_UPGRADES = {"speed": 1, "clash_stop_generations": 1}


# What GET /game/upgrades returns; every game has the same table.
_UPGRADE_TABLE = [
    {
        "type": "clash_stop_generations",
        "description": "Generations a troop stays stopped after a clash",
        "max_level": 3,
        "levels": [
            {"level": 1, "value": 3, "soldier_cost": 0},
            {"level": 2, "value": 2, "soldier_cost": 10},
            {"level": 3, "value": 1, "soldier_cost": 20},
        ],
    },
    {
        "type": "speed",
        "description": "World units a troop moves per turn",
        "max_level": 4,
        "levels": [
            {"level": 1, "value": 0.5, "soldier_cost": 0},
            {"level": 2, "value": 1, "soldier_cost": 10},
            {"level": 3, "value": 1.5, "soldier_cost": 20},
            {"level": 4, "value": 2, "soldier_cost": 30},
        ],
    },
]


def _castle(
    castle_id: int, team_color: int, soldiers: int, x: float, y: float
) -> dict[str, Any]:
    return {
        "id": castle_id,
        "team_color": team_color,
        "soldiers": soldiers,
        "x": x,
        "y": y,
        "upgrades": dict(_UPGRADES),
    }


def fixture_turn_data() -> dict[str, Any]:
    """A ``BotTurn`` as JSON: team 1 against team 2, a neutral castle, one group."""
    return {
        "game_id": 1,
        "team_color": 1,
        "upgrades": deepcopy(_UPGRADE_TABLE),
        "memory": None,
        "turn_deadline_at": None,
        "generation": {
            "id": 12,
            "castles": [
                _castle(0, 1, 30, -3.0, 0.0),
                _castle(1, 2, 30, 3.0, 0.0),
                _castle(2, 0, 10, 0.0, 2.0),
                _castle(3, 1, 12, -2.0, -2.0),
            ],
            "soldier_groups": [
                {
                    "id": 0,
                    "team_color": 2,
                    "soldiers": 4,
                    "origin_castle_id": 1,
                    "destination_castle_id": 0,
                    "x": 1.0,
                    "y": 0.0,
                    "clash_radius": 0.5,
                    "upgrades": dict(_UPGRADES),
                }
            ],
            "clashes": [],
        },
    }


def fixture_turn() -> BotTurn:
    """The same turn as a model."""
    return BotTurn.model_validate(fixture_turn_data())
