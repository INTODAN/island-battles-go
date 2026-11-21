"""Make new maps for the league: fair two-team maps from a seed."""

from __future__ import annotations

import random
from typing import Any

NEUTRAL_SOLDIERS = (5, 8, 10, 12, 15, 18, 20, 25, 30)


def generate(name: str, seed: int) -> dict[str, Any]:
    """A map file for two teams, the same from both sides.

    Every castle at (x, y) has its twin at (-x, -y), so neither seat starts
    ahead. The same seed gives the same map.
    """
    rng = random.Random(seed)
    width, height = rng.choice((10, 12, 14, 16)), rng.choice((8, 10))
    half_width, half_height = width / 2, height / 2

    def half_step(low: float, high: float) -> float:
        return rng.randint(round(low * 2), round(high * 2)) / 2

    home = (half_step(half_width - 2, half_width - 0.5), half_step(-half_height / 2, half_height / 2))
    taken = [home, (-home[0], -home[1])]
    castles: list[dict[str, Any]] = [
        {"team_color": 1, "x": -home[0], "y": -home[1], "starting_soldier_amount": 30},
        {"team_color": 2, "x": home[0], "y": home[1], "starting_soldier_amount": 30},
    ]
    if rng.random() < 0.4:
        taken.append((0.0, 0.0))
        castles.append({"team_color": 0, "x": 0, "y": 0, "starting_soldier_amount": rng.randint(20, 35)})
    pairs = rng.randint(2, 5)
    attempts = 0
    while pairs > 0 and attempts < 1000:
        attempts += 1
        spot = (half_step(0, half_width - 0.5), half_step(-half_height + 0.5, half_height - 0.5))
        twin = (-spot[0], -spot[1])
        if any((spot[0] - x) ** 2 + (spot[1] - y) ** 2 < 1.5**2 for x, y in [*taken, twin]):
            continue
        taken += [spot, twin]
        soldiers = rng.choice(NEUTRAL_SOLDIERS)
        castles.append({"team_color": 0, "x": twin[0], "y": twin[1], "starting_soldier_amount": soldiers})
        castles.append({"team_color": 0, "x": spot[0], "y": spot[1], "starting_soldier_amount": soldiers})
        pairs -= 1
    return {
        "map_name": name,
        "player_amount": 2,
        "plane_size_x": width,
        "plane_size_y": height,
        "neutral_starting_soldier_amount": 15,
        "castles": castles,
    }
