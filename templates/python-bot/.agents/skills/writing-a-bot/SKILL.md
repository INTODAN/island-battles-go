---
name: writing-a-bot
description: "How to write or change the Island Battles bot in bot.py: the play function, what it receives and returns, every SDK helper, why the server rejects a result, and the rules for memory (statelessness, 64 KiB, determinism, speed). Load it before editing bot.py or its tests."
---

# Writing a bot

A bot is one function in `bot.py`, `play(turn: BotTurn) -> BotTurnResult`, sync or `async`. The game rules are in `island-battles-rules`. Everything below is importable from `island_battles_client`.

## What `play` receives

- `turn.generation.castles` and `.soldier_groups`: the board (`.id` is the generation number; `.clashes` lists last turn's fights).
- `turn.team_color`: your team. Castle: `id`, `team_color`, `soldiers`, `x`, `y`, `upgrades` (`castle.upgrades["speed"]`). Group: also `origin_castle_id`, `destination_castle_id`, `clash_radius`.
- `turn.upgrades`: the upgrade cost table. `turn.memory`: what you returned last turn, or `None`. `turn.turn_deadline_at`: when the turn times out, or `None`.

Models are frozen: build new values instead of changing them.

## Helpers

```python
from island_battles_client import (NEUTRAL_TEAM_COLOR, TeamType, UpgradeType, log_line,
                                   next_upgrade_cost, send_soldier_group, turn_result, upgrade_castle)

own = [c for c in turn.generation.castles if c.team_color == turn.team_color]
send_soldier_group(own[0].id, 1, 5)            # 5 soldiers from castle origin to castle 1
upgrade_castle(own[0].id, UpgradeType.speed)   # buy the castle's next speed level
next_upgrade_cost(turn, own[0], UpgradeType.speed)  # that level's cost, None at the maximum
own[0].distance(turn.generation.castles[1])    # straight-line map units (castles and groups)
own[0].compare_team(other)                     # TeamType.friendly / .neutral / .enemy
group.destination_castle, group.distance_left()  # where a SoldierGroup is going, and how far
log_line("low on soldiers", level="warning")   # a game log line; see bot-logging
turn_result(commands, logs=[...], memory={})   # the result; every part is optional
```

`NEUTRAL_TEAM_COLOR` is `0`. The helpers validate their arguments and raise `pydantic.ValidationError` for bad ones (zero soldiers, unknown upgrade type) before anything is sent.

## Why the server rejects a result

The server checks the commands in order against the board and rejects the **whole result** (the turn stays open and `scripts/run.sh` stops; through `serve` the lobby page shows the error) if any one is invalid:

- `InsufficientSoldiersError`: a castle sends or pays more than it has. Commands add up: sending 10 and upgrading for 10 needs 20. Track what each castle spends in a `spent` dict, as `bot.py` does. `next_upgrade_cost` does not count upgrades bought earlier in the same result.
- `CastleNotOwnedError` (castle is neutral or an enemy's, or you sent from it), `CastleNotFoundError`, `UpgradeMaxLevelError`.
- Zero soldiers, or the same castle as origin and destination.

## Stateless, with memory

**The bot keeps nothing in variables between turns.** No globals, class attributes, caches, files or counters: the process may restart, reload on every saved file, or run on another machine. Everything it must remember goes in `memory`, a JSON object (numbers, strings, lists, objects) returned with the result, and comes back as `turn.memory` next turn.

```python
seen = (turn.memory or {}).get("turns_seen", 0) + 1
return turn_result(commands, memory={"turns_seen": seen})
```

- At most **64 KiB** as compact JSON (`MEMORY_LIMIT_BYTES` is 65,536 bytes), else `BotMemoryTooLargeError` and the result is not sent. Keep it far smaller: store plans and counters, not the board.
- Memory is saved only when a turn completes. A rejected or crashed turn leaves the old memory.
- `turn.memory` is `None` on the first turn and after a turn that returned none.

## Deterministic and fast

The same `BotTurn` must give the same result: no `random` without a seed stored in memory, no clocks, no network or file access. Sort before choosing (`sorted(castles, key=lambda c: c.id)`) so ties don't depend on order. That is what makes a bot testable with `fixture_turn()` (see `tests/test_bot.py`).

A slow bot loses the game: the turn timeout is 5 to 600 seconds (30 by default), and `serve` answers 504 after 8 seconds. Aim for well under one second. No loops that grow with the number of turns.
