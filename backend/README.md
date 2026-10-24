# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and durable event records; Centrifugo handles WebSocket transport and history recovery. Lobby and game notifications use their respective event queues. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). All seven backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret and internal proxy secret. Startup rejects missing values. Public clients receive only their guest credential and short-lived connection JWT.

Run `./scripts/dev.sh` from the repository root to load `.env`, start local dependencies with Docker Compose or Podman, and run the backend. If dependencies are already running and the environment is exported, `go run ./backend` also works. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`lobby/api/`](../lobby/api/README.md).

Open `/lobby/ui/` for the [temporary two-player lobby frontend](../lobby/frontend/README.md). It is embedded in the backend and needs no separate frontend build.

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`.

## Browser demo

The backend lists Island Battles in the lobby frontend's game catalog with [`lobbyadapter.FrontendGame`](lobbyadapter/frontend.go), and serves the browser [example player](demo/example-player.mjs) under `/game/demo/`. Once a lobby starts, the frontend shows the lobby replay from `/game/from-lobby/{lobby_id}/replay`, which takes new generations from the page's `lobby.game-state` messages.

For the quickest test, open `/lobby/ui/` and click **New demo lobby**. It creates Alex on Blue in the current tab, opens Sam on Red in a second tab, marks both ready, and starts automatically. Both tabs run the example player: send one soldier from the first available owned castle to the first enemy castle, then finish the turn. Players submit immediately when a generation arrives, without an artificial client delay. Replay playback has its own viewing pace. Failed requests still use retry backoff.

The board follows new generations live. **Pause this player** stops that tab from submitting its next turn; the other player then waits at the turn barrier. **Resume this player** continues. The timeline lets you review previous generations; **Follow live** returns to the newest state. Keep both tabs open. A matching, fully typed Python implementation is in [`examples/player.py`](../clients/island-battles-python/examples/player.py).

Manually created or joined lobbies wait for commands through the [Island Battles Python client](../clients/island-battles-python/README.md); only demo invitations let the browser example player submit turns. Disconnected players remain part of the turn barrier.

Run the browser player's strategy, deduplication, retry and no-polling checks from the repository root with `node backend/demo/example-player.test.mjs`.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. Registered example games are `island-battles` and `quiz-demo`. Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. There is no background sweeper. Every lobby request, including the heartbeat, and every Centrifugo subscription refresh applies due deadlines first and pushes the result. Connected members therefore see a peer's expiry within one heartbeat or refresh, and a lobby nobody is connected to catches up on its next access. Deadlines are persisted, so they also apply after a restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

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

Each request UUID identifies one immutable batch for one player and generation. Retrying the exact request is safe, including after a timeout or generation advance; reusing that UUID with changed content conflicts. New requests for an old generation and new commands after turn completion also conflict. A game stops at 300 generations: turns are not paced, so the cap keeps one game of fast bots from loading the server, and commands for the 300th generation conflict. Errors use Huma problem JSON: 401 for missing/invalid credentials, 404 for an inaccessible game, 403 for forbidden commands or ownership, 409 for stale/sealed turns, UUID conflicts, or insufficient unreserved soldiers, and 422 for invalid parameters. Read the response detail before retrying with a new request UUID.

The response is the caller's current `GenerationState`. The final player's completion returns the new generation. Every roster player also receives a durable `game.next_generation` event on their authenticated personal `player:<player_uuid>` channel. Delivery is at least once, so clients deduplicate by game and generation. The [Island Battles Python client](../clients/island-battles-python/README.md) builds a strategy through `player_factory(team_color)` after its first authorized state read. Strategies subclass the abstract `Player` base class and receive typed generation snapshots and a callback-scoped command receiver; a successful callback return automatically completes the turn. Its public snapshots omit transport and submission metadata, and its receiver sends one soldier group per call using direct castle IDs and a soldier amount for the callback's generation. The client reconciles authorized HTTP state on subscription/reconnect. The browser demo uses the same wire events. Neither game client polls by default. The generic lobby protocol is unchanged.

`GET /game/from-lobby/{lobby_id}/replay` shows the players on the frozen roster the recorded generation history. Games save their initial frame and append a frame after each completed turn. Each frame is the same `Generation` board view that players receive in `GenerationState.generation`, with castles and soldier groups sorted by ID, so the page appends live generations unchanged.

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

## Schema and recovery

Startup first applies two sets of versioned SQL migrations, which keep existing rows: the lobby's (`lobby/postgres/migrations/`, recorded in `lobby_schema_migrations`) and the map templates' (`backend/dal/migrations/`, recorded in `map_template_schema_migrations`). Each set runs in one transaction under a PostgreSQL advisory lock, so concurrent starts wait for each other. A database created before the schema was versioned is recorded at migration 1 without running it, but only if it has every table and column that migration 1 creates; otherwise startup fails and names what is missing. Startup then AutoMigrates the game tables and game-team bindings. Game data is not migrated: when a schema change strands it, reset the game tables (see the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md)). New games use the nullable unique `lobby_id` link. Creating or joining a lobby does not create a game; starting an Island Battles lobby initializes generation one.

The lobby outbox retries publication after Centrifugo failure. Generation notifications are also persisted with the state transition and retried by the publisher; they contain the recipient's authorized generation state and are sent only to that player's personal channel. Clients reconcile authorized snapshots after subscription/reconnect, so missed pushes and restarts recover through HTTP state.

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

[`api/openapi.json`](api/openapi.json) describes the operations a player client calls and the `GenerationEvent` pushed on its channel. Player clients are generated from it; see the [conformance suite](../clients/conformance/README.md) for the behavior they implement by hand. API view types are published without their `View` suffix (`CastleView` becomes `Castle`). After changing them, update the document; a test fails while it is stale:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
```
