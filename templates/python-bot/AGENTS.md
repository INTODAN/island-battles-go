# Agent guide

This is a bot for the game Island Battles, written in Python with the `island-battles-python` SDK. The bot is the function `play` in `bot.py`; the tests are in `tests/`. Everything runs through `uv`.

## Skills

Read the one that applies before changing the bot. They are in `.agents/skills/`.

- `island-battles-rules`: how a game works: turns, growth, movement, clashes, capturing, upgrades and costs, winning and losing.
- `writing-a-bot`: the `play` function, the SDK helpers, why the server rejects a result, memory and statelessness.
- `bot-logging`: logging from a bot, and the limits of the game log.
- `bot-workflow`: the scripts, playing against the default bots, reading a failed check, debugging.

## Rules for this project

- Run `scripts/check.sh` before you finish. It must pass.
- **Never keep state outside the memory.** The bot keeps nothing in globals, attributes, files or caches between turns. What it must remember goes in the `memory` JSON of the result (under 64 KiB).
- The same `BotTurn` must always give the same result: no unseeded randomness, clocks, network or file access.
- Keep the bot typed and documented. `ruff` and `ty` run in `check.sh`; fix their findings instead of ignoring them.
- Change behavior together with a test in `tests/test_bot.py`.
- Keep a turn well under a second: a late answer loses the game.
