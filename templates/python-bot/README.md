# My Island Battles bot

A starter project for a bot in the strategy game Island Battles: a working bot, tests, `uv`, `ruff` and `ty` set up, short scripts, and skills for coding agents (Claude Code and others) that explain the game.

## Start

1. Copy this directory (it needs nothing outside itself) and rename the project in `pyproject.toml`.
2. Install [uv](https://docs.astral.sh/uv/), then `uv sync` (it installs Python 3.14 if needed). The template has no `uv.lock`, so this takes the newest SDK and writes your own lock; commit it with your bot. The game changes its turns with its releases and only the newest SDK reads them: when the game says your bot did not understand the turn, run `uv sync --upgrade-package island-battles-python` and restart the bot.
3. `scripts/check.sh` formats, lints, type-checks, tests and runs the bot once on a sample turn.
4. `scripts/serve.sh` serves the bot on `http://127.0.0.1:41414`. Open the lobby page at `https://lobby.elidan.org/lobby/ui/` (or the 3D client at `https://island-battles.elidan.org/play/`, or a local lobby page at `http://localhost:8890/lobby/ui/`), create a lobby, add a default bot seat for the other team, pick **localhost bot** as your bot, get ready and start. The page delivers each turn to your bot, which submits its answer to the game itself ([how](#how-your-bot-gets-its-turns)). Saving `bot.py` reloads it.

The bot is in `bot.py`. It buys speed upgrades, attacks the nearest castle that is not its own and keeps a counter in its memory. Replace the strategy; keep the shape.

## Scripts

All run from any directory.

| Script | What it does |
|---|---|
| `scripts/check.sh` | `ruff format --check`, `ruff check`, `ty check`, `pytest`, then `island-battles-bot check bot:play`. Run it before playing. |
| `scripts/format.sh` | `ruff format` and `ruff check --fix`. |
| `scripts/serve.sh` | The localhost bot a browser tab plays (`island-battles-bot serve bot:play`), with the lobby page's origin, `https://lobby.elidan.org`, allowed. `scripts/serve.sh --port 41415` serves it on another port. |
| `scripts/run.sh` | Plays a seat of a started lobby without a browser: `scripts/run.sh TOKEN LOBBY_ID`, or set `ISLAND_BATTLES_TOKEN` and `ISLAND_BATTLES_LOBBY_ID`. The lobby and the game are separate sites: set `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL` (they need `island-battles-python` with #228), `https://island-battles.elidan.org` and `https://lobby.elidan.org` for the official game, or `http://localhost:8888` and `http://localhost:8890` locally. `ISLAND_BATTLES_BASE_URL` sets both, for a server that serves the game and the lobby together. Logs to `bot.log`. |
| `scripts/fixture.sh [file]` | Writes the sample turn to `turn.json` for experiments. |
| `scripts/build.sh` | `uv build`: a wheel and a source archive in `dist/`, and prints their paths. |
| `scripts/package.sh` | `island-battles-bot package`: `dist/island-battles-bot.zip`, the bot and its dependencies for the server's runners (Linux arm64). Needs a network. |

`scripts/build.sh` makes the standard Python package of the bot. `scripts/package.sh` makes the zip the game server's runners will run: your bot and its locked dependencies for Linux arm64 (`dist/island-battles-bot.zip`; see "Package your bot" in the SDK's README).

To upload the zip to your lobby, open the bot picker in the lobby page or the 3D client: **Upload bot (.zip)** takes the file, or **Upload from the command line** shows a command to copy, with the game's address and a token that is valid for 15 minutes:

```sh
uv run island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip
```

The game checks the package and lists it for everyone in the lobby; a new upload replaces your previous one. Choose the package as your bot in the bot picker, and it plays on the game's server, even with every tab closed. Uploaded bots stop starting new games once the month's free compute is used up.

## How your bot gets its turns

`scripts/serve.sh` runs `island-battles-bot serve`, the same protocol a bot hosted at a URL speaks:

1. Each turn, your game tab reads a **turn delivery** from the game: the turn (`BotTurn`: the board, your team, the upgrade table, your memory, the deadline) and a **submit token** that can submit exactly this turn of yours and nothing else.
2. The tab POSTs the delivery to `http://127.0.0.1:41414/turn`, or to the port you chose next to **localhost bot**. The bot server answers `202` at once, so nobody waits on your bot.
3. The bot server calls `play(turn)`, for at most `--turn-timeout` seconds (8 by default) and never past the turn's deadline, and submits your commands, log lines and memory to the game with the token.
4. If `play` raises or runs out of time, the bot **passes the turn**: no commands, your old memory, and one `error` line from `island_battles.bots` that says what happened, which you see in the game's **Bot logs**. The terminal shows the traceback. The game rejecting your commands passes the turn the same way, with its reason.

The bot server submits only to the official game, `https://island-battles.elidan.org`, and to `http://localhost` or `http://127.0.0.1` for a local backend; `scripts/serve.sh --allow-submit-host game.example.org` adds an `https` host, such as your own deployment of the game. To host the same server publicly, keep it on `127.0.0.1` behind a reverse proxy that terminates TLS and forwards `POST /turn` and `GET /health` with `Host: 127.0.0.1:41414`; the [SDK's README](https://pypi.org/project/island-battles-python/) has the details.

## Several of your bots in one game

To play two versions of your bot against each other, or against yourself, serve each on its own port and seat them as **localhost bots**:

1. Serve each one in its own terminal, such as this bot with `scripts/serve.sh --port 41415` and a copy of the project (or another module, `uv run island-battles-bot serve other:play --port 41416 --allow-origin https://lobby.elidan.org`) on 41416.
2. As the host, in the lobby page or the 3D client, add a bot to a free team with **My localhost bot (port)** and type its port, once per bot. A port is from 1024 to 65535, and each of your localhost bots, your own included, needs its own; the room says which ones answer.
3. Choose your own bot (a default level is fine, or your localhost bot on yet another port), get ready and start. Your tab relays the turns of every seat that you added to its port, so keep it open until the game ends; if you leave before the start, the next host must remove them before the game can start.

## Coding agents

`AGENTS.md` (and `CLAUDE.md`, which imports it) lists the project's rules and four skills in `.agents/skills/`: the game rules, how to write a bot, logging, and the workflow. Point your agent at them, or ask it to read `AGENTS.md` first.

## Where next

- The SDK, its helpers and the `island-battles-bot` command: [island-battles-python on PyPI](https://pypi.org/project/island-battles-python/).
- Ideas: attack weak neutral castles first, defend a castle that a group is walking to, store a plan in the memory, buy `clash_stop_generations`.
