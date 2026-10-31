"""Seeded random wire generations for comparing clients with the reference.

Boards favor the cases hand-picked fixtures miss: shared positions, groups at
their destination, neutral and repeated team colors, whole-number floats, and
fields a newer server might add.
"""

import copy
import random

UNKNOWN_FIELD = "added_by_newer_server"


def _coordinate(rng: random.Random) -> float | int:
    kind = rng.random()
    if kind < 0.25:
        return rng.randint(-50, 50)  # Go encodes whole floats as integers.
    if kind < 0.5:
        return rng.randint(-400, 400) / 8
    return rng.uniform(-1000, 1000)


def _position(rng: random.Random, taken: list[tuple]) -> tuple:
    if taken and rng.random() < 0.15:
        return rng.choice(taken)
    position = (_coordinate(rng), _coordinate(rng))
    taken.append(position)
    return position


def _identifiers(rng: random.Random, count: int) -> list[int]:
    return rng.sample(range(10_000), count)


def _upgrades(rng: random.Random) -> dict:
    return {"clash_stop_generations": rng.randint(1, 3), "speed": rng.randint(1, 4)}


def random_board(rng: random.Random) -> tuple[dict, dict]:
    """Return a valid wire generation and its public form without unknown fields."""
    taken: list[tuple] = []
    castles = []
    for identifier in _identifiers(rng, rng.randint(1, 8)):
        x, y = _position(rng, taken)
        castles.append(
            {
                "id": identifier,
                "team_color": rng.randint(0, 4),
                "soldiers": rng.randint(0, 500),
                "x": x,
                "y": y,
                "upgrades": _upgrades(rng),
            }
        )
    groups = []
    for identifier in _identifiers(rng, rng.randint(0, 12)):
        origin, destination = rng.choice(castles), rng.choice(castles)
        if rng.random() < 0.2:
            x, y = destination["x"], destination["y"]
        else:
            x, y = _position(rng, taken)
        groups.append(
            {
                "id": identifier,
                "team_color": rng.randint(0, 4),
                "soldiers": rng.randint(0, 200),
                "x": x,
                "y": y,
                "origin_castle_id": origin["id"],
                "destination_castle_id": destination["id"],
                "clash_radius": rng.choice([0.25, 0.5, rng.uniform(0.01, 3)]),
                "upgrades": _upgrades(rng),
            }
        )
    clashes = []
    for index in range(rng.randint(0, 3)):
        participants = []
        for _ in range(rng.randint(0, 4)):
            before = rng.randint(0, 100)
            participants.append(
                {
                    "group_id": rng.randint(0, 10_000),
                    "team_color": rng.randint(0, 4),
                    "x": _coordinate(rng),
                    "y": _coordinate(rng),
                    "clash_radius": rng.uniform(0.01, 3),
                    "soldiers_before": before,
                    "soldiers_after": rng.randint(0, before),
                }
            )
        clashes.append(
            {
                "id": index + 1,
                "time": rng.choice([0, 1, rng.random()]),
                "participants": participants,
            }
        )
    public = {
        "id": rng.randint(1, 1_000_000),
        "castles": castles,
        "soldier_groups": groups,
        "clashes": clashes,
    }
    wire = copy.deepcopy(public)
    if rng.random() < 0.3:
        targets = [wire, *wire["castles"], *wire["soldier_groups"], *wire["clashes"]]
        rng.choice(targets)[UNKNOWN_FIELD] = rng.choice(
            [1, "text", None, {"nested": [True]}]
        )
    return wire, public


def without_destination(rng: random.Random, wire: dict) -> dict | None:
    """Point one group at a castle outside the board, which clients must reject."""
    if not wire["soldier_groups"]:
        return None
    broken = copy.deepcopy(wire)
    missing = max(castle["id"] for castle in broken["castles"]) + 1
    rng.choice(broken["soldier_groups"])["destination_castle_id"] = missing
    return broken
