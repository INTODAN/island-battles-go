# Island Battles Python bot client

## Installing from PyPI

Each release publishes this client to PyPI as `island-battles-python`, at the release's version (the import name is `island_battles_client`):

```sh
pip install island-battles-python==X.Y.Z   # or: uv add island-battles-python==X.Y.Z
```

It needs Python 3.14 or newer. To obtain the guest credential and the lobby ID, install the lobby client too, `mpl-lobby-client`, with the same version. The rest of this README describes the repository checkout.

**This is a rule change, and it breaks bots that count on neutral castles growing.** A neutral castle no longer gains a soldier each turn; it keeps its soldiers until a team captures it and grows from the next turn on, like any castle a team owns. The API is the same, but a bot that forecasts a neutral castle's defenders with growth overestimates them. What to do: count growth only for castles with a team.

A bot is a **function**: it gets a `BotTurn` (the board, your team and the `memory` it returned last turn) and returns a `BotTurnResult` (commands, game log lines and its new memory). The function keeps nothing between turns; whatever it needs next turn goes into its memory, which the server stores. This package runs your function three ways:

- `island-battles-bot serve` serves it on your machine, and a game client in your browser relays each turn to it,
- `island-battles-bot check` calls it once on a fixture turn,
- `island-battles-bot run` and `IslandBattlesClient` play a seat on their own, without a browser.

**This is a breaking change, and `BotTurn` now has a required `upgrades` field.** The `Player` class, `player_factory`, `CommandReceiver`, `UpgradeTable` and `client.upgrades` are gone: a strategy is now `play(turn)`. Pass it to `IslandBattlesClient(..., play=play)`. Port a strategy by moving `on_generation(generation, commands)` into `play(turn)` (the board is `turn.generation`, your team `turn.team_color`), returning the commands instead of sending them, and keeping state in `memory` instead of attributes. Update your bots before you upgrade the client.

**Choosing a bot is also a breaking change; clients must upgrade.** Every player now chooses a bot in the lobby before getting ready (`PUT /game/lobbies/{lobby_id}/bot`), and `GenerationState` has a required `bot` field (`{"kind": "localhost"}` or `{"kind": "default", "level": N}`). `IslandBattlesClient` plays a seat only for `kind` `localhost`; for a seat that the backend plays (`default`) it stops with an error instead of submitting. What to do: upgrade the client, and choose your localhost bot in the lobby (the lobby page has the bot picker, or call the endpoint with your guest credential) before you get ready; getting ready without a choice fails with 409 `game_requirement` of type `urn:island-battles:problem:bot-not-chosen`.

Install with `uv sync --locked` (Python 3.14 or later).

To start a bot project, copy the [Python bot template](../../templates/python-bot/README.md): a working bot, tests, scripts and skills for coding agents, on top of this package.

## Writing a bot

```python
from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    BotTurn,
    BotTurnResult,
    send_soldier_group,
    turn_result,
)

def play(turn: BotTurn) -> BotTurnResult:
    own = next(
        (c for c in turn.generation.castles if c.team_color == turn.team_color and c.soldiers > 0),
        None,
    )
    enemy = next(
        (c for c in turn.generation.castles if c.team_color not in (NEUTRAL_TEAM_COLOR, turn.team_color)),
        None,
    )
    commands = [send_soldier_group(own.id, enemy.id, 1)] if own and enemy else []
    return turn_result(commands, memory={"turns": (turn.memory or {}).get("turns", 0) + 1})
```

`play` can be `def` or `async def`. Put it in a module, such as `mybot.py`, and refer to it as `mybot:play`. The full example, with upgrades, logging and memory, is [examples/bot.py](examples/bot.py).

- `turn.generation` is the board (`castles`, `soldier_groups`, `clashes`), `turn.team_color` your team, `turn.game_id` the game, `turn.upgrades` the upgrade cost table, `turn.memory` what you returned last turn (`None` on the first turn and after a turn that returned none), and `turn.turn_deadline_at` when the turn times out, or `None`. A bot that doesn't answer by then makes its whole team lose.
- `turn_result(commands=(), logs=(), memory=None)` builds the result. The helpers `send_soldier_group(origin_castle_id, destination_castle_id, soldier_amount)` and `upgrade_castle(castle_id, upgrade_type)` build commands, and `log_line(message, level="info")` builds a game log line. They validate their arguments against the generated models and raise `pydantic.ValidationError` for invalid ones, before any request.
- **Memory** is any JSON object of at most 64 KiB as compact JSON (`MEMORY_LIMIT_BYTES` is 65,536 bytes). The completing batch saves it on the server, and the next turn's `turn.memory` is that memory. `check`, `serve` and the client raise `BotMemoryTooLargeError` for more, before any request.
- **Commands** run in order. The server checks them against the board and everything already reserved this turn, including your teammates' orders, and rejects the **whole result** if any command is invalid, such as sending soldiers a castle doesn't have. Count what your commands spend. `turn.upgrades` is the upgrade cost table, the list `GET /game/upgrades` returns: every type's levels with the `soldier_cost` of reaching each. `next_upgrade_cost(turn, castle, UpgradeType.speed)` gives what the castle's next level costs, or `None` at the maximum level (levels bought earlier in the same result aren't counted). Count the cost in what the castle spends.
- **Logs:** lines the bot logs with `island_battles_client.logger` (a standard `logging.Logger` named `island_battles.player`) while it plays, and the lines in `turn_result(logs=...)`, are stored in the game log; see [Logging](#logging).

### Serve it on your machine

```sh
uv run island-battles-bot serve mybot:play
```

serves the bot on `http://127.0.0.1:41414`, the Island Battles bot port (`--port` changes it). A game client relays each turn to it and submits the result:

- `POST /turn` takes a `BotTurn` as JSON (`Content-Type: application/json`, at most 2 MiB) and answers a `BotTurnResult`. An exception in the bot answers `500` with the traceback in the body, so the relaying client can show it in the game log. A turn that takes longer than `--turn-timeout` seconds (default 8, under the lobby's shortest turn timeout of 10) answers `504` and the server keeps serving; a bot stuck in a loop keeps its thread until you stop the server.
- `GET /health` answers `{"name": "mybot:play", "ok": true}`, so a client can show "bot connected".
- **The server reloads the module when a `.py` file in its directory changes**: edit, save, and the next turn uses the new code. It re-imports the bot module and the other modules in the same directory (never the standard library or site-packages), so keep a bot in its own directory rather than a shared one. A file that doesn't load answers `500` with the error until it is fixed.
- **Browsers:** the server answers CORS preflights, with `Access-Control-Allow-Private-Network: true` for Chrome's Private Network Access, only for the game's origins: `http://localhost:8888` (the local backend), `http://localhost:3000` (the web client's dev server) and `https://island-battles.elidan.org` (the deployed game), and `tauri://localhost` and `http://tauri.localhost` (the desktop app, whose requests go through Tauri's HTTP plugin and carry its web view's origin) always, and any other with `--allow-origin https://game.example.org` (repeatable). Requests from other origins, a `Host` other than `127.0.0.1:<port>` or `localhost:<port>` (DNS rebinding) get `403`, and a POST that isn't `application/json` gets `415`; the bot doesn't run, and the connection is closed after every refusal. Chrome, Edge and Firefox can reach `http://127.0.0.1` from an HTTPS page; Safari can't.

Try it with curl:

```sh
uv run island-battles-bot fixture > turn.json
curl -X POST localhost:41414/turn -H 'Content-Type: application/json' -d @turn.json
curl localhost:41414/health
```

### Check it

```sh
uv run island-battles-bot check mybot:play
```

calls the bot once on a fixture turn (or on your own `BotTurn` file with `--turn`) and exits with status 1, and its own message, if the bot raises (with the traceback), returns something that isn't a valid `BotTurnResult`, returns memory over 64 KiB, or takes longer than 2 seconds.

### Run a seat without a browser

```sh
uv run island-battles-bot run mybot:play --base-url http://localhost:8888 --token GUEST_BEARER_TOKEN --lobby-id LOBBY_UUID
```

plays one seat of a started lobby until the game ends, for CI and headless testing. `--log-file` and `--log-level` set the log output. The seat's player must have chosen their localhost bot in the lobby before getting ready (`PUT /game/lobbies/{lobby_id}/bot` with `{"kind": "localhost"}`; see the [backend README](../../backend/README.md#choosing-a-bot)): a player who chose a default level is played by the backend, and the client stops at once with a `GameError` that says so. `GenerationState.bot` reports which bot a seat plays with. The same in code:

```python
import asyncio

from island_battles_client import IslandBattlesClient
from mybot import play

async def main() -> None:
    client = IslandBattlesClient(
        "http://localhost:8888", "guest-bearer-token", "lobby-uuid", play=play
    )
    await client.run()

asyncio.run(main())
```

Keep the authenticated generic `LobbyClient` joined and running while the game client plays: it owns the lobby heartbeat and lease. Closing it can revoke game access after the lease expires. Obtain the guest credential and lobby ID through the generic lobby flow before starting this client.

To start a lobby on a chosen map, pass Island Battles' `game_settings` to the lobby client's `create_lobby`: `{"map_template_id": id}` for a default map from `GET /map/templates`, or `{"map": map_file}` for a map file, with exactly the map's number of teams. The player OpenAPI document publishes them as `LobbySettings`; the [backend README](../../backend/README.md#choosing-a-lobbys-map) has the rules.

## Models

`Generation` exposes `id`, `castles`, `soldier_groups`, and `clashes`. These models are generated from the backend's [player OpenAPI document](../../backend/api/openapi.json) into `island_battles_client/_generated.py`, so their fields, types and constraints match the server. They are frozen Pydantic models, and nested collections are tuples. Each `Clash` has a generation-local ID, a time fraction within the turn, and `ClashParticipant` records containing group IDs, teams, contact positions, radii, and soldier counts before and after combat. Castle and group IDs are never reassigned within a game, so a group ID in a clash never refers to a later group. Transport identity and submission status stay inside the client rather than leaking into the bot API. `client.generation`, `start()`, and `refresh()` expose the current `Generation` snapshot.

`Castle` and `SoldierGroup` provide `distance(other)` for straight-line distance in map units and `compare_team(other)` returning `TeamType.friendly`, `TeamType.neutral`, or `TeamType.enemy` (import `TeamType` from `island_battles_client`). A target with team color `0` is always neutral, including when both entities are neutral. Otherwise, equal team colors are friendly and different colors are enemies. These utilities are the only model code written by hand; they live in `_entities.py`, which the generated classes extend.

`SoldierGroup.destination_castle` is the castle with `destination_castle_id` in the same generation, linked when the generation is validated; a generation whose group points at a missing castle is rejected. `distance_left()` takes no arguments and measures to that castle. Distances are floating-point map units, not travel times or generation counts; speed and clash radius do not affect them.

## The client

`IslandBattlesClient` owns authentication, the personal player-channel subscription, reconnect recovery, state reconciliation, and turn completion. It calls `play` serially, once per generation, with a `BotTurn` built from the authorized state (`memory` is the state's `memory`, `turn_deadline_at` its deadline, and `upgrades` the table the client reads from `GET /game/upgrades` once and reuses). When `play` returns, the client sends the result as the turn's **one completing batch**: the commands, the game log lines and the memory, with `complete: true`. An exception, a result that isn't valid, memory over 64 KiB, or a server rejection leaves the turn open and stops the client; `run()` and `wait()` raise the error. A bot that raised, or whose result the server rejected, first sends its error and its remaining log lines in a last batch.

A rejected result raises a `GameCommandError`: the server's `urn:island-battles:problem:` types `castle-not-found`, `castle-not-owned`, `upgrade-max-level` and `insufficient-soldiers` raise `CastleNotFoundError`, `CastleNotOwnedError`, `UpgradeMaxLevelError` and `InsufficientSoldiersError`, each with the HTTP `status` and `detail`; `bot-memory-too-large` raises `BotMemoryTooLargeError`. In team games teammates share castles, so count carefully: what your teammates reserved this turn isn't on your board.

Castles and soldier groups carry `upgrades`, which maps every `UpgradeType` (`UpgradeType.speed`, `UpgradeType.clash_stop_generations`) to its level; a group copies its origin castle's levels when it leaves. `UpgradeType` is a `StrEnum`, so `castle.upgrades["speed"]` works too.

When the game ends, the client receives the final generation with its outcome, from the `game.ended` event or an HTTP read. It does not call `play` for that generation. It calls the optional `on_game_ended(outcome, generation)` (sync or async) once with the `GameOutcome` (`reason`, `winner_team_color`, which is `None` for a draw, and a `summary` sentence) and the final board, and then `wait()` and `run()` return. `client.outcome` holds the outcome afterwards. A client started on a finished game ends the same way.

Turns have a deadline, which the lobby's host sets: 30 seconds unless the lobby chose another `turn_timeout_seconds`, or none. When a turn's deadline passes before every player completed it, each team with a player who hadn't loses, and the game ends with reason `turn_timeout` and a summary such as `Red lost: Sam's bot didn't answer within 30 s; Blue wins` (see [turn deadlines](../../backend/README.md#turn-deadlines)). The server runs no timers, so once this client has completed a turn and waits for the others, it posts to `/game/from-lobby/{lobby_id}/deadline` a second after the state's `turn_deadline_at`, unless a new generation arrives first; if the turn is still open then, it asks again after 2 seconds, doubling up to 30. The answer ends the game as above, and `on_game_ended` runs. A slow `play` whose turn passes its deadline gets `GameEndedError`, the server's `urn:island-battles:problem:game-ended`, when its result is sent; the client handles it, leaves the turn and calls `on_game_ended` instead of failing. The client's `clock` argument, which returns an aware `datetime`, is the clock it compares deadlines with.

The client creates the request UUID internally and reuses the exact request when retrying a transport failure. Authentication, request IDs, and completion belong to the client. It also deduplicates repeated events and reconciles authorized HTTP state after subscription or reconnect. No periodic polling is enabled by default; `refresh()` can request an explicit read.

The full HTTP contract, authorization rules, reservation checks, retry behavior and status codes are documented in the [backend README](../../backend/README.md), the bot contract in its [bot turns](../../backend/README.md#bot-turns). The browser's **New demo lobby** uses its own JavaScript player strategy. See the [demo walkthrough](../../backend/README.md#browser-demo).

## Logging

`island_battles_client.logger` is a standard `logging.Logger` named `island_battles.player`. Log with it as with any logger, or return lines with `log_line` in the result:

```python
from island_battles_client import logger

def play(turn):
    logger.info("generation %d: %d castles", turn.generation.id, len(turn.generation.castles))
    ...
```

Helper modules can use child loggers, such as `logging.getLogger("island_battles.player.planner")`, whose records propagate to it. From `start()` to `close()` the client attaches three handlers to the `island_battles` logger, the parent of the bot's logger and of the SDK's own `island_battles.sdk`:

- **stdout**, unless `log_to_stdout=False`;
- **a file**, when `log_file` is set; lines are appended;
- **the game log**, always. The client has no option to turn it off.

```python
client = IslandBattlesClient(
    base_url, credential, lobby_id,
    play=play,
    log_level=logging.DEBUG,  # the default is logging.INFO
    log_file="bot.log",
)
```

`log_level` applies to all three handlers. The client lowers the `island_battles` logger's level to `log_level` when it is higher, and the last client to close restores it. The `island_battles` logger propagates to the root logger as usual, so turn `log_to_stdout` off if your program already prints the root logger's records.

The **game log** stores the lines that the bot logs while it plays, and the lines in its result's `logs`, on the server, with the generation they belong to. A synchronous bot runs in a worker thread, and its records are filed the same way. The replay in the lobby page shows each generation's lines: your own team's while the game runs, and every team's once it has ended. A record is filed under the turn that is playing, through a `contextvars.ContextVar`, so tasks that the bot starts log into it too, and two clients in one process never mix their lines. Records logged outside a turn go to stdout and the file only: in `on_game_ended`, or by the client itself. The lines the bot logs come first, then those in `logs`.

The completing request carries the turn's lines. A retried request resends the identical lines. A line logged with an exception, such as `logger.exception(...)`, keeps its traceback, which the game log stores with the exception first and the innermost frames next. When `play` raises, or its result is rejected, the client logs the exception with its traceback at `error` as `island_battles.sdk`, and sends the generation's remaining lines in a last batch with no commands that leaves the turn open, before it stops.

The server stores at most **100 lines per player per generation**, each of at most **500 characters**. The client applies these limits before it sends anything, and a logging call never raises, so the bot keeps playing:

- A longer line is cut so that, ending in `… (cut at 500 characters)`, it is 500 characters.
- Past 100 lines, the client drops the rest. It writes `game log limit reached for generation 41: 100 lines per generation; the rest of this generation's lines are not stored` to stdout and the file once, when the first line is dropped. The game log stores, as the generation's last line, `game log limit reached for generation 41: 100 lines per generation; 7 lines were not stored`, at level `error` from `island_battles.sdk`, in place of the 100th line, so a generation never has more than 100. Anyone reading the replay can see why the lines stop.
- A client that starts again during a turn, for example after `play` raised, counts on from the lines its earlier run stored for the generation, which the generation state's `log_lines_stored` reports.
- If the server still refuses a batch's lines with `urn:island-battles:problem:game-log-limit`, for example because another client of the same player logged meanwhile, the client drops them, writes the limit error to stdout and the file, sends no more lines for the generation, and sends the batch's commands and completion again without lines. The bot never sees this error.
- Empty lines are not stored in the game log. A NUL character or an unpaired surrogate is stored as `�`.

The handler reports its own failures, such as a message whose arguments don't match its format, through `logging`'s `handleError`, as standard handlers do.

The game log needs a server that accepts `logs` with each batch; an older server ignores them. 

## Regenerating the models

Change the Go API types in `backend/models`, then export the document and regenerate:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py
```

Pass `--check` to `generate.py` to fail when either file is stale.

## Verification

Client behavior is verified by the language-agnostic [client conformance suite](../conformance/README.md), through [conformance/adapter.py](conformance/adapter.py). The `island-battles-bot` commands, the reloading server and its CORS answers have Python tests in [tests/](tests/). From the repository root:

```sh
uv run --project clients/island-battles-python --locked pytest clients/island-battles-python/tests
uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py
./scripts/test-client-conformance.sh
```

The SDK, adapter and example are checked with Ruff and ty from this directory:

```sh
uv run --locked ruff check island_battles_client examples scripts conformance tests
uv run --locked ruff format --check island_battles_client examples scripts conformance tests
uv run --locked ty check island_battles_client examples conformance --error-on-warning
```
