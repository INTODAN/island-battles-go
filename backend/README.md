# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and game state, and no messages waiting to be sent. Centrifugo handles WebSocket transport and history recovery. A request that commits a lobby change or a new generation pushes it to Centrifugo before it responds, and the backend runs no work between requests. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). Seven of the backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret and internal proxy secret. Startup rejects missing values. Only the HTTP server host reads the HTTP address; the [Lambda host](#hosts-and-deploys) has none. The database pool settings are optional; see [the connection pool](#the-connection-pool). So is `ISLAND_BATTLES_ADMIN_API_KEY`, the [admin API key](#map-files) that `POST /map/create` requires; without it, that endpoint refuses every request. Public clients receive only their guest credential and short-lived connection JWT.

Run `./scripts/dev.sh` from the repository root to load `.env`, start local dependencies with Docker Compose or Podman, migrate the schema and run the backend. If dependencies are already running and the environment is exported, run `go run ./backend/cmd/migrate` and then `go run ./backend`. The backend only connects to the database, so it fails on a database the migrate command hasn't brought up to date. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`lobby/api/`](../lobby/api/README.md).

Open `/lobby/ui/` for the [temporary two-player lobby frontend](../lobby/frontend/README.md); `/` redirects there. It is embedded in the backend and needs no separate frontend build.

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`.

## Browser demo

The backend lists Island Battles in the lobby frontend's game catalog with [`lobbyadapter.FrontendGame`](lobbyadapter/frontend.go), and serves the browser [example player](demo/example-player.mjs) and the create form's [map choice](demo/lobby-settings.mjs) under `/game/demo/`. Once a lobby starts, the frontend shows the lobby replay from `/game/from-lobby/{lobby_id}/replay`, which takes new generations from the page's `lobby.game-state` messages.

For the quickest test, open `/lobby/ui/` and click **New demo lobby**. It creates Alex on Blue in the current tab, opens Sam on Red in a second tab, marks both ready, and starts automatically. Both tabs run the example player: send one soldier from the first available owned castle to the first enemy castle, then finish the turn. Players submit immediately when a generation arrives, without an artificial client delay. Replay playback has its own viewing pace. Failed requests still use retry backoff.

The board follows new generations live. **Pause this player** stops that tab from submitting its next turn; the other player then waits at the turn barrier. **Resume this player** continues. The timeline lets you review previous generations; **Follow live** returns to the newest state. Keep both tabs open. A matching, fully typed Python implementation is in [`examples/player.py`](../clients/island-battles-python/examples/player.py).

Manually created or joined lobbies wait for commands through the [Island Battles Python client](../clients/island-battles-python/README.md); only demo invitations let the browser example player submit turns. Disconnected players remain part of the turn barrier.

Run the browser player's strategy, deduplication, retry and no-polling checks from the repository root with `node backend/demo/example-player.test.mjs`, and the map choice's checks with `node backend/demo/lobby-settings.test.mjs`.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. Registered example games are `island-battles` and `quiz-demo`. A create request may carry a `game_settings` object, which the game checks at creation and receives, compacted with sorted keys, in the started snapshot. Settings never change after creation. Island Battles reads them as its [map choice](#choosing-a-lobbys-map); `quiz-demo` has no game behind it and accepts any object. Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. There is no background sweeper. Every lobby request, including the heartbeat, and every Centrifugo subscription refresh applies due deadlines first and pushes the result. Connected members therefore see a peer's expiry within one heartbeat or refresh, and a lobby nobody is connected to catches up on its next access. Deadlines are persisted, so they also apply after a restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

The host is the earliest active joiner. Team changes clear readiness. At least two members are required to start; all active members must choose a team, become ready and have a live lease. Starting freezes the roster; only roster players can subsequently rejoin that started room. An empty waiting room closes. Expired guest credentials require an explicit new session and do not transfer ownership.

## Interactive Island Battles generations

Starting an `island-battles` lobby initializes generation one. Each occupied team starts with a castle and 30 soldiers. The frozen roster defines who takes part in each generation: every roster player can submit commands and then complete their turn, and the game advances exactly once after all roster players complete. A player disconnecting does not remove them from this barrier; their teammates wait until that player reconnects and completes the turn.

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
    }
  ]
}
```

`send_soldier_group` is the only public command. Commands may be sent in several batches with `complete: false`; `complete: true` seals the turn. Omitting `complete` defaults to true for a one-batch turn. An empty command list is valid, so send `{"commands":[],"complete":true}` (with the other required fields) to pass. Once complete, that player's turn is sealed for the generation. The server checks ownership and available soldiers across all accepted orders, including reservations by teammates. It rejects a whole invalid batch without storing any part of it or completing the turn.

Each request UUID identifies one immutable batch for one player and generation. Retrying the exact request is safe, including after a timeout or generation advance; reusing that UUID with changed content conflicts. New requests for an old generation and new commands after turn completion also conflict. A game ends after the turn that leaves one team with castles or soldier groups (neutral castles need not be captured), as a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins and equal counts draw. Turns are not paced, so the cap keeps one game of fast bots from loading the server. The game is then over: its final generation stays readable, `GenerationState.outcome` says how it ended, and every new command conflicts with problem type `urn:island-battles:problem:game-ended`. Errors use Huma problem JSON: 401 for missing/invalid credentials, 404 for an inaccessible game, 403 for forbidden commands or ownership, 409 for stale/sealed turns, UUID conflicts, or insufficient unreserved soldiers, and 422 for invalid parameters. Read the response detail before retrying with a new request UUID.

The response is the caller's current `GenerationState`. The final player's completion returns the new generation. The request that completes the turn also pushes a `game.next_generation` event to every roster player's authenticated personal `player:<player_uuid>` channel. The turn that ends the game pushes `game.ended` instead, with the final generation and its outcome; it is the game's last event. Centrifugo keeps a short history on that channel for reconnecting clients. A client can see the same generation both in an event and over HTTP, so clients deduplicate by game and generation. The [Island Battles Python client](../clients/island-battles-python/README.md) builds a strategy through `player_factory(team_color)` after its first authorized state read. Strategies subclass the abstract `Player` base class and receive typed generation snapshots and a callback-scoped command receiver; a successful callback return automatically completes the turn. Its public snapshots omit transport and submission metadata, and its receiver sends one soldier group per call using direct castle IDs and a soldier amount for the callback's generation. The client reconciles authorized HTTP state on subscription/reconnect. The browser demo uses the same wire events. Neither game client polls by default. The generic lobby protocol is unchanged.

`GET /game/from-lobby/{lobby_id}/replay` shows the players on the frozen roster the recorded generation history. The replay is not stored: each request loads the game's generations in ID order, in a fixed number of batched queries, and turns each into a frame. Completing a turn writes no replay data. Each frame is the same `Generation` board view that players receive in `GenerationState.generation`, with castles and soldier groups sorted by ID, so the page appends live generations unchanged. Only the replay's static metadata lives on the game: its teams with their lobby names and players' display names, copied at start because the lobby is deleted later, and its outcome, which stays empty while the game runs and is saved by the turn that ends the game. The page shows the outcome, also when it arrives with a live state.

Clashes are saved atomically with each generation. Generation snapshots, live notifications and replay frames always include a `clashes` array, empty for turns without combat. Each event has a generation-local `id`, a `time` fraction within the turn, and participant records with `group_id`, `team_color`, contact `x`/`y`, `clash_radius`, `soldiers_before`, and `soldiers_after`. Counts describe each individual contact, including multiple clashes by the same group in one turn. The stored and published form is the same `models.Clash`.

The lobby battlefield pulses an impact ring, shows loss numbers only above surviving troops, and leaves a dashed outline with × for defeated groups. Defeated markers stay at their recorded contact positions, including when they overlap other troops or castles. Losses from multiple clashes are combined above each surviving troop. Effects remain visible while paused and clear on the next generation; scrubbing restores static effects, and reduced-motion settings disable the pulse. Combat mechanics and soldier totals are unaffected.

Soldier groups have a persisted `ClashRadius`, defaulting to **0.25 world units**, which is copied to subsequent generations. Enemies clash when their center-to-center distance reaches either group's radius (the larger radius, rather than their sum). Contacts resolve in time order using troop counts before casualties. If one group has strictly less than half the other's soldiers, the survivor completes its movement. Otherwise the groups stop at the midpoint between their contact positions for the generation; exactly half also stops. In a simultaneous connected clash, any comparable opposing pair causes a stop at the average participant position. Paths are recalculated after a stop so survivors can still fight later enemies. Dead and empty groups cannot participate in later clashes. Simultaneous connected contacts resolve together; each team loses up to the combined opposing force, with losses assigned in persistent group ID order.

Each group's `clash_stop_generations` upgrade controls its own pause duration: **level 1 → 3 generations**, **level 2 → 2**, and **level 3 → 1**, including the clash generation. Troops copy this level when spawned, so upgrading the castle does not change existing troops. Each group persists its remaining `MovementPauseGenerations`, which is copied across generations and decremented when it stays still. Smaller attacks do not restart or extend an existing pause.

Groups leave field combat when they reach their destination, including arrivals exactly tied with contact. Already-arrived groups cannot clash with newly spawned troops. Short trips retain normal movement speed until arrival. At the end of the turn, castle arrivals resolve in persistent group ID order against the updated castle state, preserving earlier damage, captures, and reinforcements. Sending zero soldiers or sending to the same castle is rejected.

The replay draws troop circles using their stored radius and castle circles with a visual-only radius of **0.5 units**, with the same world scale on both axes.

The Island Battles adapter wraps the generic start operation. Generic lobby start freezes the roster first; game initialization uses that roster and persists generation one and its player/team bindings. Concurrent or repeated starts reuse the initialized game. Other game keys are unaffected.

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

Castles and soldier groups persist an `upgrades` map, such as `{"speed":1,"clash_stop_generations":1}`. Each upgrade type defines its own level effects, fixed soldier costs, and maximum in `models/upgrades.go`. Speed currently has four levels: **1 → 0.5**, **2 → 1**, **3 → 1.5**, and **4 → 2** world units per turn. The `clash_stop_generations` type has three levels, reducing stoppage from **3** to **2** to **1** generations. Purchasing its levels **2** and **3** costs **10** and **20 soldiers**. Omitted upgrades default to level 1, and every created castle and group stores all of its levels. Map templates can provide initial castle levels. Purchasing speed levels **2**, **3**, and **4** costs **10**, **20**, and **30 soldiers**, respectively; the default level 1 has no purchase cost.

The internal migration engine accepts `upgrade_castle` to increase a castle's upgrade by one level. Public player submissions currently allow only `send_soldier_group`; the raw `/generation/migrate` endpoint is removed. Trusted server code can pass this command to `services.MigrateGeneration`:

```json
{
  "CommandType": "upgrade_castle",
  "Parameters": {"castle_id": 0, "upgrade_type": "speed"}
}
```

Use `"upgrade_type":"clash_stop_generations"` in the same command to reduce future troops' clash pauses.

Commands execute in the supplied order before movement. Sending troops after upgrading copies the new levels; troops sent before upgrading retain the old levels. Every group keeps its own snapshot throughout later turns. The castle must have enough soldiers at the moment the command executes. The purchase atomically deducts the target level's cost and advances the upgrade; an exact balance is allowed and leaves zero soldiers before normal turn production. Insufficient soldiers, maximum levels, unknown upgrade types and invalid levels return typed model errors. A rejected command rolls back the whole turn. The temporary lobby displays recorded generations; the Python gameplay client submits player turns.

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

Open `/map/builder/` to build a map in the browser, from the [`mapbuilder`](mapbuilder/) package embedded in the backend. Set the map's name, players, default soldiers and plane size; add castles with **Add castle** or a double-click on the plane, drag them or move them with the arrow keys, and remove them with **Delete**. Each castle has a team colour or neutral, an optional starting soldier count and its upgrade levels, which the page reads from `upgrades.json` next to it, so they follow `models/upgrades.go`.

The page holds no copy of the map rules. Each edit sends the file to `POST /map/validate`, and the page shows each problem next to the castle and field it names. **Download map** saves exactly the text that the server last accepted; while the map is invalid or unchecked, the button is disabled and the page says why. **Open map file** loads a file to keep editing; a downloaded file reopens unchanged. A hint names any team from 1 to `player_amount` that owns no castle, because lobbies are expected to need one for each team, but it doesn't block the download.

When the lobby's create form opens the page with its **Build a map…** button, the page also shows **Use in lobby** next to **Download map**. It needs what Download needs, and a castle for every team from 1 to `player_amount`, because lobby creation rejects any other map; while it is disabled, the page says why. It posts `{"type": "island-battles.map-file", "text": ...}` with exactly the accepted text to the form's window, with this page's origin as the target origin, and says so on the page. The button appears only when a same-origin window opened the page and is still open; when that window closes, the page hides the button and says to download the map instead. Opened directly, the page shows no such button.

Run the page logic's checks from the repository root with `node backend/mapbuilder/builder.test.mjs`.

### Choosing a lobby's map

An Island Battles lobby starts on the map its `game_settings` choose, published as the `LobbySettings` schema:

- `{"map_template_id": 4}` picks a default map. `GET /map/templates` lists them with each map's name, `team_count`, plane size and castle count.
- `{"map": {...}}` carries a whole map file, such as one downloaded from the map builder. It must pass `models.DecodeMapTemplate`.
- `{}`, or no settings, starts on the generated map, with one castle per team that has players.

Pass at most one of the two. The map sets the lobby's teams and its neutral castles, so a lobby on a map with `player_amount` N has exactly N teams, and the map's castle team colours are exactly 1 to N, with 0 neutral. The lobby's first team plays colour 1, its second colour 2, and so on. `GET /map/templates` leaves out stored maps that fit no lobby: those whose colours skip one, and those made for fewer than 2 or more than 16 teams, the lobby's limits. A bad choice fails lobby creation with `invalid_game_settings`, and the detail names the invalid fields, listing the first 20 and counting the rest, such as `game_settings.map_template_id: no default map has ID 9` or `game_settings.map.castles[2].x: must be inside the plane, from -5 to 5`. Settings are capped at the lobby's 16 KiB, which holds a map file of over a hundred castles.

The lobby frontend's create form offers the same choice through the catalog's settings module, [`demo/lobby-settings.mjs`](demo/lobby-settings.mjs): the generated map, a default map, or a loaded map file. Picking a map fills in one team per colour, named after the colour the board draws (Blue, Red, Green, Purple, Amber, Teal), with one seat each, and the room shows the chosen map's name. The board fits itself to the map's castles, keeping at least the generated map's 8 by 6 units.

The form's **Build a map…** button opens [the map builder](#map-builder) in a window named `island-battles-map-builder`, which keeps a reference to the form, and clicking it again focuses that window. When the builder's **Use in lobby** posts a map, the form selects **Load a map file…** with that map, as if the host had loaded its file, and names the map, its size and its teams. The form accepts the message only when its origin is the form's own origin and its source is the window the form opened; it ignores everything else. A message that holds no usable map shows its error as a bad file does. The latest map or file the host picks wins.

## Schema and recovery

The hosts never change the schema. The migrate command, [`cmd/migrate`](cmd/migrate/main.go), does, once per deploy and before the new build serves; `scripts/dev.sh`, the acceptance and conformance runners and the Go test helpers (`testutils.InitTestDB`) run it, or the `dependencies.MigrateSchema` function behind it, first. It reads only `ISLAND_BATTLES_DATABASE_URL` and the optional pool settings. It first applies two sets of versioned SQL migrations, which keep existing rows: the lobby's (`lobby/postgres/migrations/`, recorded in `lobby_schema_migrations`) and the map templates' (`backend/dal/migrations/`, recorded in `map_template_schema_migrations`). Each set runs in one transaction under a PostgreSQL advisory lock, so concurrent runs of a set wait for each other. A database created before the schema was versioned is recorded at migration 1 without running it, but only if it has every table and column that migration 1 creates; otherwise the command fails and names what is missing. It then AutoMigrates the game tables and game-team bindings, drops the retired `generation_notifications` table, and drops the `games.replay` column, because replays are now derived from generations. The game steps take no lock, so never run two migrate commands against one database at once. Running it again finds nothing to do. Game data is not migrated: when a schema change strands it, reset the game tables (see the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md)). New games use the nullable unique `lobby_id` link. Creating or joining a lobby does not create a game; starting an Island Battles lobby initializes generation one.

Lobby invalidations and generation events are pushed after their transaction commits, through the lobby's `realtime.Publisher`. It sends a request's messages to Centrifugo's HTTP API in one call and retries what failed twice within about a second. If Centrifugo is still unreachable, it logs an error and the request succeeds anyway, because the change is already committed. Nothing is queued for later. Generation events contain the recipient's authorized generation state and go only to that player's personal channel. Clients reconcile authorized snapshots after subscription/reconnect, so a failed push or a restart recovers through HTTP state.

## Game retention

A game is deleted with all its rows 7 days after its latest generation was created, or after the game itself was created if it never got a generation. Finished and abandoned games are treated alike, because an abandoned game never gets an outcome. The replay goes with the game, because it is built from the generations. Map templates (`map_templates`, `castle_templates`) are never deleted: a game's maps are copies of them. The game's [`retention`](retention/retention.go) package and [`dal/game_retention.go`](dal/game_retention.go) own this. They never read lobby tables, and the lobby deletes its own data separately.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_GAME_RETENTION_PERIOD` | `168h` | How long a game is kept after its latest generation, as a positive Go duration. Go durations have no days unit, so 7 days is `168h`. |

There is no timer. The game's own requests run the passes as they end: turns (`POST /generation/commands`), which are what adds game data, and the state and replay reads that players poll (`GET /game/from-lobby/{lobby_id}/generation` and `/replay`). When nobody plays, no game data is added, so none needs deleting. The pass runs before the response is sent, so the request waits for it. Each process runs at most one pass at a time, and at most one a minute while passes find no backlog. A pass that fills a batch lets the next request run another, so a backlog drains one batch per request. A turn adds one generation and a pass deletes up to 10, so deleting keeps ahead of playing. A pass has a 5-second timeout, and a failed pass is logged without failing the request. Against a local database a pass takes under a millisecond when there's nothing to delete and 5 to 7 ms for a full batch. On Lambda every statement, and every BEGIN and COMMIT, is a round trip over TLS to the VM: 4 for a pass with nothing to delete, about 20 for a batch from one long game, and about 50 for a pass that finishes 3 small games, the most a pass purges. That is a few to about a hundred milliseconds at a few milliseconds a round trip, at most once a minute per instance while there's no backlog, and on every game request while there is one.

A pass has two steps. Each runs even when the other fails, and the pass logs every failure:

1. **Retire** up to 10 expired games by soft-deleting the game row. Every request finds and locks its game through GORM's default scope, so a retired game disappears from every endpoint at once, and its turns and reads return 404. The step skips games that a request holds locked (`FOR UPDATE SKIP LOCKED`), so it never waits for a turn and never retires a game while a turn on it is in progress. It checks each locked candidate again in a new statement, so a turn that commits just before the lock keeps its game.
2. **Purge** up to 10 generations of at most 3 retired games, lowest game IDs first, with `Unscoped`, so rows are removed for real. Each generation goes with its requests, player inputs, commands and soldier groups, then the generation, then its map's castles and the map. A retired game with no generations left then goes with its remaining requests, frozen players and team bindings. Every game is purged in its own transaction, so a game whose purge fails is rolled back and retried by later passes while the other games are still deleted. A long game takes several passes, and no transaction deletes more than 10 generations' rows.

The rows a pass deletes belong to games nobody can reach any more, and the indexes on the foreign key columns (`generations.game_id` and `map_id`, `castles.map_id`, `commands.generation_id`, and `soldier_groups.generation_id`, `origin_castle_id` and `destination_castle_id`) keep the deletes and their foreign key checks from scanning other games' rows. Passes on several hosts at once lock different games, so they never wait for each other either.

## Hosts and deploys

[`app.New`](app/app.go) builds the HTTP handler from the configuration and a database connection. Two hosts serve it:

- **The HTTP server,** [`backend/main.go`](main.go), for local development. It listens on `ISLAND_BATTLES_HTTP_ADDR`, shuts down gracefully on SIGINT or SIGTERM and logs in colour through `tint`. It alone passes `app.WithGameGraph()`, so it alone serves `GET /game/{id}/graph`, which reads any game without authentication.
- **The AWS Lambda function,** [`cmd/lambda`](cmd/lambda/main.go), behind a Function URL, which players reach through CloudFront at the game's name ([deploy/README.md](../deploy/README.md)). It is public, so it serves no graph page, and every route it serves that reads game data requires a player on the game's frozen roster. It serves the handler through `lambdaurl`, so the Function URL must use the `RESPONSE_STREAM` invoke mode. Streaming also lets replay pages, which inline JSON that grows with the game, pass Lambda's 6 MB limit on buffered responses; streamed responses may reach 200 MB. It logs warnings and errors only, as JSON lines with the `level` and `timestamp` keys that Lambda's log-level filtering reads, within the limits in [Logs and traces](#logs-and-traces).

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

[`api/openapi.json`](api/openapi.json) describes the operations a player client calls, the `GenerationEvent` and `GameEndedEvent` pushed on its channel and the `MapTemplate` [map file format](#map-files). Player clients are generated from it; see the [conformance suite](../clients/conformance/README.md) for the behavior they implement by hand. API view types are published without their `View` suffix (`CastleView` becomes `Castle`). After changing them, update the document; a test fails while it is stale:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
```
