# The league's ledger

One entry per iteration, newest last. An entry says what was measured, what was tried, what was kept and what was learned, so that the next session starts from here and doesn't try a rejected idea again. Ratings have `level3` at 1000.

## Lessons so far

Short, general statements about the game that the games have shown. Add to them, and correct them when a later game says otherwise.

- On `circle` (two castles 4 apart, no neutral castles) a bot that spends its first soldiers on upgrades loses to one that sends everything at once: 32 soldiers arrive on turn 11, before production level 3 has paid for itself.
- Production upgrades pay on most maps with neutral castles: the economist, which does nothing else cleverly, beats default level 3 in 22 of 26 games. `islands` is the exception so far (4 of 12 points there).

## Iteration 0: the founders

**Date:** 2026-11-21. **Maps:** 8 training, 5 holdout. **Matches:** 468.

Four founders with different strategies, written to be simple and to disagree with each other, not to be good:

| Bot | Idea |
|---|---|
| `expander` | Each castle takes the cheapest castle it can afford alone, neutral or enemy; no upgrades. |
| `rusher` | Buy speed, then send every wave at the nearest enemy castle; neutrals only when close and cheap. |
| `economist` | Buy production levels first, expand with the surplus, attack enemies only with a margin. |
| `sentinel` | Burst and reinforce castles that would fall, and strike enemy castles that emptied. |

Training maps:

| Player | Rating | Score | W | D | L |
|---|---|---|---|---|---|
| economist | 1093 | 0.833 | 80 | 0 | 16 |
| level3 | 1000 | 0.656 | 38 | 8 | 18 |
| sentinel | 857 | 0.542 | 46 | 12 | 38 |
| expander | 805 | 0.469 | 41 | 8 | 47 |
| rusher | 767 | 0.417 | 32 | 16 | 48 |
| level2 | 753 | 0.344 | 19 | 6 | 39 |
| level1 | 496 | 0.109 | 0 | 14 | 50 |

Holdout maps:

| Player | Rating | Score | W | D | L |
|---|---|---|---|---|---|
| economist | 1462 | 0.983 | 59 | 0 | 1 |
| level3 | 1000 | 0.588 | 22 | 3 | 15 |
| sentinel | 836 | 0.508 | 27 | 7 | 26 |
| rusher | 802 | 0.467 | 25 | 6 | 29 |
| expander | 775 | 0.433 | 26 | 0 | 34 |
| level2 | 710 | 0.275 | 10 | 2 | 28 |
| level1 | 380 | 0.050 | 0 | 4 | 36 |

**Champion:** `economist`.

**Where it loses (training maps):** `islands` (4 of 12 points) and `strait` (8 of 12), and both `circle` games against `expander`. Nobody has read those games beyond `circle` yet; that is iteration 1's first job.

**No bot failed a turn**, and no bot took more than 20 ms for a turn.
