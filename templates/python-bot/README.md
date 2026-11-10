# My Island Battles bot

A starter project for a bot in the strategy game Island Battles: a working bot, tests, `uv`, `ruff` and `ty` set up, short scripts, and skills for coding agents (Claude Code and others) that explain the game.

## Start

1. Copy this directory (it needs nothing outside itself) and rename the project in `pyproject.toml`.
2. Install [uv](https://docs.astral.sh/uv/), then `uv sync` (it installs Python 3.14 if needed).
3. `scripts/check.sh` formats, lints, type-checks, tests and runs the bot once on a sample turn.
4. `scripts/serve.sh` serves the bot on `http://127.0.0.1:41414`. Open the game at `https://island-battles.elidan.org` (or a local lobby page), create a lobby, add a bot seat for the other team, pick **localhost bot** as your bot, get ready and start. The page relays each turn to your bot. Saving `bot.py` reloads it.

The bot is in `bot.py`. It buys speed upgrades, attacks the nearest castle that is not its own and keeps a counter in its memory. Replace the strategy; keep the shape.

## Scripts

All run from any directory.

| Script | What it does |
|---|---|
| `scripts/check.sh` | `ruff format --check`, `ruff check`, `ty check`, `pytest`, then `island-battles-bot check bot:play`. Run it before playing. |
| `scripts/format.sh` | `ruff format` and `ruff check --fix`. |
| `scripts/serve.sh` | The localhost bot a browser tab plays (`island-battles-bot serve bot:play`). |
| `scripts/run.sh` | Plays a seat of a started lobby without a browser: `scripts/run.sh BASE_URL TOKEN LOBBY_ID`, or set `ISLAND_BATTLES_BASE_URL`, `ISLAND_BATTLES_TOKEN`, `ISLAND_BATTLES_LOBBY_ID`. Logs to `bot.log`. |
| `scripts/fixture.sh [file]` | Writes the sample turn to `turn.json` for experiments. |
| `scripts/build.sh` | `uv build`: a wheel and a source archive in `dist/`, and prints their paths. |

`scripts/build.sh` makes the standard Python package of the bot. Uploading a bot to the game server does not exist yet; bots run on your machine.

## Coding agents

`AGENTS.md` (and `CLAUDE.md`, which imports it) lists the project's rules and four skills in `.agents/skills/`: the game rules, how to write a bot, logging, and the workflow. Point your agent at them, or ask it to read `AGENTS.md` first.

## Where next

- The SDK, its helpers and the `island-battles-bot` command: [island-battles-python on PyPI](https://pypi.org/project/island-battles-python/).
- Ideas: attack weak neutral castles first, defend a castle that a group is walking to, store a plan in the memory, buy `clash_stop_generations`.
