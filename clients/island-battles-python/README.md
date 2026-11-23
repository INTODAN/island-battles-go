# Island Battles Python bot client

## Installing from PyPI

Each release publishes this client to PyPI as `island-battles-python`, at the release's version (the import name is `island_battles_client`):

```sh
pip install island-battles-python==X.Y.Z   # or: uv add island-battles-python==X.Y.Z
```

It needs Python 3.14 or newer. To obtain the guest credential and the lobby ID, install the lobby client too, `mpl-lobby-client`, with the same version. The rest of this README describes the repository checkout.

**Bots from the leaderboard play away (#412), a breaking change of the API.** A member's bot may name a listed bot by its competitor ID, `{"bot": {"kind": "hosted", "competitor_id": "…"}}`, and a seat may be one, `{"bot_competitor_id": "…"}`, which plays at the bot name's URL. In a rated lobby such a bot plays away from its owner, within daily caps, and a missed turn pauses it until its owner resumes it. `BotChoice.name` is then the listed bot's name. The new fields and problem types (`rated-bot-not-listed`, `rated-bot-paused`, `rated-pair-limit`, `rated-bot-day-limit`) are in the [backend README](../../backend/README.md#listed-bots-away-from-their-owners); upgrade to read them.

**A bot can learn how its game ended and download its demo (#413), a new feature, not a breaking change.** Define `game_ended(delivery)` (or `async def`) next to `play` in your bot's module, and it gets a `GameEndedDelivery` once the game has its outcome, however it ended and also after your team was eliminated: the outcome, the final board as your team saw it, your last memory and `demo`, a link that downloads the game's demo with a plain GET until `demo.expires_at`; `download_demo(delivery, "demo.json")` saves it. `serve` takes it at its new `POST /game-ended`, the worker of uploaded bots as the line `{"game_ended": ...}`, and `run` reads it from `GET /game/from-lobby/{lobby_id}/game-ended-delivery`; see [When the game ends](#when-the-game-ends). What to do: nothing, unless you want it. A bot without `game_ended` plays as before, and an older SDK answers the new request with 404, 422 or an error line, which costs nothing: the game has ended either way. The generated models add `GameEndedDelivery` and `DemoLink`.

**Rated bots are hosted bots (#400), a breaking change of the API.** In a rated lobby a named bot must be a hosted bot, `{"bot": {"kind": "hosted", "name": "Kraken"}}` in the member settings, which plays at the URL that its owner registered with the name in the 3D client's account page; a named localhost or uploaded bot is refused with 409 `game_requirement` of type `urn:island-battles:problem:rated-hosted-bot-required`. A listed bot name's URL is public on the leaderboard, so anyone may play against it as a hosted bot seat of an unrated lobby. The generated models change with it: `MemberBot.url` may be left out of a named hosted bot, and `Competitor`, `LeaderboardEntry` and `LeaderboardCompetitor` have a `url`. What to do: upgrade the SDK. `serve` and `run` are unchanged, and lobbies that aren't rated take every bot as before.

**Rated lobbies, a breaking change of the state (#365).** A lobby's host can make it rated (`{"rated": true}` in its `game_settings`). `GenerationState` has a required `rated`, and `GenerationState.bot` a `name`: the bot name that a localhost, hosted or uploaded bot plays as in a rated game, one of its player's bot names (`{"bot": {"kind": "localhost", "name": "Kraken"}}` in the member settings). Only players signed in with Google in the 3D client play in a rated lobby, so `island-battles-bot run`, which plays as a guest, can't: a guest who gets ready there with the lobby client is refused with 409 `game_requirement` of type `urn:island-battles:problem:rated-sign-in-required`, and the `LobbyError` reads "game_requirement: This lobby is rated. Sign in with Google in the 3D client to play in it." What to do: upgrade the SDK, whose models require `rated`; play your bot in lobbies that aren't rated.

**Leaving a real-time game, a breaking change of the outcome and the state (#347).** In a real-time game a player can resign (`POST /game/from-lobby/{lobby_id}/resign`), a player the server hasn't heard from for the lobby's turn limit has left, and a game that nobody plays for that long is abandoned. A team that resigns or leaves is out: its castles turn neutral and keep their soldiers, and its soldier groups vanish, with no clash. The outcome has a new reason, `abandoned`, and `departures`, the teams that resigned or left; `GenerationState` has `departures` too. Every game request made as your player, with a player token or a submit token and late ones included, counts as hearing from it, so a bot that answers each turn it is given never leaves; one that stops answering does. A bot whose team resigned or left has nothing to play: its batches are refused with `team-out`, and `run` stops there. What to do: upgrade the SDK. Older ones read the new states, which only add a field, but fail on the final state of an abandoned game, whose reason they don't know. This SDK needs a server that sends `departures`.

**In a real-time game a slow bot passes its turn, and a late batch has a problem type (#353).** A batch for a turn that has ended is refused with 409 and the problem type `urn:island-battles:problem:stale-generation`, in both kinds of game, which raises `StaleGenerationError`, a `GameError` as the untyped 409 was. In a real-time game the client takes it as a passed turn: it writes one line, such as `the bot was slower than the turn pace of 1 s on generation 12, so the turn was played without its answer and it passed this turn`, sends no failure batch and plays the next turn instead of stopping. In a game that waits for every player, `run()` raises it as before. `serve` now stops the bot 1 s before `deadline_at`, or a quarter of the time left when that is less, so `play` has three quarters of a short turn: 0.375 s at a pace of 0.5 s, 0.75 s at 1 s, 1.5 s at 2 s and 4 s at 5 s. Before, it had nothing at a pace of 1 s or less and passed every turn. The same margin applies to a game that waits for every player when less than 4 s of the turn is left. What to do: upgrade the SDK before your bot plays a real-time game, and keep `play` under three quarters of the pace; under `run` it has until the turn's deadline, less the time its answer takes to arrive.

**Real-time games, a breaking change of the state (#339).** A game with a player who plays by hand is real-time: each turn ends after the lobby's turn pace (`turn_pace_milliseconds`, 1 s by default, 0.5 to 5 s), and the server then plays it with the orders that arrived. A bot that answers later passes that turn, and its late batch is refused with 409 as a stale generation; nobody loses for a missed turn. `GenerationState` has the required `turn_pace_milliseconds`, `null` in a game of bots, which keeps the turn limit. `turn_deadline_at` is the turn's end in both kinds of game. What to do: upgrade the SDK, whose state reads fail without the field, and keep `play` well under the pace if your bot may meet a hand player.

**A new bot kind, `manual`, for a player who plays by hand, a breaking change (#338).** `GenerationState.bot.kind` can be `manual`: a person plays the seat in the 3D client, so nothing plays or relays it. `run` refuses that seat with "this seat is played by hand in the 3D client". What to do: upgrade the SDK, whose models reject a kind they don't know; nothing changes for a localhost bot's seat.

**Turns carry their turn number and the game's last generation (#309).** `BotTurn` and `GenerationState` have the required `turn_number`, 1 on the game's first turn and one more each turn, and `max_generations`, the game's last generation (300 unless the host chose another length): the turn that creates it ends the game, so `turn.max_generations - turn.turn_number` turns are left, this one included. `turn.generation.id` is the server's ID of the generation, which every game shares, so it doesn't start at 1 and isn't the turn number. Older SDKs ignore the new fields, but this one requires them: `BotTurn(...)`, `BotTurn.model_validate` and `check --turn` reject a turn without them. What to do: use `turn.turn_number` instead of counting turns or reading `generation.id`, and in your tests add `turn_number` and `max_generations` to the turns you build, or build them from `fixture_turn_data()`; print the fixture again (`island-battles-bot fixture`) for a turn file saved from an older SDK.

**Two new upgrades and a burst command, a breaking change of the turn's board (#281).** `generation_amount` sets what a castle produces each turn (1 to 4 soldiers), and `burst_potential` the largest burst it may order (0, 4, 8 or 16 soldiers); both cost 10, 20 and 30 soldiers for levels 2 to 4. The new command `burst_castle` orders a burst: the very next generation adds it on top of the castle's production and makes it the castle's `burst_debt`, which later production repays before the castle grows again. Every castle now has `burst_debt`, and every `upgrades` map lists the four types. What to do: upgrade the SDK, since older ones refuse every turn whose upgrades list the new types, and count each castle's production and debt where your bot forecasts growth.

**This is a rule change, and it breaks bots that count on neutral castles growing.** A neutral castle no longer gains a soldier each turn; it keeps its soldiers until a team captures it and grows from the next turn on, like any castle a team owns. The API is the same, but a bot that forecasts a neutral castle's defenders with growth overestimates them. What to do: count growth only for castles with a team.

A bot is a **function**: it gets a `BotTurn` (the board, your team and the `memory` it returned last turn) and returns a `BotTurnResult` (commands, game log lines and its new memory). The function keeps nothing between turns; whatever it needs next turn goes into its memory, which the server stores. This package runs your function three ways:

- `island-battles-bot serve` serves it on your machine: a game client in your browser delivers each turn to it, and it submits its answer itself,
- `island-battles-bot check` calls it once on a fixture turn,
- `island-battles-bot run` and `IslandBattlesClient` play a seat on their own, without a browser.

`island-battles-bot package` zips the bot for the server's runners, `island-battles-bot upload` uploads that zip to a lobby, and `island-battles-bot match` plays bots that the server runs, such as your package and someone's hosted bot, against each other on map files, without a browser.

**Localhost bots take a port, and the host can seat several of them, a breaking change for clients of the game API (#286).** `GenerationState` has a required `relayed_bots`, the localhost bot seats that you added as the host, which your browser tab relays, and `GenerationState.bot` a localhost bot's `port`. A player's localhost bot may serve on any port from 1024 to 65535 (`serve --port N`) and say so in the lobby, and the host can seat their own localhost bots, each on its own port, to play them against each other. What to do: upgrade the SDK, whose models read the new fields; `run` still plays one person's seat.

**Every Island Battles lobby needs a map, a breaking change for code that creates lobbies (#277).** The generated map is gone: `create_lobby` and the lobby's settings change refuse Island Battles `game_settings` without `map_template_id` or `map`, and leaving them out, with `invalid_game_settings`. What to do: pass a map, such as the first default map from `GET /map/templates`; see [Run a seat without a browser](#run-a-seat-without-a-browser) below.

**The official lobby moved to its own site, `https://lobby.elidan.org`, a breaking change for clients of the official game (#232).** The game stays at `https://island-battles.elidan.org`, which no longer serves the lobby: its `/lobby/v1/*` answers `410 Gone`, and its `/lobby/ui/` redirects to the same path on `https://lobby.elidan.org`. What to do: give `run` both addresses, `--game-url https://island-battles.elidan.org --lobby-url https://lobby.elidan.org` (`game_url=` and `lobby_url=` for `IslandBattlesClient`); `--base-url https://island-battles.elidan.org` no longer works. Upgrade the SDK for `serve` too: its default origins now include the lobby page's, `https://lobby.elidan.org`, and an older `serve` needs `--allow-origin https://lobby.elidan.org` before the lobby page can deliver turns to it.

**The lobby and the game are separate sites, a breaking change for the client's configuration (#228).** The lobby gets its own address (such as `https://lobby.example.org`), apart from the game's (such as `https://island-battles.example.org`). `IslandBattlesClient(credential, lobby_id, play=..., game_url=..., lobby_url=...)` sends the lobby's routes (`/lobby/v1/*`: player tokens and the Centrifugo connection token) to `lobby_url`, and the game's (`/game/*`, `/generation/*`) to `game_url`. `base_url=...` stays as the shorthand that sets both, for a server that serves the game and the lobby together; the official game's are two sites (see #232 above). `island-battles-bot run` takes `--game-url` and `--lobby-url`, or reads `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL`, and `--base-url` stays as the shorthand. What to do: upgrade the SDK, and pass the URLs as keywords: the first positional argument is now the credential.

**Two more bot kinds that the server plays, `hosted` and `uploaded` (#222).** Where the server offers them (`server_bots` in `GET /bots/defaults`), a player's bot, and a bot seat's, can also be their own bot server at a public URL, `{"bot": {"kind": "hosted", "url": "https://…"}}`, or one of their bot packages in the lobby, `{"bot": {"kind": "uploaded", "package_id": "…"}}`. `GenerationState.bot.kind` can then be `hosted`, with the URL's `host`, or `uploaded`. `run` refuses those seats, as it does a default bot's, and says which bot the server plays. An older SDK stops on such a seat with a protocol error instead; nothing changes for a localhost bot's seat.

**The game's routes take a player token, a breaking change for every client of the game API (#218).** `GET /game/from-lobby/…`, `POST /game/from-lobby/…/deadline` and `POST /generation/commands` refuse the guest credential (401): they take `Authorization: Bearer <player token>`, a short-lived token from the lobby's `POST /lobby/v1/player-tokens` with `{"game_key": "island-battles"}` (`issue_player_token` in the lobby client). `IslandBattlesClient` still takes the guest credential: it gets the player token itself, renews it at 80 % of its life, and once more when the game answers 401. What to do: upgrade the client; code that calls the game's routes itself gets a player token first.

**This is a breaking change, and `BotTurn` now has a required `upgrades` field.** The `Player` class, `player_factory`, `CommandReceiver`, `UpgradeTable` and `client.upgrades` are gone: a strategy is now `play(turn)`. Pass it to `IslandBattlesClient(..., play=play)`. Port a strategy by moving `on_generation(generation, commands)` into `play(turn)` (the board is `turn.generation`, your team `turn.team_color`), returning the commands instead of sending them, and keeping state in `memory` instead of attributes. Update your bots before you upgrade the client.

**Turn deliveries are a breaking change for `serve`; upgrade the SDK.** `POST /turn` now takes a `TurnDelivery` (the turn plus a one-turn submit token), answers `202` at once, and submits the bot's answer to the game itself; it no longer answers a `BotTurnResult`. The 3D web client and the lobby page deliver turns this way, so an older SDK's `serve` doesn't work with them, and this SDK's `serve` doesn't work with older game clients. A bot that raises or runs out of time now passes its turn instead of leaving it open. What to do: upgrade `island-battles-python` (and the Go SDK, for Go bots); your `play` function doesn't change.

**The bot choice moved into the lobby's member settings, a breaking change for clients that chose a bot through the game.** `PUT /game/lobbies/{lobby_id}/bot` and `GET /game/lobbies/{lobby_id}/bots` are gone, and so are the `LobbyBots` and `LobbyMemberBot` models: a player's bot is their lobby member settings, `{"bot": {"kind": "localhost"}}` or `{"bot": {"kind": "default", "level": N}}`, set with the lobby's `setLobbyMemberSettings` (`set_member_settings` in the lobby client) and shown in every snapshot member's `member_settings`. `IslandBattlesClient` reads the bot from `GenerationState.bot`, as before. What to do: choose your bot with the lobby's endpoint.

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

- `turn.generation` is the board (`castles`, `soldier_groups`, `clashes`), `turn.turn_number` the turn's number in the game (1 on its first turn; `turn.generation.id` is the server's ID of the generation, which doesn't start at 1), `turn.max_generations` the game's last generation (the turn that creates it ends the game, so `turn.max_generations - turn.turn_number` turns are left, this one included), `turn.team_color` your team, `turn.game_id` the game, `turn.upgrades` the upgrade cost table, `turn.memory` what you returned last turn (`None` on the first turn and after a turn that returned none), and `turn.turn_deadline_at` when the turn ends, or `None`. A bot that doesn't answer by then makes its whole team lose, or, in a real-time game with a player who plays by hand, where the turn ends after the turn pace (1 second by default), passes the turn.
- `turn_result(commands=(), logs=(), memory=None)` builds the result. The helpers `send_soldier_group(origin_castle_id, destination_castle_id, soldier_amount)`, `upgrade_castle(castle_id, upgrade_type)` and `burst_castle(castle_id, soldier_amount)` build commands, and `log_line(message, level="info")` builds a game log line. They validate their arguments against the generated models and raise `pydantic.ValidationError` for invalid ones, before any request.
- **Memory** is any JSON object of at most 64 KiB as compact JSON (`MEMORY_LIMIT_BYTES` is 65,536 bytes). The completing batch saves it on the server, and the next turn's `turn.memory` is that memory. `check`, `serve` and the client raise `BotMemoryTooLargeError` for more, before any request.
- **Commands** run in order. The server checks them against the board and everything already reserved this turn, including your own earlier batches, and rejects the **whole result** if any command is invalid, such as sending soldiers a castle doesn't have. Count what your commands spend. `turn.upgrades` is the upgrade cost table, the list `GET /game/upgrades` returns: every type's levels with the `soldier_cost` of reaching each. `next_upgrade_cost(turn, castle, UpgradeType.speed)` gives what the castle's next level costs, or `None` at the maximum level (levels bought earlier in the same result aren't counted). Count the cost in what the castle spends.
- **Production and bursts.** Each turn a castle that a team owns produces its `generation_amount` value, `production(turn, castle)`. `burst_castle(castle_id, soldier_amount)` orders a burst of 1 up to the castle's `burst_potential(turn, castle)`, counting levels bought in the same result, before or after it, for a castle that `can_burst(castle)`: one without `burst_debt`. The very next generation adds the burst on top of the production and makes it the castle's `burst_debt`, which later production repays before the castle grows again. A castle bursts at most once a turn, and a capture clears its debt.
- **Logs:** lines the bot logs with `island_battles_client.logger` (a standard `logging.Logger` named `island_battles.player`) while it plays, and the lines in `turn_result(logs=...)`, are stored in the game log; see [Logging](#logging).
- **The end of the game:** an optional `game_ended(delivery)` function in the same module says how the game ended; see [When the game ends](#when-the-game-ends).

### When the game ends

```python
from island_battles_client import GameEndedDelivery, download_demo, logger

def game_ended(delivery: GameEndedDelivery) -> None:
    logger.info("game %d: %s", delivery.game_id, delivery.outcome.summary)
    download_demo(delivery, f"game-{delivery.game_id}.json")
```

A function named `game_ended` in the module of `mybot:play`, `def` or `async def`, gets the bot's `GameEndedDelivery` once its game has an outcome, however the game ended, also when its team was eliminated before the end (and, under `serve` and the worker, when it resigned or left a real-time game; `run` stops at once then, without it). `serve`, the worker of an uploaded package and `run` all call it; the module needs no other change, and a module without one plays as before (a `game_ended` that isn't a function, such as a flag, is ignored). It returns nothing, and nothing it does changes the game, which is over: it can't send commands.

- `delivery.game_id`, `delivery.team_color` (your team), `delivery.turn_number` (the final generation's turn number), `delivery.outcome` (the `GameOutcome`: `reason`, `winner_team_color`, `summary`, `departures`), `delivery.generation` (the final board, as a turn shows it to your team) and `delivery.memory` (the memory of your latest completed turn, or `None`).
- `delivery.demo` is a `DemoLink`: `url`, `GET /game/{game_id}/demo` on the game with a read-only demo token in its `token` query parameter, and `expires_at`, when the game is deleted (7 days after it ended by default). A plain GET with no headers downloads the game's [demo](../../backend/README.md#demos) as you saw it, the `Demo` JSON; the link stands for you, so keep it private. `download_demo(delivery, path=None, *, timeout=60.0)` downloads it, unpacks a gzip answer, writes it to `path` when given and returns the JSON bytes, which `Demo.model_validate_json` reads. It blocks: from an `async def game_ended`, call `await asyncio.to_thread(download_demo, delivery)`. It raises `GameError` with the status when the game refuses the link, such as after `expires_at`, and `GameTransportError` when the game doesn't answer.
- Lines logged with `logger` go to the terminal (under `serve` and `run`), not to the game log: the game has ended.
- Under `serve` and in the worker, an exception in `game_ended` is printed or reported to the runner and changes nothing; under `run` it is logged at `warning`. The worker of an uploaded bot has 5 seconds for it, after which the runner stops the worker.

### Serve it on your machine

```sh
uv run island-battles-bot serve mybot:play
```

serves the bot on `http://127.0.0.1:41414`, the Island Battles bot port (`--port` changes it: type the same port next to your localhost bot in the lobby page or the 3D client). Your game client, the lobby page (`https://lobby.elidan.org/lobby/ui/`) or the 3D web client (`https://island-battles.elidan.org/play/`) with your localhost bot chosen, delivers each turn to it, and the bot submits its answer to the game itself; nobody waits on the bot:

- `POST /turn` takes a `TurnDelivery` as JSON (`Content-Type: application/json`, at most 2 MiB): `{"turn": <BotTurn>, "submit": {"url", "token", "deadline_at"}}`, which the client reads from `GET /game/from-lobby/{lobby_id}/turn-delivery` (see [turn deliveries](../../backend/README.md#turn-deliveries)). It answers `202 {"accepted": true}` at once, then plays the turn in the background and submits `{game_id, generation_id, request_id, commands, logs, memory, complete: true}` to `submit.url` with `Authorization: Bearer <submit.token>`, a token that submits this one turn and nothing else. A turn delivered again while it is being played is accepted and played once.
- **The bot has `--turn-timeout` seconds (default 8)**, or less: it stops before `deadline_at` by 1 second, or by a quarter of the time left when that is less, so the answer arrives in time. In a real-time game, whose deadline is one turn pace away, the bot has three quarters of the pace: 0.375 s at 0.5 s, 0.75 s at 1 s. A slower bot passes the turn, as below. The delivery doesn't say which kind of game it is, so the same margin applies when a game that waits for every player has less than 4 s of the turn left. A lobby's turn timeout is 30 seconds by default, and its host can set it from 5 to 600 seconds or to none.
- **A bot that raises, or runs out of time, passes its turn**: the server submits a completing batch with no commands, the memory it was given, and one `error` line from the logger `island_battles.bots` that says what happened, such as `The bot failed on generation 12, so it passes this turn: ValueError: no plan`. You see it in the game's **Bot logs**, and the terminal shows the traceback. A bot stuck in a loop keeps its thread until you stop the server. When the game server rejects the answer (an invalid command, or too much memory or too many log lines), the bot passes the turn the same way, with the server's reason. Without an answer from the game server, or after a server error, the same request is sent up to four times.
- **Where it submits:** only to `https://island-battles.elidan.org`, the official game, and to `http://localhost` and `http://127.0.0.1` on any port, for a local backend. `--allow-submit-host game.example.org` (repeatable; a name, or `name:port`) adds an `https` host, such as your own deployment. A delivery for any other `submit.url` answers `400` with the reason, and nothing is played or sent, so a page can't make your bot send its token elsewhere.
- `POST /game-ended` takes a `GameEndedDelivery` as JSON (`Content-Type: application/json`, at most 2 MiB), once the game has its outcome: the browser tab that relays your turns posts it, from `GET /game/from-lobby/{lobby_id}/game-ended-delivery`, and the game's server posts it to a hosted bot, to its URL with the last path segment replaced by `game-ended` (`https://bot.example.com/turn` gets `https://bot.example.com/game-ended`). It answers `202 {"accepted": true}` at once, prints the outcome, and calls the module's `game_ended`, if it has one, in the background ([When the game ends](#when-the-game-ends)); nobody waits for it, and a game's end delivered again for the same team is accepted and taken once. A body that isn't a `GameEndedDelivery` gets `422`, and a `demo.url` that isn't on a host the bot submits to (below) gets `400`, and `game_ended` doesn't run. The origin and `Host` rules are those of `POST /turn`. An older `serve` answers `404`, which the tab and the game ignore.
- `GET /health` answers `{"name": "mybot:play", "ok": true}`, so a client can show "bot connected".
- **Several bots at once:** serve each on its own port, such as `--port 41415` and `--port 41416`, and, as the lobby's host, seat each as a localhost bot with its port (#286). Your tab delivers every seat that you added its turn on its port, with the seat's own submit token; a later host's tab never does.
- **The server reloads the module when a `.py` file in its directory changes**: edit, save, and the next turn uses the new code. It re-imports the bot module and the other modules in the same directory (never the standard library or site-packages), so keep a bot in its own directory rather than a shared one. A file that doesn't load passes each turn, with the error in the line, until it is fixed.
- **Browsers:** the server answers CORS preflights, with `Access-Control-Allow-Private-Network: true` for Chrome's Private Network Access, only for the game's origins: `http://localhost:8888` (the local backend), `http://localhost:8890` (the local lobby's page), `http://localhost:3000` (the web client's dev server), `https://island-battles.elidan.org` (the deployed game's 3D client) and `https://lobby.elidan.org` (the deployed lobby's page), and `tauri://localhost` and `http://tauri.localhost` (the desktop app, whose requests go through Tauri's HTTP plugin and carry its web view's origin) always, and any other with `--allow-origin https://game.example.org` (repeatable). Requests from other origins, a `Host` other than `127.0.0.1:<port>` or `localhost:<port>` (DNS rebinding) get `403`, a POST that isn't `application/json` gets `415`, and a body that isn't a `TurnDelivery` gets `422`; the bot doesn't run, and the connection is closed after every refusal. Chrome, Edge and Firefox can reach `http://127.0.0.1` from an HTTPS page; Safari can't.

Deliver a turn by hand with curl, with your guest credential and a started lobby against a local backend. The game's routes take a player token from the lobby, not the guest credential:

```sh
PLAYER_TOKEN=$(curl -s -X POST localhost:8890/lobby/v1/player-tokens -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"game_key": "island-battles"}' | jq -r .token)
curl -s -H "Authorization: Bearer $PLAYER_TOKEN" localhost:8888/game/from-lobby/$LOBBY_ID/turn-delivery \
  | curl -X POST localhost:41414/turn -H 'Content-Type: application/json' -d @-
curl localhost:41414/health
```

**Hosting the same server publicly.** A localhost bot speaks the same protocol as a hosted bot: whoever delivers the turn POSTs a `TurnDelivery`, and the bot answers `202` and submits with the token. The game will deliver turns to bots at a URL itself once hosted bots arrive (#112). To run `serve` on a machine of your own, keep it bound to `127.0.0.1` and put a reverse proxy in front of it that terminates TLS and forwards `POST /turn`, `POST /game-ended` and `GET /health` with `Host: 127.0.0.1:41414` (server-to-server requests carry no `Origin`, so the origin list doesn't apply to them). The bot only submits to the allowed hosts above, so a delivery can't send your token or your bot's answer anywhere else.

### Check it

```sh
uv run island-battles-bot check mybot:play
```

calls the bot once on a fixture turn (or on your own `BotTurn` file with `--turn`) and exits with status 1, and its own message, if the bot raises (with the traceback), returns something that isn't a valid `BotTurnResult`, returns memory over 64 KiB, or takes longer than 2 seconds. On success it prints `ok: N commands, M log lines, memory B of 65536 bytes, T s`, where M counts the lines the game log would store for the turn: the lines the bot logged with the logger while it played, plus the lines in its result's `logs`.

### Package your bot

```sh
uv run island-battles-bot package [--entry bot:play] [--output dist/island-battles-bot.zip]
```

run in the bot's project, builds **one zip that the server's runners can run** without installing anything (server-run bots, #112). It needs [uv](https://docs.astral.sh/uv/) and a `uv.lock` (`uv lock`), and it checks first that the entry's module and function are in the project.

- **Target:** AWS Lambda, Linux arm64, the `python3.14` runtime. uv installs the locked dependencies for that platform (`--python-platform aarch64-manylinux_2_28 --python-version 3.14`) **from wheels only**, so it works from any machine. A dependency with no arm64 wheel stops the command with an error that names it. The bot project itself is built as a wheel and must be pure Python; the module of `--entry` must be in the wheel (the template's `only-include = ["bot.py"]` is).
- **Layout:** `island-battles-bot.json` at the root, `{"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}`, and `site/`, the project and its dependencies as for `PYTHONPATH=site`. The locked `island-battles-python` must be a version with the worker (`island_battles_client/worker.py`); the command says so if it isn't.
- **Size limits:** the zip at most 250 MiB, unpacked at most 1 GiB. Over either, the command fails and names the largest files.

**Worker mode.** The runner starts `python -m island_battles_client.worker bot:play` with `PYTHONPATH=<unpacked>/site` (not with `-I`, which ignores `PYTHONPATH`) and talks to it in JSON Lines over stdin and stdout, UTF-8:

1. The worker loads the bot and writes `{"ready": true}`. A bot that doesn't load gets one `{"error": "..."}` line and exit status 1.
2. For each turn the runner writes one line `{"turn": <BotTurn>}`; the worker answers one line, `{"result": <BotTurnResult>}` or `{"error": "<message>"}`. The result is checked as `check` checks it (memory size, commands), and the lines the bot logged come first in `logs`, as under `serve`.
3. When the game ends the runner writes one line `{"game_ended": <GameEndedDelivery>}` (#413). The worker calls the module's `game_ended`, if it has one, and answers one line, `{"ended": true}`, or `{"error": "<message>"}` when it raised; a module without one answers `{"ended": true}` at once. The runner allows it 5 seconds and submits nothing. A worker of an older SDK answers `{"error": ...}`, which costs nothing.
4. The bot's own stdout goes to stderr, so prints never corrupt the protocol. Stderr is free text.
5. The worker exits when stdin closes.

Try it on the fixture turn after unzipping (on Linux arm64; elsewhere use a build for your platform):

```sh
uv run island-battles-bot fixture | python -c 'import json,sys; print(json.dumps({"turn": json.load(sys.stdin)}))' > request.jsonl
PYTHONPATH=unpacked/site python -m island_battles_client.worker bot:play < request.jsonl
```

### Upload your package to a lobby

```sh
uv run island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip
```

uploads the zip that `package` wrote to a lobby (#221), where everyone in it sees it in the bot picker. Copy the command from the bot picker's **Upload from the command line**, in the lobby page or the 3D client: it fills in the game's address and an **upload token**, which stands for you in that lobby for 15 minutes, so the command line needs no session of yours. The zip goes straight to the game's storage, never through the game server; then the game checks it.

- **One package per member and lobby:** uploading a new one replaces your previous one. Any member may upload, and a package belongs to its lobby. Packages are deleted 8 days after their upload.
- **The checks** are those of `package`: the manifest (format 1, Python 3.14, a `module:function` entry), at most 250 MiB zipped and 1 GiB unpacked, and no path outside the package (`..`, absolute paths, symbolic links). A package that fails one is refused, and the command prints the game's reason and exits with status 1.
- An expired token, or one for another lobby, is refused: copy the command again. The browser pickers can also upload the zip themselves with **Upload bot (.zip)**.

Choosing an uploaded package as your bot comes later; for now it is only uploaded and listed.

### Run a seat without a browser

```sh
uv run island-battles-bot run mybot:play --game-url https://island-battles.elidan.org --lobby-url https://lobby.elidan.org --token=GUEST_BEARER_TOKEN --lobby-id LOBBY_UUID
```

plays one seat of a started lobby until the game ends, for CI and headless testing. `--game-url` is the game's address and `--lobby-url` the lobby's, which issued the guest credential: `https://island-battles.elidan.org` and `https://lobby.elidan.org` for the official game, `http://localhost:8888` and `http://localhost:8890` for a local backend and lobby. Without the flags, `run` reads `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL`. `--base-url` sets both, for a server that serves the game and the lobby together; the official game's are two sites, so give it both flags. `--log-file` and `--log-level` set the log output. The seat's player must have chosen their localhost bot in the lobby before getting ready (their lobby member settings `{"bot": {"kind": "localhost"}}`, set with `PUT /lobby/v1/games/island-battles/lobbies/{lobby_id}/member-settings`; see the [backend README](../../backend/README.md#choosing-a-bot)): a player who chose a default level, a hosted bot or an uploaded bot is played by the server, and the client stops at once with a `GameError` that says so. `GenerationState.bot` reports which bot a seat plays with. The same in code:

```python
import asyncio

from island_battles_client import IslandBattlesClient
from mybot import play

async def main() -> None:
    client = IslandBattlesClient(
        "guest-bearer-token",
        "lobby-uuid",
        play=play,
        game_url="https://island-battles.elidan.org",
        lobby_url="https://lobby.elidan.org",
    )
    await client.run()

asyncio.run(main())
```

Keep the authenticated generic `LobbyClient` joined and running while the game client plays: it owns the lobby heartbeat and lease. Closing it can revoke game access after the lease expires. Obtain the guest credential and lobby ID through the generic lobby flow before starting this client. The client sends the guest credential only to the lobby: for the game's routes it gets a player token from the lobby (`POST /lobby/v1/player-tokens`), renews it at 80 % of its life, and once more when the game answers 401.

To start a lobby, pass Island Battles' `game_settings` with its map to the lobby client's `create_lobby`: `{"map_template_id": id}` for a default map from `GET /map/templates`, or `{"map": map_file}` for a map file, with exactly the map's number of teams. Settings without a map are refused. The player OpenAPI document publishes them as `LobbySettings`; the [backend README](../../backend/README.md#choosing-a-lobbys-map) has the rules.

### Play bots against each other on map files

```sh
uv run island-battles-bot match --game-url https://island-battles.elidan.org --lobby-url https://lobby.elidan.org \
  --map maps/*.json --seat mine=package:dist/island-battles-bot.zip --seat rival=hosted:https://bot.example.org/turn
```

plays bots that the server runs against each other (#407), on each map file from every side, and saves each game's result and demo, without a browser. It does what a person does on the lobby page, with the lobby's and the game's own APIs, so the games are real games of that server, with its deadlines, its runners and its [compute guard](../../backend/README.md#monthly-compute-guard):

- **One guest per seat.** Each `--seat` gets a guest session, named after the seat, that joins one lobby on its own team and chooses the seat's bot as its own ([choosing a bot](../../backend/README.md#choosing-a-bot)). The first creates the lobby and hosts it. A seat is `hosted:URL`, a [hosted bot](../../backend/README.md#hosted-and-uploaded-bots) at that turn URL; `package:ZIP`, a zip that `package` wrote, which its guest uploads to the lobby once, before the first game; or `default:LEVEL`, a [default bot](../../backend/README.md#default-bots). `NAME=` before it names the seat, such as `mine=package:dist/island-battles-bot.zip`; otherwise it is named after the URL's host, the zip's file name or the level. Give one `--seat` per team: every map must be made for that many teams.
- **Every side.** Each map is played once per rotation of the seats: with two seats, each plays colour 1 once and colour 2 once, since maps are fair but turn order is not always. `--games N` plays every map from every side N times: with two seats and M map files, 2 × M × N games. A lobby is reopened between its games ([several games in one lobby](../../backend/README.md#several-games-in-one-lobby)), its map changed and its teams arranged, so it uploads a package only once. The guests renew their leases in the background and leave the lobby at the end.
- **In parallel.** `--parallel N` plays up to N games at once (1 by default), each in a lobby of its own with its own guests, which take the next game of the schedule as they finish one. Each lobby uploads its packages once.
- **Settings.** `--turn-timeout SECONDS` sets the lobby's turn timeout, 5 to 600, 30 when left out: a bot that doesn't answer a turn in time loses the game. `--max-generations N` sets the game's length, 50 to 1200, 300 when left out. The lobby isn't rated.
- **Output.** For each game, in `--output DIR` (`matches` by default), `ID.json`, the result, and `ID.demo.json`, the demo that **Save demo** writes, with every team's log lines, which the demo viewer opens. The same result goes to standard output as one JSON line, and progress to standard error. An ID is the map file's name and the side, such as `strait-1` and `strait-2`, with the repeat after it when `--games` is more than 1 (`strait-1-2`). A result has `id`, `map`, `players` (the seats' names in colour order), `outcome` (`reason`, `winner_team_color`, `summary`), `winner` (a seat's name, or null for a draw), `generations`, `lobby_id`, `lobby_round`, `game_id` and `demo`, the demo's path.
- **Failures.** A request that the lobby or the game refuses, such as a start refused because uploaded bots have used this month's compute, fails its game: no lobby starts another game, not even one that was still getting ready, the games already running in other lobbies play to their end and are saved, every guest leaves its lobby, and the command prints each server's reason and exits with status 1. The results already written stay. The command doesn't stop the running games at once because nothing can end a started game early; a lobby left alone would still play its game, with nobody saving it. Reading a game, its demo or a player token is tried again, up to 4 times 2 seconds apart, after a 5xx answer, such as a proxy's 502, or a dropped connection, before it fails its game. A game whose turn number doesn't advance for twice the turn timeout and a minute fails too, naming the turn, since its deadline should have ended the turn long before.
- **Ctrl+C** stops at once: every guest leaves its lobby and the command exits with status 130. A game that had started plays on at the server until it ends, unsaved.
- **Long runs.** A guest session lasts 24 hours, so a run longer than that loses its guests mid-run and fails; split a very long schedule into several runs, or raise `--parallel`.

The addresses are those of `run`: `--game-url` and `--lobby-url`, `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL`, or `--base-url`. A local stack (`./scripts/dev.sh`) takes a hosted bot on this machine, such as `hosted:http://127.0.0.1:41414/turn` from `island-battles-bot serve`, which the official game refuses. To measure bots quickly, without a server, use [headless matches](../../backend/README.md#headless-matches), which play workers and default levels in memory but can't play a hosted bot.

## Models

`Generation` exposes `id`, `castles`, `soldier_groups`, and `clashes`. These models are generated from the backend's [player OpenAPI document](../../backend/api/openapi.json) into `island_battles_client/_generated.py`, so their fields, types and constraints match the server. They are frozen Pydantic models, and nested collections are tuples. Each `Clash` has a generation-local ID, a time fraction within the turn, and `ClashParticipant` records containing group IDs, teams, contact positions, radii, and soldier counts before and after combat. Castle and group IDs are never reassigned within a game, so a group ID in a clash never refers to a later group. Transport identity and submission status stay inside the client rather than leaking into the bot API. `client.generation`, `start()`, and `refresh()` expose the current `Generation` snapshot.

`Castle` and `SoldierGroup` provide `distance(other)` for straight-line distance in map units and `compare_team(other)` returning `TeamType.friendly`, `TeamType.neutral`, or `TeamType.enemy` (import `TeamType` from `island_battles_client`). A target with team color `0` is always neutral, including when both entities are neutral. Otherwise, equal team colors are friendly and different colors are enemies. These utilities are the only model code written by hand; they live in `_entities.py`, which the generated classes extend.

`SoldierGroup.destination_castle` is the castle with `destination_castle_id` in the same generation, linked when the generation is validated; a generation whose group points at a missing castle is rejected. `distance_left()` takes no arguments and measures to that castle. Distances are floating-point map units, not travel times or generation counts; speed and clash radius do not affect them.

## The client

`IslandBattlesClient` owns authentication, the personal player-channel subscription, reconnect recovery, state reconciliation, and turn completion. It calls `play` serially, once per generation, with a `BotTurn` built from the authorized state (`memory` is the state's `memory`, `turn_deadline_at` its deadline, and `upgrades` the table the client reads from `GET /game/upgrades` once and reuses). When `play` returns, the client sends the result as the turn's **one completing batch**: the commands, the game log lines and the memory, with `complete: true`. An exception, a result that isn't valid, memory over 64 KiB, or a server rejection leaves the turn open and stops the client; `run()` and `wait()` raise the error. A bot that raised, or whose result the server rejected, first sends its error and its remaining log lines in a last batch. In a real-time game, a result that arrives after the turn's end, which the server refuses with `StaleGenerationError`, is a passed turn instead: the client writes that the bot was slower than the turn pace, sends nothing more for that turn and plays the next one.

A rejected result raises a `GameCommandError`: the server's `urn:island-battles:problem:` types `castle-not-found`, `castle-not-owned`, `upgrade-max-level`, `insufficient-soldiers`, `burst-unavailable` and `burst-above-potential` raise `CastleNotFoundError`, `CastleNotOwnedError`, `UpgradeMaxLevelError`, `InsufficientSoldiersError`, `BurstUnavailableError` and `BurstAbovePotentialError`, each with the HTTP `status` and `detail`; `bot-memory-too-large` raises `BotMemoryTooLargeError`. Count carefully: what your earlier batches reserved this turn isn't on your board.

Castles and soldier groups carry `upgrades`, which maps every `UpgradeType` (`UpgradeType.speed`, `UpgradeType.clash_stop_generations`, `UpgradeType.generation_amount`, `UpgradeType.burst_potential`) to its level; a group copies its origin castle's levels when it leaves. `UpgradeType` is a `StrEnum`, so `castle.upgrades["speed"]` works too.

When the game ends, the client receives the final generation with its outcome, from the `game.ended` event or an HTTP read. It does not call `play` for that generation. It calls the optional `on_game_ended(outcome, generation)` (sync or async) once with the `GameOutcome` (`reason`, `winner_team_color`, which is `None` for a draw, and a `summary` sentence) and the final board. Then, when the client has the bot's optional `game_ended` function (`IslandBattlesClient(..., play=play, game_ended=game_ended)`, which `run` passes when the module has one), it reads the player's `GameEndedDelivery` from `GET /game/from-lobby/{lobby_id}/game-ended-delivery` with the player token and calls `game_ended` with it ([When the game ends](#when-the-game-ends)); a failure to read it, such as a server without the route, or an exception in `game_ended`, is logged at `warning` and never fails the client. Then `wait()` and `run()` return. `client.outcome` holds the outcome afterwards. A client started on a finished game ends the same way.

Turns have a deadline, which the lobby's host sets: 30 seconds unless the lobby chose another `turn_timeout_seconds`, or none. When a turn's deadline passes before every player completed it, each team with a player who hadn't loses, and the game ends with reason `turn_timeout` and a summary such as `Red lost: Sam's bot didn't answer within 30 s; Blue wins` (see [turn deadlines](../../backend/README.md#turn-deadlines)). The server runs no timers, so once this client has completed a turn and waits for the others, it posts to `/game/from-lobby/{lobby_id}/deadline` a second after the state's `turn_deadline_at`, unless a new generation arrives first; if the turn is still open then, it asks again after 2 seconds, doubling up to 30. The answer ends the game as above, and `on_game_ended` runs. A slow `play` whose turn passes its deadline gets `GameEndedError`, the server's `urn:island-battles:problem:game-ended`, when its result is sent; the client handles it, leaves the turn and calls `on_game_ended` instead of failing. The client's `clock` argument, which returns an aware `datetime`, is the clock it compares deadlines with.

The client creates the request UUID internally and reuses the exact request when retrying a transport failure. Authentication, request IDs, and completion belong to the client. It also deduplicates repeated events and reconciles authorized HTTP state after subscription or reconnect. No periodic polling is enabled by default; `refresh()` can request an explicit read.

The full HTTP contract, authorization rules, reservation checks, retry behavior and status codes are documented in the [backend README](../../backend/README.md), the bot contract in its [bot turns](../../backend/README.md#bot-turns). The browser's **New demo lobby** uses its own JavaScript player strategy. See the [demo walkthrough](../../backend/README.md#browser-demo).

## Logging

`island_battles_client.logger` is a standard `logging.Logger` named `island_battles.player`. Log with it as with any logger, or return lines with `log_line` in the result:

```python
from island_battles_client import logger

def play(turn):
    logger.info("turn %d: %d castles", turn.turn_number, len(turn.generation.castles))
    ...
```

Helper modules can use child loggers, such as `logging.getLogger("island_battles.player.planner")`, whose records propagate to it. From `start()` to `close()` the client attaches three handlers to the `island_battles` logger, the parent of the bot's logger and of the SDK's own `island_battles.sdk`:

- **stdout**, unless `log_to_stdout=False`;
- **a file**, when `log_file` is set; lines are appended;
- **the game log**, always. The client has no option to turn it off.

```python
client = IslandBattlesClient(
    credential, lobby_id,
    play=play,
    game_url=game_url,
    lobby_url=lobby_url,
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
