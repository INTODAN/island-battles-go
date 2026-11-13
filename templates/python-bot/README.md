# My Island Battles bot

A starter project for a bot in the strategy game Island Battles: a working bot, tests, `uv`, `ruff` and `ty` set up, short scripts, and skills for coding agents (Claude Code and others) that explain the game.

## Start

1. Copy this directory (it needs nothing outside itself) and rename the project in `pyproject.toml`.
2. Install [uv](https://docs.astral.sh/uv/), then `uv sync` (it installs Python 3.14 if needed).
3. `scripts/check.sh` formats, lints, type-checks, tests and runs the bot once on a sample turn.
4. `scripts/serve.sh` serves the bot on `http://127.0.0.1:41414`. Open the game at `https://island-battles.elidan.org` (or a local lobby page), create a lobby, add a bot seat for the other team, pick **localhost bot** as your bot, get ready and start. The page delivers each turn to your bot, which submits its answer to the game itself ([how](#how-your-bot-gets-its-turns)). Saving `bot.py` reloads it.

The bot is in `bot.py`. It buys speed upgrades, attacks the nearest castle that is not its own and keeps a counter in its memory. Replace the strategy; keep the shape.

## Scripts

All run from any directory.

| Script | What it does |
|---|---|
| `scripts/check.sh` | `ruff format --check`, `ruff check`, `ty check`, `pytest`, then `island-battles-bot check bot:play`. Run it before playing. |
| `scripts/format.sh` | `ruff format` and `ruff check --fix`. |
| `scripts/serve.sh` | The localhost bot a browser tab plays (`island-battles-bot serve bot:play`). |
| `scripts/run.sh` | Plays a seat of a started lobby without a browser: `scripts/run.sh TOKEN LOBBY_ID`, or set `ISLAND_BATTLES_TOKEN` and `ISLAND_BATTLES_LOBBY_ID`. The lobby and the game are separate sites: set `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL` (they need `island-battles-python` with #228), or `ISLAND_BATTLES_BASE_URL` for one server that serves both. Logs to `bot.log`. |
| `scripts/fixture.sh [file]` | Writes the sample turn to `turn.json` for experiments. |
| `scripts/build.sh` | `uv build`: a wheel and a source archive in `dist/`, and prints their paths. |
| `scripts/package.sh` | `island-battles-bot package`: `dist/island-battles-bot.zip`, the bot and its dependencies for the server's runners (Linux arm64). Needs a network. |

`scripts/build.sh` makes the standard Python package of the bot. `scripts/package.sh` makes the zip the game server's runners will run: your bot and its locked dependencies for Linux arm64 (`dist/island-battles-bot.zip`; see "Package your bot" in the SDK's README). Packaging needs an `island-battles-python` release that has the worker mode, so raise the minimum in `pyproject.toml` and run `uv lock --upgrade-package island-battles-python` when it is out.

To upload the zip to your lobby, open the bot picker in the lobby page or the 3D client: **Upload bot (.zip)** takes the file, or **Upload from the command line** shows a command to copy, with the game's address and a token that is valid for 15 minutes:

```sh
uv run island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip
```

The game checks the package and lists it for everyone in the lobby; a new upload replaces your previous one. `upload` needs an `island-battles-python` release that has it, like the worker mode. Running uploaded packages comes with #112; until then bots run on your machine.

## How your bot gets its turns

`scripts/serve.sh` runs `island-battles-bot serve`, the same protocol a bot hosted at a URL speaks:

1. Each turn, your game tab reads a **turn delivery** from the game: the turn (`BotTurn`: the board, your team, the upgrade table, your memory, the deadline) and a **submit token** that can submit exactly this turn of yours and nothing else.
2. The tab POSTs the delivery to `http://127.0.0.1:41414/turn`. The bot server answers `202` at once, so nobody waits on your bot.
3. The bot server calls `play(turn)`, for at most `--turn-timeout` seconds (8 by default) and never past the turn's deadline, and submits your commands, log lines and memory to the game with the token.
4. If `play` raises or runs out of time, the bot **passes the turn**: no commands, your old memory, and one `error` line from `island_battles.bots` that says what happened, which you see in the game's **Bot logs**. The terminal shows the traceback. The game rejecting your commands passes the turn the same way, with its reason.

The bot server submits only to the official game, `https://island-battles.elidan.org`, and to `http://localhost` or `http://127.0.0.1` for a local backend; `scripts/serve.sh --allow-submit-host game.example.org` adds an `https` host, such as your own deployment of the game. To host the same server publicly, keep it on `127.0.0.1` behind a reverse proxy that terminates TLS and forwards `POST /turn` and `GET /health` with `Host: 127.0.0.1:41414`; the [SDK's README](https://pypi.org/project/island-battles-python/) has the details.

## Coding agents

`AGENTS.md` (and `CLAUDE.md`, which imports it) lists the project's rules and four skills in `.agents/skills/`: the game rules, how to write a bot, logging, and the workflow. Point your agent at them, or ask it to read `AGENTS.md` first.

## Where next

- The SDK, its helpers and the `island-battles-bot` command: [island-battles-python on PyPI](https://pypi.org/project/island-battles-python/).
- Ideas: attack weak neutral castles first, defend a castle that a group is walking to, store a plan in the memory, buy `clash_stop_generations`.
