---
name: bot-workflow
description: "The day-to-day loop for this bot project: the scripts and when to use each, playing a bot in a lobby against the default bots, reading a failed check, and debugging with a fixture turn or a saved demo. Load it before running, testing, building or debugging the bot."
---

# Bot workflow

All scripts are in `scripts/`, run from any directory, and use `uv`. Install first with `uv sync`.

| Script | Use it to |
|---|---|
| `scripts/check.sh` | Format check, lint, types, tests and a fixture turn. **Run it before you finish and before you play.** |
| `scripts/format.sh` | Fix formatting and safe lint errors, then run `check.sh`. |
| `scripts/serve.sh` | Serve the bot on `http://127.0.0.1:41414` for a browser game. |
| `scripts/run.sh` | Play a seat of a started lobby without a browser (needs a base URL, token and lobby id). |
| `scripts/fixture.sh [file]` | Write the fixture turn (default `turn.json`) to edit and replay. |
| `scripts/build.sh` | Build the package (wheel and source archive) into `dist/`. |

## Playing against the default bots

1. `scripts/serve.sh` and leave it running. Saving a `.py` file reloads the bot.
2. Open the lobby page (`https://island-battles.elidan.org`, or `http://localhost:8888/lobby/ui/` for a local backend). Create a lobby, pick a team, add a **bot seat** (default bot level 1 is the easiest) to the other team, and pick **localhost bot** as your bot.
3. Get ready and start. The page relays each turn to your bot, so keep the tab open. Failed turns show their error in the game log.
4. `curl http://127.0.0.1:41414/health` answers `{"name": "bot:play", "ok": true}` when the bot is up.

Without a browser: `scripts/run.sh BASE_URL TOKEN LOBBY_ID` (the guest token and lobby id come from the lobby flow). The player must have picked the localhost bot before getting ready.

## Reading a failed check

`check.sh` stops at the first failing step; fix that and run it again.

- `ruff format --check`: run `scripts/format.sh`. `ruff check`: the message names the rule (a missing docstring or annotation is a real error here; every function is typed and documented).
- `ty check`: a type error; fix the code rather than adding `# type: ignore`.
- `pytest`: the assertion says which property broke (valid result, small memory, same answer for same input).
- `island-battles-bot check bot:play` exits 1 with a message if the bot raises (with the traceback), returns an invalid result, returns memory over 64 KiB, or takes more than 2 seconds. It prints `ok: N commands, M log lines, memory B of 65536 bytes` on success.

`check` does not prove the commands are legal on a real board: the server rejects overspending (see `writing-a-bot`). Test your own boards.

## Debugging

- **Fixture turn:** `scripts/fixture.sh` writes `turn.json`. Edit it (move a castle, set `memory`, add a group), then `uv run island-battles-bot check bot:play --turn turn.json`. In tests, `from island_battles_client.fixture import fixture_turn, fixture_turn_data` gives the same turn as a model or as a dict to change before `BotTurn.model_validate`.
- **A saved demo:** the lobby page's replay has **Save demo**, which downloads the game as a JSON file. Its `frames` list is every generation's board (`castles`, `soldier_groups`, `clashes`). Pick a frame and wrap it in a `BotTurn` (`game_id`, `team_color`, the `upgrades` table from `fixture.sh`, `memory`, `turn_deadline_at: null`, `generation`), then run it with `--turn`.
- **POST a turn by hand:** `curl -X POST localhost:41414/turn -H 'Content-Type: application/json' -d @turn.json` while `serve.sh` runs.

## Building

`scripts/build.sh` makes a standard Python package in `dist/`. There is no way to upload a bot to the server yet; the bot always runs on your machine.
