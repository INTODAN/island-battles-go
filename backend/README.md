# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and game state, and no messages waiting to be sent. Centrifugo handles WebSocket transport and history recovery. A request that commits a lobby change or a new generation pushes it to Centrifugo before it responds, and the backend runs no work between requests. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). Seven of the backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret and internal proxy secret. Startup rejects missing values. Only the HTTP server host reads the HTTP address; the [Lambda host](#hosts-and-deploys) has none. The database pool settings are optional; see [the connection pool](#the-connection-pool). So is `ISLAND_BATTLES_ADMIN_API_KEY`, the [admin API key](#map-files) that `POST /map/create` requires; without it, that endpoint refuses every request. Public clients receive only their guest credential and short-lived connection JWT.

Run `./scripts/dev.sh` from the repository root to load `.env`, start local dependencies with Docker Compose or Podman, migrate the schema and run the backend. If dependencies are already running and the environment is exported, run `go run ./backend/cmd/migrate` and then `go run ./backend`. The backend only connects to the database, so it fails on a database the migrate command hasn't brought up to date. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`lobby/api/`](../lobby/api/README.md).

Open `/lobby/ui/` for the [temporary two-player lobby frontend](../lobby/frontend/README.md); `/` redirects there. It is embedded in the backend and needs no separate frontend build.

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`.

## Browser demo

The backend lists Island Battles in the lobby frontend's game catalog with [`lobbyadapter.FrontendGame`](lobbyadapter/frontend.go), and serves the browser [example player](demo/example-player.mjs) and the create form's [map choice](demo/lobby-settings.mjs) under `/game/demo/`. Once a lobby starts, the frontend shows the lobby replay from `/game/from-lobby/{lobby_id}/replay`, which takes new generations from the page's `lobby.game-state` messages and reads each generation's [game logs](#game-logs) and the [demo](#demos) it saves through the page's `game.request` messages. The catalog entry also links **Watch a saved demo**, the [demo viewer](#demos), and **Play in 3D**, the web client that the deployment serves at `/play/` ([deploy README](../deploy/README.md#the-web-client)), on the lobby page.

For the quickest test, open `/lobby/ui/` and click **New demo lobby**. It creates Alex on Blue in the current tab, opens Sam on Red in a second tab, marks both ready, and starts automatically. Both tabs run the example player: send one soldier from the first available owned castle to the first enemy castle, then finish the turn. Players submit immediately when a generation arrives, without an artificial client delay. Replay playback has its own viewing pace. Failed requests still use retry backoff.

The board follows new generations live. **Pause this player** stops that tab from submitting its next turn; the other player then waits at the turn barrier, until the [turn deadline](#turn-deadlines) makes the paused player's team lose. **Resume this player** continues if it's in time. The timeline lets you review previous generations; **Follow live** returns to the newest state. Keep both tabs open. A matching, fully typed Python implementation is in [`examples/player.py`](../clients/island-battles-python/examples/player.py).

Manually created or joined lobbies wait for commands through the [Island Battles Python client](../clients/island-battles-python/README.md); only demo invitations let the browser example player submit turns. Disconnected players remain part of the turn barrier. In every lobby, the example player [applies the turn's deadline](#turn-deadlines) while it waits: after completing its turn, while paused, and when it only follows the game.

Run the browser player's strategy, deduplication, retry, deadline and no-polling checks from the repository root with `node backend/demo/example-player.test.mjs`, and the map choice's and turn timeout's checks with `node backend/demo/lobby-settings.test.mjs`.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. Registered example games are `island-battles` and `quiz-demo`. A create request may carry a `game_settings` object, which the game checks at creation and receives, compacted with sorted keys, in the started snapshot. Settings never change after creation. Island Battles reads them as its [map choice](#choosing-a-lobbys-map) and [turn timeout](#turn-deadlines); `quiz-demo` has no game behind it and accepts any object. Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. There is no background sweeper. Every lobby request, including the heartbeat, and every Centrifugo subscription refresh applies due deadlines first and pushes the result. Connected members therefore see a peer's expiry within one heartbeat or refresh, and a lobby nobody is connected to catches up on its next access. Deadlines are persisted, so they also apply after a restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

The host is the earliest active joiner. Team changes clear readiness. At least two members are required to start; all active members must choose a team, become ready and have a live lease. Starting freezes the roster; only roster players can subsequently rejoin that started room. An empty waiting room closes. Expired guest credentials require an explicit new session and do not transfer ownership.

## Interactive Island Battles generations

Starting an `island-battles` lobby initializes generation one. Each occupied team starts with a castle and 30 soldiers. The frozen roster defines who takes part in each generation: every roster player can submit commands and then complete their turn, and the game advances exactly once after all roster players complete. A player disconnecting does not remove them from this barrier; their teammates wait until that player reconnects and completes the turn, or until the turn's [deadline](#turn-deadlines) passes, which makes their team lose.

Players read their authorized current state with `GET /game/from-lobby/{lobby_id}/generation`, then register commands with `POST /generation/commands`. Both requests require the guest bearer credential of a player on the game's frozen roster. Identity and team color come from that credential and the frozen roster, so clients cannot choose another player or team. The game reads its lobby only once, when it binds at start; afterwards it authorizes against its own roster and finds itself by the lobby ID it stored, so players keep their access after they leave the lobby or it is deleted. Example request:

```json
{
  "game_id": 1,
  "generation_id": 1,
  "request_id": "f8b7e4e2-8e32-4a13-9370-4d5fa740e21b",
  "complete": false,
  "commands": [
    {
      "command_type": "send_soldier_group",
      "parameters": {
        "origin_castle_id": 0,
        "destination_castle_id": 1,
        "soldier_amount": 5
      }
    },
    {
      "command_type": "upgrade_castle",
      "parameters": {"castle_id": 0, "upgrade_type": "speed"}
    }
  ]
}
```

Players have two commands: `send_soldier_group` sends soldiers from a castle their team owns, and `upgrade_castle` buys the next level of one upgrade type for such a castle, as [Castle upgrades](#castle-upgrades) describes. Other command types are forbidden. Commands may be sent in several batches with `complete: false`; `complete: true` seals the turn. Omitting `complete` defaults to true for a one-batch turn. An empty command list is valid, so send `{"commands":[],"complete":true}` (with the other required fields) to pass. Once complete, that player's turn is sealed for the generation. The server checks each command against the generation and every command already accepted this turn, the player's own and their teammates': it tracks, per castle, the soldiers not yet reserved and each upgrade type's pending level. The turn deducts the same totals whatever order the players' commands run in, so an accepted command cannot fail when the turn runs. The server rejects a whole invalid batch without storing any part of it or completing the turn.

Each request UUID identifies one immutable batch for one player and generation. Retrying the exact request is safe, including after a timeout or generation advance; reusing that UUID with changed content conflicts. New requests for an old generation and new commands after turn completion also conflict. A game ends after the turn that leaves one team with castles or soldier groups (neutral castles need not be captured), as a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins and equal counts draw. Turns are not paced, so the cap keeps one game of fast bots from loading the server. A game also ends when a turn passes its [deadline](#turn-deadlines) before every roster player completed it. The game is then over: its final generation stays readable, `GenerationState.outcome` says how it ended, and every new command conflicts with problem type `urn:island-battles:problem:game-ended`. Errors use Huma problem JSON: 401 for missing/invalid credentials, 404 for an inaccessible game, 403 for forbidden commands or ownership, 409 for stale/sealed turns, UUID conflicts, insufficient unreserved soldiers, or an upgrade past its maximum level, and 422 for invalid parameters. Command rejections that clients tell apart carry a problem `type`, shared by both commands:

| Rejection | Status | Problem type |
|---|---|---|
| Parameters missing, of the wrong type, or with extra keys | 422 | `about:blank` |
| `upgrade_type` is not a known type | 422 | `urn:island-battles:problem:unknown-upgrade-type` |
| A castle isn't in this generation | 422 | `urn:island-battles:problem:castle-not-found` |
| The castle belongs to another team or is neutral | 403 | `urn:island-battles:problem:castle-not-owned` |
| The type's pending level is already its maximum | 409 | `urn:island-battles:problem:upgrade-max-level` |
| Soldiers or an upgrade cost beyond the castle's unreserved soldiers | 409 | `urn:island-battles:problem:insufficient-soldiers` |
| The batch's [game log](#game-logs) lines would pass 100 for the player's generation, or one has more than 500 characters | 422 | `urn:island-battles:problem:game-log-limit` |
| The batch's [bot memory](#bot-turns) is larger than 64 KiB as compact JSON | 422 | `urn:island-battles:problem:bot-memory-too-large` |

Each `detail` names the castle and the numbers, such as `castle 3 has 12 unreserved soldiers; speed level 3 costs 20`. Read it before retrying with a new request UUID.

The response is the caller's current `GenerationState`. The final player's completion returns the new generation. The request that completes the turn also pushes a `game.next_generation` event to every roster player's authenticated personal `player:<player_uuid>` channel. The turn that ends the game pushes `game.ended` instead, with the final generation and its outcome; it is the game's last event. Centrifugo keeps a short history on that channel for reconnecting clients. A client can see the same generation both in an event and over HTTP, so clients deduplicate by game and generation. The [Island Battles Python client](../clients/island-battles-python/README.md) builds a strategy through `player_factory(team_color)` after its first authorized state read. Strategies subclass the abstract `Player` base class and receive typed generation snapshots and a callback-scoped command receiver; a successful callback return automatically completes the turn. Its public snapshots omit transport and submission metadata, and its receiver sends one soldier group or buys one upgrade level per call, for the callback's generation. The client reconciles authorized HTTP state on subscription/reconnect. The browser demo uses the same wire events. Neither game client polls by default. The generic lobby protocol is unchanged.

`GET /game/from-lobby/{lobby_id}/replay` shows the players on the frozen roster the recorded generation history. The replay is not stored: each request loads the game's generations in ID order, in a fixed number of batched queries, and turns each into a frame. Completing a turn writes no replay data. Each frame is the same `Generation` board view that players receive in `GenerationState.generation`, with castles and soldier groups sorted by ID, so the page appends live generations unchanged. The page writes each castle's upgrade levels under its name, such as `Speed 2 · Stop 1`. Only the replay's static metadata lives on the game: its teams with their lobby names and players' display names, copied at start because the lobby is deleted later, and its outcome, which stays empty while the game runs and is saved by the turn that ends the game. The page data carries the outcome as players see it, the `GameOutcome` with its summary, or null while the game runs. The page shows the outcome, also when it arrives with a live state. Its controls play and pause, step one generation back or forward, jump to the latest generation, follow live generations and scrub with a slider, next to the generation number. **Save demo** saves the game as a [demo file](#demos).

Clashes are saved atomically with each generation. Generation snapshots, live notifications and replay frames always include a `clashes` array, empty for turns without combat. Each event has a generation-local `id`, a `time` fraction within the turn, and participant records with `group_id`, `team_color`, contact `x`/`y`, `clash_radius`, `soldiers_before`, and `soldiers_after`. Counts describe each individual contact, including multiple clashes by the same group in one turn. The stored and published form is the same `models.Clash`.

The lobby battlefield pulses an impact ring, shows loss numbers only above surviving troops, and leaves a dashed outline with × for defeated groups. Defeated markers stay at their recorded contact positions, including when they overlap other troops or castles. Losses from multiple clashes are combined above each surviving troop. Effects remain visible while paused and clear on the next generation; scrubbing restores static effects, and reduced-motion settings disable the pulse. Combat mechanics and soldier totals are unaffected.

Soldier groups have a persisted `ClashRadius`, defaulting to **0.25 world units**, which is copied to subsequent generations. Enemies clash when their center-to-center distance reaches either group's radius (the larger radius, rather than their sum). Contacts resolve in time order using troop counts before casualties. If one group has strictly less than half the other's soldiers, the survivor completes its movement. Otherwise the groups stop at the midpoint between their contact positions for the generation; exactly half also stops. In a simultaneous connected clash, any comparable opposing pair causes a stop at the average participant position. Paths are recalculated after a stop so survivors can still fight later enemies. Dead and empty groups cannot participate in later clashes. Simultaneous connected contacts resolve together; each team loses up to the combined opposing force, with losses assigned in persistent group ID order.

Each group's `clash_stop_generations` upgrade controls its own pause duration: **level 1 → 3 generations**, **level 2 → 2**, and **level 3 → 1**, including the clash generation. Troops copy this level when spawned, so upgrading the castle does not change existing troops. Each group persists its remaining `MovementPauseGenerations`, which is copied across generations and decremented when it stays still. Smaller attacks do not restart or extend an existing pause.

Groups leave field combat when they reach their destination, including arrivals exactly tied with contact. Already-arrived groups cannot clash with newly spawned troops. Short trips retain normal movement speed until arrival. At the end of the turn, castle arrivals resolve in persistent group ID order against the updated castle state, preserving earlier damage, captures, and reinforcements. Sending zero soldiers or sending to the same castle is rejected.

The replay draws troop circles using their stored radius and castle circles with a visual-only radius of **0.5 units**, with the same world scale on both axes.

The Island Battles adapter wraps the generic start operation. Generic lobby start freezes the roster first; game initialization uses that roster and persists generation one and its player/team bindings. Concurrent or repeated starts reuse the initialized game. Other game keys are unaffected.

### Turn deadlines

The host sets how long each turn lasts when creating the lobby, with `game_settings.turn_timeout_seconds`: a whole number of seconds from 5 to 600, or `null` for no limit, which helps while debugging a bot at a breakpoint. Settings without it get **30 seconds**. The lobby page's create form offers 10 s, 30 s, 2 min, 10 min and No limit. The game copies the timeout at the start, to `games.turn_timeout_seconds`, which is null for no limit; AutoMigrate adds the column, so games started before it have no limit.

- **The deadline.** Each generation's turn times out at the generation's creation time plus the timeout. `GenerationState.turn_deadline_at` and `BotTurn.turn_deadline_at` carry it, also in pushed events. They are `null` without a limit and once the game has ended.
- **One missed turn loses.** When the deadline passes before every roster player completed the turn, every team still in the game with a player who hadn't has timed out and loses, and the game ends at once. A team is still in the game while it owns a castle or has soldier groups on the field, the same rule as the end rule's; an **eliminated team never times out**. Its players have nothing to play, so the turn doesn't wait for them: the turn plays as soon as every player of a team still in the game completed it, and a deadline that finds only eliminated players late changes nothing. Among the other teams that are still standing, the team with the most castles wins, as at the generation cap, and equal counts are a draw. When several teams time out together, they all lose; when every team still in the game does, the game is a draw. Teammates who completed the turn lose with their team.
- **The outcome** has reason `turn_timeout` and the winner, or `null` for a draw. Its `summary` names each team that timed out and its players who didn't answer, and then the winner: `Red lost: Sam's bot didn't answer within 30 s; Blue wins` (an eliminated team is never named), `Blue lost: Alex's and Lee's bots didn't answer within 2 min; Red lost: Sam's bot didn't answer within 2 min; Green wins`, `…; the other teams draw`, or, when every team still in the game timed out, `Draw: Blue's and Red's bots didn't answer within 30 s`. The saved `games.outcome` also lists the teams that timed out and those players' start display names under `timed_out`, and the colours of the teams still in the game that didn't time out under `survivors`. The open generation stays the final one; it is pushed once more to every roster player as `game.ended`, each with their own `submitted`, and every later command conflicts with `urn:island-battles:problem:game-ended`, including a late batch for the expired generation. A retry of a batch that was accepted in time still answers as before.
- **No scheduler.** The backend runs no work between requests, so a deadline applies when a request finds it overdue, as lobby leases expire. Every game request applies it first, under the game lock: the state read, every batch, the replay, game log and demo reads, and `POST /game/from-lobby/{lobby_id}/deadline`. The reads that take no game lock check the deadline with one light query first and lock the game only once it has passed. A batch that arrives after the deadline ends the game itself and is rejected, while the end still commits. Applying a deadline again changes nothing, so the game ends once and `game.ended` is pushed once, also when several requests race.
- **`POST /game/from-lobby/{lobby_id}/deadline`** applies the deadline for a roster player and answers their `GenerationState`, as the state read does. Before the deadline, without a limit, or after the end, it changes nothing. A client that waits for other players calls it once, about a second after `turn_deadline_at`, so a game nobody answers still ends. The [Python client](../clients/island-battles-python/README.md) does this after completing its turn, and the lobby page's example player does it while it waits; the web client will too ([#101](https://github.com/INTODAN/island-battles-go/issues/101)). If the turn is still open, for example because the server's clock is behind the client's, they ask again after 2 seconds, doubling up to 30.

### Game logs

A bot can send the lines it logs while playing a generation, its **game log**, with that generation's batches. `POST /generation/commands` takes an optional `logs` array:

```json
"logs": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}]
```

- `level` is `debug`, `info`, `warning` or `error`. `logger` names the logger that wrote the line, in at most 100 characters. `message` has 1 to 500 characters, counted as Unicode code points, and no NUL.
- A batch may carry logs, commands, both, or neither. The server appends a batch's lines, in order, after the player's earlier lines for the generation, under the game lock. The payload hash includes them, so retrying a request stores nothing twice.
- A player has at most **100 lines per generation**. A batch that would take the player past 100 lines, or that has a line longer than 500 characters, is rejected whole with 422 and problem type `urn:island-battles:problem:game-log-limit`; the detail names the limit. Nothing of a rejected batch is stored: not its lines, its commands, or its completion. Other invalid lines, such as an unknown level, are a plain 422. The [Python client](../clients/island-battles-python/README.md#logging) cuts its lines and counts them from the state's `log_lines_stored`, so it doesn't send such a batch unless another client of the same player logged meanwhile; then it sends the batch again without its lines.
- Every `GenerationState`, from `GET .../generation` and from batches, has `log_lines_stored`: how many lines the caller has stored for its generation. A client that starts again during a turn counts on from it. Pushed events carry 0, because a new generation has no lines yet.
- Lines are accepted only while the turn is open, as commands are: a new batch after completion, or for a generation that is no longer current, conflicts with 409.

The lines live in the `generation_logs` table, one row per line, with the game, the generation, the player, the line's `sequence` within the player's generation (from 1), its level, logger and message. The unique index on the generation, player and sequence serves the line count under the lock and the reads. The table is game data: AutoMigrate creates it, and [retention](#game-retention) deletes the lines with their generation. Players are named in the logs by `FrozenGamePlayer.DisplayName`, the lobby display name copied at start, as the replay's team players are; a player that the start snapshot doesn't list is named `Player <n>` after their roster position.

`GET /game/from-lobby/{lobby_id}/logs?generation_id=N` reads one generation's game log, with the same authorization as `/generation`: a place on the game's frozen roster. Its response is the `GenerationLogs` schema:

```json
{
  "generation_id": 41,
  "players": [
    {"display_name": "Alex", "team_color": 1, "lines": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}]}
  ],
  "hidden_team_colors": [2]
}
```

- While the game runs, the caller sees only their own team's lines. Once the game has an outcome, every roster player sees every player's lines.
- `players` lists every player of the visible teams, also those without lines, ordered by team colour and then display name. Each player's `lines` keep the order they were stored in.
- `hidden_team_colors` lists, in ascending order, the teams whose lines the caller can't see yet, so a page can explain why. It doesn't reveal whether those teams logged anything. It is empty once the game has ended.
- The response contains no player IDs.
- `generation_id` must be a generation of this game; any other is 404, as are callers off the roster. Like the other game reads, the request ends with a game [retention](#game-retention) pass.

A [demo](#demos) holds one `GenerationLogs` per generation, with the visibility rule that applied when it was saved.

The replay shows the game log in a **Logs** panel below the board, for the generation on the board. The replay runs in the lobby page's sandboxed frame without the credential, so it reads the logs through the page: Island Battles' catalog entry sets the lobby frontend's `DataURLPrefix` to `/game/from-lobby/{lobby_id}/`, and the replay posts `game.request` messages for `/game/from-lobby/{lobby_id}/logs?generation_id=N`, which the page fetches for it (see the [lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)). The replay page data carries the `lobby_id` for that path. The panel groups the lines by team and then by player, in monospace, coloured by level, and scrolls inside a fixed height, so the frame's reported size doesn't change as lines load. It caches the lines of generations whose turn is over. For the open turn, the latest generation of a running game, it says that lines may still arrive and, while that generation is shown in a visible tab, reads its lines again every 3 seconds, at most one read at a time; a generation that was open when shown is read once more when the next one arrives, for its final lines. It explains hidden teams ("Red’s logs appear when the game ends") and generations without lines, and loads every generation again when the game's outcome arrives. Opened outside the lobby page, it says the logs need the lobby.

### Bot turns

A bot is a **stateless function**. Each turn it gets a `BotTurn` and answers with a `BotTurnResult`; both are schemas in the [player API document](#player-api-document), so every client generates them:

```json
{"game_id": 7, "generation": {"id": 41, "castles": [], "soldier_groups": [], "clashes": []}, "team_color": 1,
 "memory": {"target": 3}, "turn_deadline_at": "2026-11-07T12:00:30Z"}
```

```json
{"commands": [{"command_type": "send_soldier_group", "parameters": {"origin_castle_id": 0, "destination_castle_id": 3, "soldier_amount": 12}}],
 "logs": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}],
 "memory": {"target": 3, "sent": 12}}
```

- `generation` is the generation of `GenerationState`, and `team_color` is the bot's team. There are no previous generations: a generation is 10 to 25 KB of JSON, so a whole game would be megabytes per turn. A bot that needs history keeps what it needs in its memory.
- `memory` is the memory the bot returned last turn: any JSON object, or `null` on its first turn. The bot returns its new memory, or `null` for none.
- `turn_deadline_at` is when the turn times out, `GenerationState.turn_deadline_at`, or `null` when the lobby set no limit. A bot that hasn't answered by then makes its whole team lose; see [turn deadlines](#turn-deadlines).
- `commands` and `logs` have the shapes of `POST /generation/commands`, and the same rules apply.

Whoever runs the bot, a client relaying it or the backend itself, sends the result as the turn's completing batch: `POST /generation/commands` with its `commands`, `logs` and `memory`, and `complete: true`. The **server stores the memory**, so every way of running a bot behaves the same, and a bot keeps nothing between turns itself:

- Only the batch with `complete: true` saves `memory`, under the game lock, replacing the player's previous memory. An absent or `null` memory clears it. Batches with `complete: false` may carry memory, but it isn't saved. A client that sends a turn in several batches puts the memory in the last one.
- Memory is at most **64 KiB** (65,536 bytes) as compact JSON; whitespace around values doesn't count. Larger memory is rejected whole with 422 and problem type `urn:island-battles:problem:bot-memory-too-large`, and nothing of the batch is stored: not its commands, its lines, its memory or its completion. A memory that isn't a JSON object or `null` is a plain 422.
- The payload hash includes the memory, so a retry stores it once, and a late retry of an earlier turn doesn't bring that turn's memory back. Absent and `null` memory hash alike.
- Every `GenerationState` has `memory`: the memory sent with the caller's latest completed turn of the game, or `null`. It's the next `BotTurn.memory`. Events pushed after a turn carry each player's own memory, on their personal channel only.
- Memory is kept as sent: numbers keep every digit. PostgreSQL may reorder an object's keys.

The memory lives in the `game_player_bot_memory` table, one row per player who has memory, keyed by the game and the player, with the generation whose turn saved it. The memory column is `bytea`, the exact compact JSON the service validated, because `jsonb` would reorder keys, expand numbers like `1e131071` into every digit and reject `\u0000`. A turn that clears the memory deletes the row. The table is game data: AutoMigrate creates it, and [retention](#game-retention) deletes it with the game.

Bots that the backend plays itself implement the Go interface `bots.Bot` in [`bots/`](bots/bots.go), `Play(ctx, BotTurn) (BotTurnResult, error)`, on the Go types `models.BotTurn` and `models.BotTurnResult` that the schemas are exported from.

### Default bots

The **default bots** are numbered levels that the backend plays itself, in Go, so they work with any client and with every tab closed. `GET /bots/defaults` lists them in level order, each with its `level`, `name` and `description`; it needs no authentication.

| Level | Plays |
|---|---|
| 1 | The example player's strategy: each turn one soldier from its first castle with soldiers to the first enemy castle, in ID order. It never attacks neutral castles. |
| 2 | Each of its castles attacks the nearest castle, enemy or neutral, that it can take: it forecasts the defenders when its group lands, as the turn rules play it (every castle grows by one soldier a turn, then the groups heading there land in ID order), and sends one more soldier plus a margin of 2 and a tenth. Otherwise it waits and grows. It doesn't defend. |
| 3 | Level 2's forecasts, and it also keeps enough soldiers in each castle to hold against the groups heading for it and as a guard against the strongest enemy castle near it; sends help to a castle that would fall even keeping every soldier; combines attacks from several castles, the farthest sending first and the nearer ones later, so the groups land together; counts the enemy groups that its attack meets on the way; and buys speed levels 2 and 3 with soldiers it can spare from turn 15. Its memory keeps a turn counter and the launches its combined attacks have yet to send. |

The bots are deterministic: the same `BotTurn` always gives the same result, and they use no randomness. Each writes one or two game log lines a turn that explain its choice, with the logger `island_battles.bots.level1` to `level3`.

**Which players they play.** The game's frozen roster (`frozen_game_players`) has a nullable `bot_level` column: the level the backend plays for that player, or null for a player whose client plays. AutoMigrate adds it. Nothing sets it yet: a player's bot choice ([#111](https://github.com/INTODAN/island-battles-go/issues/111)) and bot seats ([#115](https://github.com/INTODAN/island-battles-go/issues/115)) will set it when the game starts.

**How they play.** In the transaction that creates a generation, at the start or when a turn plays, the backend gives each Go bot of a team still in the game its `BotTurn`, with the memory it returned last turn, checks the result with the rules of a completing `POST /generation/commands` batch, and stores its commands, game log lines and memory and completes its turn, before the generation is pushed. So a Go bot never times out, and a game of one human and one Go bot plays each turn as soon as the human completes it. Teammates' Go bots play one after another, in player ID order, and each bot's `BotTurn.generation` shows its team's castles as the commands already accepted this turn leave them: the soldiers not yet reserved by its teammates' commands (and any its own player stored before the backend took over), and the upgrade levels with those the commands bought. The batch check counts the same reservations, so two bots on one team never send the same soldiers twice. Other teams' castles show what every player sees. A Go bot's game log lines follow any its player stored for the generation; its first lines are stored up to the [limit](#game-logs) of 100 a generation, and the rest are left out, rather than the turn being rejected. A Go bot that returns an error, panics or answers with something the rules reject passes the turn: it sends no commands, keeps its memory, and its game log gets one `error` line from the logger `island_battles.bots`.

**Games that only Go bots play.** While every player still in the game is a Go bot, the turn is complete as soon as it is created. The request that got there keeps playing turns, each in its own transaction under the game lock and pushed as it commits, until the game ends or its time budget has passed, and less when the request's own deadline, such as the Lambda's, is nearer. Every game request first has the Go bots catch up the same way, so the next one continues where the last stopped, and its deadline never ends a turn that the Go bots completed. The budget depends on the request:

| Requests | Budget |
|---|---|
| The game start, every `POST /generation/commands` batch, and the deadline call | about **5 seconds** |
| The reads: `GET` state, the replay, the game log and the demo | about **1 second**, so reading a game that only Go bots play stays quick |

Every request plays at least one turn, however small its budget, so a game always makes progress. A game without Go bots pays one read of its roster for this. Locally a game plays about 60 turns a second.

### Demos

A roster player can save the game as a **demo file** at any time, and watch it later in the browser, after the lobby, the guest session and the game itself are gone.

`GET /game/from-lobby/{lobby_id}/demo` returns the demo, with the same authorization as `/generation`: a place on the game's frozen roster. Its body is the `Demo` schema in the [player API document](#player-api-document), sent as an attachment named `island-battles-game-<game_id>-generation-<n>.json`, where `n` is the number of generations played:

```json
{
  "format": "island-battles-demo",
  "schema_version": 1,
  "saved_at": "2026-10-31T12:00:00Z",
  "game_id": 12,
  "teams": [{"color": 1, "name": "Blue", "players": ["Alex"]}],
  "outcome": {"reason": "last_team_standing", "winner_team_color": 1, "summary": "Blue wins as the last team standing"},
  "frames": [],
  "logs": []
}
```

- `frames` holds every generation played when the demo was saved, in order, each the same `Generation` board view that players and the replay receive.
- `logs` holds one entry per frame, in the same order, each a `GenerationLogs` as the [game log read](#game-logs) returns it. Its lines follow that read's visibility rule when the demo is saved: a demo saved during the game holds only the saver's team's lines, and one saved after the end holds every team's. `hidden_team_colors` names the teams left out.
- `outcome` is null when the demo was saved while the game was running.
- `schema_version` is `models.DemoSchemaVersion`. Raise it whenever the format, or a type it holds such as `Generation`, changes in a way the viewer can't read. Following the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md), the viewer opens only the current version and keeps no readers for older ones.

The request loads the generations with the replay's batched loader and the visible lines in one query, by generation ID and player, so the number of queries doesn't grow with the game. It takes no game lock, like the replay: the game is read first, so a game that ends meanwhile is saved by the stricter rule. It streams the file a frame and a generation's logs at a time, without escaping `<`, `>` and `&`. Like the other game reads, the request ends with a game [retention](#game-retention) pass.

A long game's demo is large, and the deployed backend can't send a large response quickly. The [Lambda host](#hosts-and-deploys) streams a response's first 6 MB at full speed and the rest at about 2 MB/s, and a request has 30 seconds by default, less the backend's 2-second margin. So when the request's `Accept-Encoding` allows gzip, as every browser's does, the demo is packed with gzip as it is written (`Content-Encoding: gzip`, with `Vary: Accept-Encoding`), never whole in memory. Browsers unpack it as they read it. CloudFront forwards the browser's `Accept-Encoding` to the Lambda, because its origin request policy forwards every viewer header, and passes `Content-Encoding` back unchanged; it compresses nothing itself. Bots' lines repeat their wording, so they pack well:

| Two players at the generation cap, both logging 100 lines of 500 characters every turn | Unpacked | Packed |
|---|---|---|
| Bot-like lines, with numbers that vary | 35 MiB | 4.4 MiB |
| Random characters, the worst case for packing | 35 MiB | 22 MiB |

A packed demo of bot-like lines fits in the first 6 MB. Even random lines from three players at the limit, about 52 MiB unpacked, stream in under 20 seconds. The lobby page reads at most 64 MiB, counted unpacked ([lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)), so four or more players all logging at the limit every turn can pass it, and then saving reports that the demo is too large.

The replay's **Save demo** button asks the lobby page for the demo with a `game.request` message and the relay's longest wait, 120 seconds, because a large demo can take longer than the default 8 seconds to arrive. The frame has no credential, so the page fetches it ([lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)). The button counts the seconds while it waits, and then the replay saves the answer with a `Blob` download under the same file name; the result frame's sandbox allows downloads. A failure is reported next to the button. Opened outside the lobby page, the replay has no **Save demo** button.

The demo viewer at `/game/demo-viewer/` is a public page from the [`demoviewer`](demoviewer/) package, embedded in the backend like the map builder. It is the replay's own template rendered without a game, so both draw a game with the same code: teams, board, castle upgrade levels, clashes, outcome and the **Logs** panel. **Open demo file** reads the file in the browser and uploads nothing. The page checks every value it draws before showing anything, writes all of the file's text with `textContent` or SVG attributes, and names the first wrong value of a damaged file, such as `frames[3].castles[0].x must be a number`. Positions must lie within a million units of the origin, so the board has a size to draw, and every player in the logs must belong to one of the file's teams. It refuses a file that isn't a demo, and a demo of another `schema_version`, saying that it comes from an earlier or later version of Island Battles. A demo starts at its first generation; play, step, jump to the last generation or scrub through it. The page says when a demo was saved before the game ended. The lobby page links the viewer as **Watch a saved demo**, through the catalog's `Links`.

Run the viewer's file checks from the repository root with `node backend/demoviewer/demo-file.test.mjs`.

### Retry or explicitly bind a started lobby

`POST /game/from-lobby` accepts the guest bearer credential and body:

```json
{"game_key":"island-battles","lobby_id":"<started-lobby-uuid>"}
```

The active host receives `game_id`, `lobby_id` and a stable team UUID/order/color mapping for the initialized game. Repeated requests return the same game after reauthorizing the caller.

`lobbyadapter` selects the Island Battles game key, connects game persistence to the shared binding service, and registers the game endpoint. It uses types directly from `models`, without aliases. Authorization and HTTP handling are delegated to the [shared game-binding helpers](../lobby/README.md#binding-a-game-to-a-started-lobby). Island Battles persistence lives in `dal/lobby_binding.go`; its only translation is from generic team UUID/order to simulation colors 1..N, with `models.NeutralTeamColor` (0) reserved for neutral. Clients expose the same value as `NEUTRAL_TEAM_COLOR`.

## Turn simulation

`services.MigrateGeneration` plays a turn in three steps. `dal.LoadGameState` locks the game and reads its latest generation once. `simulation.Simulate` applies the turn's commands, then the end-of-turn steps (movement and clashes, castle growth, arrivals), to the in-memory `simulation.GameState` and returns the clashes as events. `dal.SaveGeneration` stores the result as the next generation with one batched insert per table. The game rules live only in `backend/simulation`, which never touches the database, so its tests run without Postgres.

## Castle upgrades

Castles and soldier groups persist an `upgrades` map, such as `{"speed":1,"clash_stop_generations":1}`. Each upgrade type defines its own level effects, fixed soldier costs, and maximum in `models/upgrades.go`, the only copy of the table. Speed currently has four levels: **1 → 0.5**, **2 → 1**, **3 → 1.5**, and **4 → 2** world units per turn. The `clash_stop_generations` type has three levels, reducing stoppage from **3** to **2** to **1** generations. Purchasing its levels **2** and **3** costs **10** and **20 soldiers**. Omitted upgrades default to level 1, and every created castle and group stores all of its levels. Map templates can provide initial castle levels. Purchasing speed levels **2**, **3**, and **4** costs **10**, **20**, and **30 soldiers**, respectively; the default level 1 has no purchase cost.

`GET /game/upgrades` serves the table without authentication: every type, sorted by type, with a description and, for each level, its effect (`value`) and the soldiers it costs to reach (`soldier_cost`). The map builder's `upgrades.json` is the same table. Every castle and soldier group in a player's state, its events and the replay carries `upgrades` with every type's level; a group copies its origin castle's levels when it leaves.

Players buy levels with `upgrade_castle` in `POST /generation/commands`:

```json
{"command_type": "upgrade_castle", "parameters": {"castle_id": 0, "upgrade_type": "speed"}}
```

Each command buys the type's next level for a castle the player's team owns and pays that level's cost from the castle's soldiers. A castle may be upgraded several times in one turn, also of the same type: speed 1 → 2 → 3 in one turn costs 10 + 20 soldiers. Registration counts every upgrade and send already accepted this turn, by the player and their teammates, and rejects an upgrade past the maximum level or beyond the castle's unreserved soldiers with the problem types in [Interactive Island Battles generations](#interactive-island-battles-generations). Spending the exact balance is allowed.

Commands execute in the supplied order before movement. Sending troops after upgrading copies the new levels; troops sent before upgrading retain the old levels. Every group keeps its own snapshot throughout later turns. The purchase atomically deducts the target level's cost and advances the upgrade; an exact balance leaves zero soldiers before normal turn production. The engine still checks each command when it runs, and returns typed model errors for insufficient soldiers, maximum levels, unknown upgrade types and invalid levels; a rejected command rolls back the whole turn, which registration prevents for player commands.

## Map files

A map file is the JSON that `POST /map/create` stores as a default map, published as the `MapTemplate` schema in [`api/openapi.json`](api/openapi.json):

```json
{
  "map_name": "crossing",
  "player_amount": 2,
  "plane_size_x": 8,
  "plane_size_y": 6,
  "neutral_starting_soldier_amount": 10,
  "castles": [
    {"team_color": 1, "x": -3, "y": 0, "starting_soldier_amount": 30, "upgrades": {"speed": 2}},
    {"team_color": 2, "x": 3, "y": 0, "starting_soldier_amount": 30},
    {"team_color": 0, "x": 0, "y": 2.5}
  ]
}
```

- The plane is centred on the origin, so a castle's `x` lies from `-plane_size_x/2` to `plane_size_x/2` and its `y` from `-plane_size_y/2` to `plane_size_y/2`. The replay and the game graph draw it the same way.
- `team_color` runs from 1 to `player_amount`, and 0 is neutral.
- A castle without `starting_soldier_amount` starts with `neutral_starting_soldier_amount`. Omitted upgrades start at level 1.
- A castle's position in `castles` is its castle ID in every game built from the map.

Only admins create default maps. Send the deployment's admin API key, `ISLAND_BATTLES_ADMIN_API_KEY`, in the `X-Admin-API-Key` header:

```sh
curl -X POST http://localhost:8888/map/create \
  -H "X-Admin-API-Key: $ISLAND_BATTLES_ADMIN_API_KEY" -H 'Content-Type: application/json' \
  --data @crossing.json
```

The key is compared in constant time, before the body is read. A missing or wrong key returns 401. When the backend has no key set, the endpoint is disabled: every request returns 403, and startup logs a warning. `scripts/dev.sh` reads the key from `.env`; an `.env` copied from an older `.env.example` lacks it, so add the line from `.env.example`.

`models/map_template.go` holds the format and the functions every map source uses: `models.ValidateMapTemplate` checks a map without storing it and returns a `*models.MapTemplateError` that lists each invalid field, such as `castles[2].x`; `models.BuildMap` builds the game map. Read map files with `models.DecodeMapTemplate`, not plain `encoding/json`: it rejects unknown keys and missing required fields, which `encoding/json` ignores or reads as 0, then validates the map. `/map/create` answers an invalid map with 422 and one error per field, stores nothing, and answers a `map_name` that another map uses with 409. Stored templates keep working: games built from them are not validated again, and castles stored before `starting_soldier_amount` existed start with `neutral_starting_soldier_amount`.

`POST /map/validate` checks a map file with `models.DecodeMapTemplate` and stores nothing, so it is safe to leave public. Any non-empty body answers 200 with `{"valid": ..., "errors": [...]}`, each error located relative to the file, such as `castles[2].x`. A `*models.MapTemplateError` lists at most 100 problems and counts the rest in a last detail, and it echoes only scalar values, never a whole object, so a large invalid file gets a small answer. A `map_name` that a default map already uses is not a problem here.

### Map builder

Open `/map/builder/` to build a map in the browser, from the [`mapbuilder`](mapbuilder/) package embedded in the backend. Set the map's name, players, default soldiers and plane size; add castles with **Add castle** or a double-click on the plane, drag them or move them with the arrow keys, and remove them with **Delete**. Each castle has a team colour or neutral, an optional starting soldier count and its upgrade levels, which the page reads from `upgrades.json` next to it, the same table as `GET /game/upgrades`, so they follow `models/upgrades.go`.

The page holds no copy of the map rules. Each edit sends the file to `POST /map/validate`, and the page shows each problem next to the castle and field it names. **Download map** saves exactly the text that the server last accepted; while the map is invalid or unchecked, the button is disabled and the page says why. **Open map file** loads a file to keep editing; a downloaded file reopens unchanged. A hint names any team from 1 to `player_amount` that owns no castle, because lobbies are expected to need one for each team, but it doesn't block the download.

When the lobby's create form opens the page with its **Build a map…** button, the page also shows **Use in lobby** next to **Download map**. It needs what Download needs, and a castle for every team from 1 to `player_amount`, because lobby creation rejects any other map; while it is disabled, the page says why. It posts `{"type": "island-battles.map-file", "text": ...}` with exactly the accepted text to the form's window, with this page's origin as the target origin, and says so on the page. The button appears only when a same-origin window opened the page and is still open; when that window closes, the page hides the button and says to download the map instead. A form that reloaded while the builder stayed open names itself to the builder with an `island-battles.lobby-page` message, so the builder sends to the new page and keeps its unsaved map. Opened directly, the page shows no such button.

Run the page logic's checks from the repository root with `node backend/mapbuilder/builder.test.mjs`.

### Choosing a lobby's map

An Island Battles lobby starts on the map its `game_settings` choose, published as the `LobbySettings` schema:

- `{"map_template_id": 4}` picks a default map. `GET /map/templates` lists them with each map's name, `team_count`, plane size and castle count.
- `{"map": {...}}` carries a whole map file, such as one downloaded from the map builder. It must pass `models.DecodeMapTemplate`.
- `{}`, or no settings, starts on the generated map, with one castle per team that has players.

Pass at most one of the two. Either way, the settings may also set `turn_timeout_seconds`; see [turn deadlines](#turn-deadlines). The map sets the lobby's teams and its neutral castles, so a lobby on a map with `player_amount` N has exactly N teams, and the map's castle team colours are exactly 1 to N, with 0 neutral. The lobby's first team plays colour 1, its second colour 2, and so on. `GET /map/templates` leaves out stored maps that fit no lobby: those whose colours skip one, and those made for fewer than 2 or more than 16 teams, the lobby's limits. A bad choice fails lobby creation with `invalid_game_settings`, and the detail names the invalid fields, listing the first 20 and counting the rest, such as `game_settings.map_template_id: no default map has ID 9` or `game_settings.map.castles[2].x: must be inside the plane, from -5 to 5`. Settings are capped at the lobby's 16 KiB, which holds a map file of over a hundred castles.

The lobby frontend's create form offers the same choice through the catalog's settings module, [`demo/lobby-settings.mjs`](demo/lobby-settings.mjs): the generated map, a default map, or a loaded map file. Picking a map fills in one team per colour, named after the colour the board draws (Blue, Red, Green, Purple, Amber, Teal), with one seat each, and the room shows the chosen map's name and the turn limit, such as `Map: Strait · Turn limit: 30 s`. A **Turn timeout** select below the map sets `turn_timeout_seconds`: 10 s, 30 s, which is selected, 2 min, 10 min, or No limit, which sends `null`. The board fits itself to the map's castles, keeping at least the generated map's 8 by 6 units.

The form's **Build a map…** button opens [the map builder](#map-builder) in a window named `island-battles-map-builder`, which keeps a reference to the form, and clicking it again focuses that window, also after the form reloaded, because the form asks the browser for the window by name before it navigates anything. When the builder's **Use in lobby** posts a map, the form selects **Load a map file…** with that map, as if the host had loaded its file, and names the map, its size and its teams. The form accepts the message only when its origin is the form's own origin and its source is the window the form opened; it ignores everything else. A message that holds no usable map shows its error as a bad file does. The latest map or file the host picks wins.

## Schema and recovery

The hosts never change the schema. The migrate command, [`cmd/migrate`](cmd/migrate/main.go), does, once per deploy and before the new build serves; `scripts/dev.sh`, the acceptance and conformance runners and the Go test helpers (`testutils.InitTestDB`) run it, or the `dependencies.MigrateSchema` function behind it, first. It reads only `ISLAND_BATTLES_DATABASE_URL` and the optional pool settings. It first applies two sets of versioned SQL migrations, which keep existing rows: the lobby's (`lobby/postgres/migrations/`, recorded in `lobby_schema_migrations`) and the map templates' (`backend/dal/migrations/`, recorded in `map_template_schema_migrations`). Each set runs in one transaction under a PostgreSQL advisory lock, so concurrent runs of a set wait for each other. A database created before the schema was versioned is recorded at migration 1 without running it, but only if it has every table and column that migration 1 creates; otherwise the command fails and names what is missing. It then AutoMigrates the game tables and game-team bindings, drops the retired `generation_notifications` table, and drops the `games.replay` column, because replays are now derived from generations. The game steps take no lock, so never run two migrate commands against one database at once. Running it again finds nothing to do. Game data is not migrated: when a schema change strands it, reset the game tables (see the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md)). New games use the nullable unique `lobby_id` link. Creating or joining a lobby does not create a game; starting an Island Battles lobby initializes generation one.

Lobby invalidations and generation events are pushed after their transaction commits, through the lobby's `realtime.Publisher`. It sends a request's messages to Centrifugo's HTTP API in one call and retries what failed twice within about a second. If Centrifugo is still unreachable, it logs an error and the request succeeds anyway, because the change is already committed. Nothing is queued for later. Generation events contain the recipient's authorized generation state and go only to that player's personal channel. Clients reconcile authorized snapshots after subscription/reconnect, so a failed push or a restart recovers through HTTP state.

## Game retention

A game is deleted with all its rows 7 days after its latest generation was created, or after the game itself was created if it never got a generation. Finished and abandoned games are treated alike, because an abandoned game never gets an outcome. The replay goes with the game, because it is built from the generations. Map templates (`map_templates`, `castle_templates`) are never deleted: a game's maps are copies of them. The game's [`retention`](retention/retention.go) package and [`dal/game_retention.go`](dal/game_retention.go) own this. They never read lobby tables, and the lobby deletes its own data separately.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_GAME_RETENTION_PERIOD` | `168h` | How long a game is kept after its latest generation, as a positive Go duration. Go durations have no days unit, so 7 days is `168h`. |

There is no timer. The game's own requests run the passes as they end: turns (`POST /generation/commands`), which are what adds game data, and the state, replay, game log and demo reads (`GET /game/from-lobby/{lobby_id}/generation`, `/replay`, `/logs` and `/demo`). When nobody plays, no game data is added, so none needs deleting. The pass runs before the response is sent, so the request waits for it. Each process runs at most one pass at a time, and at most one a minute while passes find no backlog. A pass that fills a batch lets the next request run another, so a backlog drains one batch per request. A turn adds one generation and a pass deletes up to 10, so deleting keeps ahead of playing. A pass has a 5-second timeout, and a failed pass is logged without failing the request. Against a local database a pass takes under a millisecond when there's nothing to delete and 5 to 7 ms for a full batch. On Lambda every statement, and every BEGIN and COMMIT, is a round trip over TLS to the VM: 4 for a pass with nothing to delete, about 20 for a batch from one long game, and about 50 for a pass that finishes 3 small games, the most a pass purges. That is a few to about a hundred milliseconds at a few milliseconds a round trip, at most once a minute per instance while there's no backlog, and on every game request while there is one.

A pass has two steps. Each runs even when the other fails, and the pass logs every failure:

1. **Retire** up to 10 expired games by soft-deleting the game row. Every request finds and locks its game through GORM's default scope, so a retired game disappears from every endpoint at once, and its turns and reads return 404. The step skips games that a request holds locked (`FOR UPDATE SKIP LOCKED`), so it never waits for a turn and never retires a game while a turn on it is in progress. It checks each locked candidate again in a new statement, so a turn that commits just before the lock keeps its game.
2. **Purge** up to 10 generations of at most 3 retired games, lowest game IDs first, with `Unscoped`, so rows are removed for real. Each generation goes with its requests, player inputs, game log lines, commands and soldier groups, then the generation, then its map's castles and the map. A retired game with no generations left then goes with its remaining requests, bot memory, frozen players and team bindings. Every game is purged in its own transaction, so a game whose purge fails is rolled back and retried by later passes while the other games are still deleted. A long game takes several passes, and no transaction deletes more than 10 generations' rows.

The rows a pass deletes belong to games nobody can reach any more, and the indexes on the foreign key columns (`generations.game_id` and `map_id`, `castles.map_id`, `commands.generation_id`, and `soldier_groups.generation_id`, `origin_castle_id` and `destination_castle_id`) keep the deletes and their foreign key checks from scanning other games' rows. Passes on several hosts at once lock different games, so they never wait for each other either.

## Hosts and deploys

[`app.New`](app/app.go) builds the HTTP handler from the configuration and a database connection. Two hosts serve it:

- **The HTTP server,** [`backend/main.go`](main.go), for local development. It listens on `ISLAND_BATTLES_HTTP_ADDR`, shuts down gracefully on SIGINT or SIGTERM and logs in colour through `tint`. It alone passes `app.WithGameGraph()`, so it alone serves `GET /game/{id}/graph`, which reads any game without authentication.
- **The AWS Lambda function,** [`cmd/lambda`](cmd/lambda/main.go), behind a Function URL, which players reach through CloudFront at the game's name ([deploy/README.md](../deploy/README.md)). It is public, so it serves no graph page, and every route it serves that reads game data requires a player on the game's frozen roster. It serves the handler through `lambdaurl`, so the Function URL must use the `RESPONSE_STREAM` invoke mode. Streaming also lets replay pages, which inline JSON that grows with the game, and [demos](#demos) pass Lambda's 6 MB limit on buffered responses; streamed responses may reach 200 MB. It logs warnings and errors only, as JSON lines with the `level` and `timestamp` keys that Lambda's log-level filtering reads, within the limits in [Logs and traces](#logs-and-traces).

Build the Lambda for the `provided.al2023` runtime on arm64. The runtime runs the executable named `bootstrap` at the root of the zip:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o bootstrap ./backend/cmd/lambda
zip -j bootstrap.zip bootstrap
```

A deploy runs `go run ./backend/cmd/migrate` (or the same command built for the deploy machine) against the production database first, then publishes the new code. A cold start runs no DDL, and its first connection gives up after `dependencies.ConnectTimeout`, 5 seconds, well inside Lambda's 10 seconds for init. The Lambda has no VPC and reaches PostgreSQL over the internet, which accepts only TLS connections. `sslmode=require` encrypts the connection but doesn't check the server's certificate. To check it, bundle the VM's certificate with the Lambda and use `sslmode=verify-full&sslrootcert=<path>`, as [the deploy guide](../deploy/README.md#the-lambdas-settings) describes. Every page the backend serves, including the lobby UI, the browser demo and the local host's `GET /game/{id}/graph`, is embedded in the binary.

### The connection pool

Every process has its own pool, and every Lambda instance is a separate process. These optional settings bound it:

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` | 2 | Most connections open at once |
| `ISLAND_BATTLES_DATABASE_MAX_IDLE_CONNECTIONS` | 2, or the open limit if lower | Most connections kept open while idle |
| `ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME` | `1m` | How long an idle connection stays open, as a Go duration |

A Lambda instance serves one request at a time, so the defaults suffice there. Cap the function's reserved concurrency so that **reserved concurrency × max open connections < PostgreSQL `max_connections`** (100 by default), leaving room for the migrate command and administration. For example, with the default of 2 connections, the default reserved concurrency of 5 uses at most 10. The free-tier budget below also grows with the concurrency. A frozen Lambda instance keeps its idle connections until it thaws or ends, so count them as open.

If PgBouncer is added later in transaction mode, it must be 1.21 or newer with `max_prepared_statements` set, because pgx uses named prepared statements by default.

### Logs and traces

[`observability`](observability/) gives both hosts the same request logging (#46):

- **Every request** is logged once, after it is answered. A 5xx answer or a recovered panic is one ERROR line, `Request failed` or `Request panicked`, with the operation, route, status, the error chain and the IDs involved: `lobby_id`, `game_key`, `game_id`, `generation_id` and `player_id`, as far as the request got. A panic also carries its value and stack, and answers 500. Every other request, client errors included, is an INFO line.
- **Recording causes:** a handler that turns an error into a response without its cause calls `observability.RecordError(ctx, err)` first, and `observability.Annotate` adds IDs. Huma's 5xx problems and the lobby's problems are recorded on their own, with what they wrap. A Huma API middleware, `observability.NameOperations`, names the operation before any group middleware runs. The answers that bypass Huma's transformers record their causes through hooks: the app's authenticator wrapper for the lobby's authentication problems, and the Centrifugo proxy's `realtime.WithErrorReporter` for its 503s.
- **Failed queries** are warnings, like slow ones: some fail as expected, such as a unique violation that answers 409.
- **Traces** are off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set, as OpenTelemetry's standard variable. Then each request, each GORM query and each Centrifugo API call gets a span. A request's sampled spans are exported over OTLP/HTTP before it returns, in its own goroutine: there's no batch processor, timer or kept-alive connection to the collector, so nothing runs between requests. Locally every request is traced. Trace headers on incoming requests are ignored, so callers can't start traces. A line logged inside a sampled trace carries `trace_id` and `xray_trace_id`.

The Lambda host also applies limits that keep CloudWatch Logs and X-Ray inside AWS's free tier, worst case included. Terraform sets them, and refuses them in `lambda.env` ([deploy/README.md](../deploy/README.md#logs-alerts-and-traces)):

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_CONCURRENCY` | 5 | C: the most instances that run at once, the function's reserved concurrency |
| `ISLAND_BATTLES_LOG_LINES_PER_MINUTE` | 5 | N: the most warning and error lines an instance writes a minute. Warnings may use N−1. The rest become one line that counts them |
| `ISLAND_BATTLES_LOG_LINE_BYTES` | 1024 | L: the most bytes of a line. Longer lines are cut and stay JSON |
| `ISLAND_BATTLES_TRACE_INTERVAL` | `180s` | T: an instance starts at most one trace every T, the first one T after it starts |

At startup the Lambda host works out the worst case for a 31-day month, C × 44,640 minutes × (N × (L + 128) + 384) bytes of logs and C × 2,678,400 s / T traces. Over 1.5 GB or 80,000, it refuses to serve: it answers 503 and logs why. A backend that fails to start for any other reason, such as an unreachable database, does the same and tries again at most every 10 seconds, rather than failing the cold start, which Lambda would retry on every request. The local server host applies none of these limits.

## Admin MCP server

[`cmd/admin-mcp`](cmd/admin-mcp/main.go) is a [Model Context Protocol](https://modelcontextprotocol.io) server (#79) that answers questions about lobbies, players and games, such as "How many lobbies are currently active?" or "How many players are in lobby X?". It runs on your own machine, serves one client over stdio, and connects straight to PostgreSQL, the local database or production. It adds no endpoint to the backend and is never deployed. It's built on the official Go SDK, `github.com/modelcontextprotocol/go-sdk`.

| Tool | What it answers |
| --- | --- |
| `overview` | Lobby counts by game key and phase; active lobbies, which have a connected member; members connected now; live guest sessions; games running and finished; generations created in the last hour |
| `list_lobbies` | Lobbies, newest first, by game key, phase or only active ones, with their member and connected counts, host, times and game |
| `get_lobby` | One lobby by ID or join code: also its settings, teams, members with their leases and grace deadlines, start roster and retention deadline |
| `find_players` | Players by ID or by part of a display name, with their live sessions (times only), lobbies and start roster places |
| `list_games` | Games, newest first, running or finished, with teams, roster, generation count, outcome and retention deadline |
| `get_game` | One game by ID or lobby ID: also its current turn, who has completed it, and each team's castles and soldiers |
| `get_generation` | One generation by ID or turn: each player's commands and completion, the commands that produced it, its clashes, and optionally the board that players receive |
| `describe_schema` | The tables, or one table's columns, from `information_schema` |
| `query` | One SQL query, for everything else |

A member is connected while one of the leases of its current membership has neither expired nor been released. The tools work this out from the timestamps, as the lobby does, but never apply expiry themselves, so a lobby that nobody has touched since its members left may still be waiting. Answers are JSON with stable field names and times in UTC as RFC 3339. No answer contains `lobby_sessions.bearer_hash`, a connection token or any other secret.

The lobby tools are the lobby's own, [`lobby/adminmcp`](../lobby/README.md#admin-mcp-tools), which reads only the `lobby_*` tables, so another game can reuse them. The binary adds the Island Battles game tools and the SQL tools.

**It doesn't write.** Its one connection's sessions start with `default_transaction_read_only=on` and a 5-second `statement_timeout`, and every tool runs in a read-only transaction that is rolled back. After every call it releases the session's advisory locks, which outlive the rollback and would otherwise block the migrations until the server exits. `query` runs the statement through a cursor (`DECLARE ... CURSOR FOR <sql>`), which takes only a `SELECT`, `VALUES`, `TABLE` or `WITH` query and fetches at most one row more than its cap. Commands such as `SET`, `COPY` or `COMMIT` are therefore syntax errors, and the read-only transaction refuses anything in a query that writes, such as `nextval()` or `FOR UPDATE`. Statements go over PostgreSQL's extended protocol, which refuses a string of several statements. Results are cut to 200 rows and 64 KiB, and values to 16 KiB, and `truncated` says when. The driver still reads each whole row into memory first, so one huge value, such as a `repeat()` of many megabytes, costs about twice its size in memory on your machine, within the statement timeout.

**The guarantee is complete only for the production read-only role**, which is no superuser and may only read. A superuser can do much that a read-only transaction doesn't stop, such as reading server files, ending other sessions or creating replication slots, and the local development role is one. When the server connects as a superuser, it logs a warning and its sessions `SET ROLE pg_read_all_data`, which reads every table and nothing more. That is best-effort: a query can switch the role back with `set_config` for its own duration.

**`bearer_hash` is protected only by the production role's column grant**, which makes `lobby_sessions.bearer_hash` unreadable. The typed tools never read it. Locally, `query` also leaves out any result column named `bearer_hash`, but that is best-effort: an alias or `row_to_json` gets past it. If a migration outruns the production role's grants and the role can read the column, `query` refuses to run, and the server logs why at startup and on each refused call, until a VM deploy grants them again ([the deploy guide](../deploy/README.md#the-admin-mcp-server)).

It requires `ISLAND_BATTLES_ADMIN_DATABASE_URL`, and reads the optional [`ISLAND_BATTLES_GAME_RETENTION_PERIOD`](#game-retention) the same way the backend does, to say when retention deletes a game. Set it only if the backend you ask about sets it. It logs to stderr only, because stdout carries the MCP stream. Locally, use the development database's URL from `.env`. For production, use the read-only role described in [the deploy guide](../deploy/README.md#the-admin-mcp-server).

Register it with Claude Code from the repository root, which `go run` needs as its working directory:

```sh
claude mcp add island-battles-admin \
  --env ISLAND_BATTLES_ADMIN_DATABASE_URL='postgres://islandBattles:islandBattles@localhost:5432/islandBattles?sslmode=disable' \
  -- go run ./backend/cmd/admin-mcp
```

`go run` compiles it on every start. A built binary starts at once and runs from any directory:

```sh
go build -o ~/.local/bin/island-battles-admin-mcp ./backend/cmd/admin-mcp
claude mcp add island-battles-admin --env ISLAND_BATTLES_ADMIN_DATABASE_URL=... -- ~/.local/bin/island-battles-admin-mcp
```

Rebuild it after pulling changes to the tools. `claude mcp list` shows whether the server connects, and `/mcp` inside Claude Code shows its tools. Other MCP clients start the same command with the same environment variable. To use the local database and production side by side, register the server twice under different names.

## Verification

```sh
go test ./... ./lobby/...
go vet ./... ./lobby/...
go test -race ./lobby/... ./backend/lobbyadapter/... ./backend/dal/...
./scripts/test-lobby-acceptance.sh
```

Database tests require a Docker-compatible runtime and use disposable PostgreSQL. On rootless Podman, set the runtime up first as described in [Database tests on rootless Podman](#database-tests-on-rootless-podman). The live runner supports Linux Docker or Podman, creates unique test containers, and preserves failure logs. It never uses the configured developer database.

Castle and soldier group IDs stay stable across generations. Backend Go models call them `PersistentCastleID` and `PersistentSoldierGroupID`, stored in `persistent_castle_id` and `persistent_soldier_group_id`; the API calls them `id`, `origin_castle_id`, `destination_castle_id`, `group_id` and, for castle upgrades, `castle_id`. Soldier group IDs come from a per-game counter (`games.next_soldier_group_id`), so a defeated or arrived group's ID is never reassigned.

### Database tests on rootless Podman

The Go database tests start their PostgreSQL containers through testcontainers-go. Rootless Podman runs them without code changes once two things are set up.

1. Enable Podman's Docker-compatible API socket. It's off by default:
   ```sh
   systemctl --user enable --now podman.socket
   ```
2. Point testcontainers at the socket and disable Ryuk, its cleanup container. Ryuk asks for Docker's default `bridge` network, which rootless Podman calls `podman`, so with it enabled every database test fails with a `reaper:` error. To configure the shell:
   ```sh
   export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
   export TESTCONTAINERS_RYUK_DISABLED=true
   ```
   IDEs started from the desktop often don't load shell rc files. To configure the terminal and IDEs at once, put the same settings in `~/.testcontainers.properties` instead, replacing `<uid>` with the output of `id -u`:
   ```properties
   docker.host=unix:///run/user/<uid>/podman/podman.sock
   ryuk.disabled=true
   ```
   testcontainers-go v0.40.0 resolves the Docker host in this order: its `tc.host` setting, the `DOCKER_HOST` variable, the Docker context, `/var/run/docker.sock`, and only then the `docker.host` property. If `/var/run/docker.sock` exists, for example from the `podman-docker` package, it wins over the property.

With Ryuk off, normal runs still remove their containers through `t.Cleanup`. An interrupted run (Ctrl+C, stopping the run in an IDE, or a test timeout) leaves its PostgreSQL containers behind. List them with `podman ps -a --filter label=org.testcontainers=true` and remove them with `podman rm -f`. Don't use `podman container prune` for this: if they're stopped, it also removes the `island-battles-dev-db` and `island-battles-dev-centrifugo` containers that `scripts/dev.sh` creates.

## Player API document

[`api/openapi.json`](api/openapi.json) describes the operations a player client calls, including `GET /game/upgrades` and `GET /bots/defaults` ([default bots](#default-bots)), the `GenerationEvent` and `GameEndedEvent` pushed on its channel, the `MapTemplate` [map file format](#map-files), the `Demo` [demo file format](#demos), and `BotTurn` and `BotTurnResult`, the [bot turn contract](#bot-turns). Player clients are generated from it; see the [conformance suite](../clients/conformance/README.md) for the behavior they implement by hand. API view types are published without their `View` suffix (`CastleView` becomes `Castle`). After changing them, update the document; a test fails while it is stale:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
```
