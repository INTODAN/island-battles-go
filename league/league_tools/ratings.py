"""Ratings from a set of games: a Bradley-Terry fit, shown on the Elo scale."""

from __future__ import annotations

import math
from collections import defaultdict
from collections.abc import Iterable

# The default bot that every rating is measured against: it is always 1000,
# so ratings from different tournaments compare.
RATING_ANCHOR = "level3"


def fit(games: Iterable[tuple[str, str, float]]) -> dict[str, float]:
    """Rate the players of ``games``.

    Each game is ``(first, second, score)``, where score is the first
    player's: 1 for a win, 0.5 for a draw, 0 for a loss. Unlike running Elo
    updates, the fit doesn't depend on the order of the games. Every player
    also gets one drawn game against an average player, which keeps the
    rating of a player who won or lost everything finite.

    Returns:
        Each player's rating. ``level3`` is 1000 when it played; otherwise the
        mean is 1000.
    """
    score: dict[str, float] = defaultdict(float)
    played: dict[str, dict[str, int]] = defaultdict(lambda: defaultdict(int))
    for first, second, first_score in games:
        score[first] += first_score
        score[second] += 1 - first_score
        played[first][second] += 1
        played[second][first] += 1
    players = sorted(played)
    if not players:
        return {}
    strength = dict.fromkeys(players, 1.0)
    for _ in range(1000):
        updated = {}
        for player in players:
            expected = 1 / (strength[player] + 1)
            for opponent, count in played[player].items():
                expected += count / (strength[player] + strength[opponent])
            updated[player] = (score[player] + 0.5) / expected
        change = max(abs(math.log(updated[player] / strength[player])) for player in players)
        strength = updated
        if change < 1e-9:
            break
    ratings = {player: 400 * math.log10(strength[player]) for player in players}
    reference = ratings.get(RATING_ANCHOR, sum(ratings.values()) / len(ratings))
    return {player: 1000 + rating - reference for player, rating in ratings.items()}
