---
name: bot-logging
description: "How an Island Battles bot logs: the island_battles.player logger, log_line and the logs of a result, the limits (100 lines per turn, 500 characters per line) and where the lines show up. Load it when adding logging to bot.py or when log lines are missing, cut or counted."
---

# Bot logging

A bot's logs are how a player sees what it thought, in the game's log panel, after the game or while it runs. There are two ways to log, and they end up in the same place.

## 1. The logger

```python
from island_battles_client import logger  # a logging.Logger named "island_battles.player"

def play(turn: BotTurn) -> BotTurnResult:
    logger.info("generation %d: %d own castles", turn.generation.id, len(own))
    logger.warning("castle %d is under attack", castle.id)
    logger.exception("planner failed")  # inside `except`: keeps the traceback
```

Use it like any standard logger (`debug`, `info`, `warning`, `error`). Helper modules can use a child, `logging.getLogger("island_battles.player.planner")`. Use `%` arguments instead of f-strings for values that are costly to format. Records logged while `play` runs are filed under that turn; this works in sync and `async` bots.

## 2. Lines in the result

```python
from island_battles_client import log_line, turn_result

return turn_result(commands, logs=[log_line("attacking castle 3", level="info")], memory=memory)
```

`log_line(message, level="info")` takes a level of `debug`, `info`, `warning` or `error`. The lines logged with `logger` come first, then those in `logs`. `log_line` cuts a message longer than 500 characters itself.

## Limits

The server stores at most **100 lines per player per generation**, each of at most **500 characters**.

- A line over 500 characters is cut to exactly 500, ending in `… (cut at 500 characters)`.
- Beyond 100 lines the rest is dropped. The 100th line becomes an `error` line saying `game log limit reached for generation N: 100 lines per generation; K lines were not stored`, so the reader knows why the lines stop. A logging call never raises.
- Empty lines are not stored.

So log a summary, not one line per castle: with 40 castles, one line per castle runs out of room in a turn. Log the decision (the target and why), the numbers that drove it, and surprises.

## Where lines show up

- **The game's log panel** in the lobby page's replay: your team's lines while the game runs, every team's after it ends. Always on.
- **stdout** of the process that plays (`scripts/serve.sh`, `scripts/run.sh`), at `info` and above.
- **A file**: `scripts/run.sh` appends to `bot.log`. For `serve`, redirect: `scripts/serve.sh 2>&1 | tee bot.log`.

An exception in `play` is answered as an error with its traceback, and shown in the game log with the exception first.

## Check your logging

`scripts/check.sh` ends with `ok: N commands, M log lines, ...`, where M counts the lines the game log would store for the turn: the lines written with `logger` while `play` ran, plus the lines in the result's `logs`. In a test, run `play(fixture_turn())` and look at `result.logs`; that holds only the lines returned in `logs`, not the ones written with `logger`, so use `caplog` (the `island_battles.player` logger) to assert on those.
