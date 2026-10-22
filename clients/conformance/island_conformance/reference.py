"""Executable definition of the model utilities every client must match.

Clients implement these as methods on their generated models. The fixtures and
the randomized boards take their expected values from this module, so a
change to a rule is made here once and every client is checked against it.
"""

import math

NEUTRAL_TEAM_COLOR = 0


def distance(entity: dict, other: dict) -> float:
    """Straight-line distance in map units between two castles or groups."""
    return math.hypot(other["x"] - entity["x"], other["y"] - entity["y"])


def compare_team(entity: dict, other: dict) -> str:
    """Other's allegiance relative to entity; a neutral target is always neutral."""
    if other["team_color"] == NEUTRAL_TEAM_COLOR:
        return "neutral"
    return "friendly" if entity["team_color"] == other["team_color"] else "enemy"


def evaluate(generation: dict) -> dict | None:
    """Expected utilities for a valid wire generation, or None when a group's
    destination castle is not part of it. Entities are castles, then groups."""
    castles = {castle["id"]: castle for castle in generation["castles"]}
    destinations = [
        castles.get(group["destination_castle_id"])
        for group in generation["soldier_groups"]
    ]
    if None in destinations:
        return None
    entities = [*generation["castles"], *generation["soldier_groups"]]
    return {
        "distances": [
            [distance(entity, other) for other in entities] for entity in entities
        ],
        "relations": [
            [compare_team(entity, other) for other in entities] for entity in entities
        ],
        "distance_left": [
            distance(group, destination)
            for group, destination in zip(generation["soldier_groups"], destinations)
        ],
    }
