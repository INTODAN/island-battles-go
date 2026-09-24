# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and an outbox; Centrifugo handles WebSocket transport and history recovery. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). All seven backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret and internal proxy secret. Startup rejects missing values. Public clients receive only their guest credential and short-lived connection JWT.

Run `./scripts/dev.sh` from the repository root to load `.env`, start local dependencies with Docker Compose or Podman, and run the backend. If dependencies are already running and the environment is exported, `go run ./backend` also works. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`lobby/api/`](../lobby/api/README.md).

Open `/lobby/ui/` for the [temporary two-player lobby frontend](../lobby/frontend/README.md). It is embedded in the backend and needs no separate frontend build.

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. Registered example games are `island-battles` and `quiz-demo`. Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. A five-second sweeper and command-time normalization apply persisted deadlines after restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

The host is the earliest active joiner. Team changes clear readiness. At least two members are required to start; all active members must choose a team, become ready and have a live lease. Starting freezes the roster; only roster players can subsequently rejoin that started room. An empty waiting room closes. Expired guest credentials require an explicit new session and do not transfer ownership.

## Automatic Island Battles games

Starting an `island-battles` lobby through the hosted lobby API also creates and simulates its game. Each occupied team starts with a castle and 30 soldiers. Automatic orders send half a castle's soldiers toward the nearest enemy every six turns, staggered between teams. The existing generation engine handles production, movement, combat, and captures. The simulation ends when one team remains or after 60 turns; reaching the limit reports no winner.

Both players see the saved replay at `/lobby/ui/`. Playback controls do not issue gameplay commands. `GET /game/from-lobby/{lobby_id}/replay` requires a guest bearer credential and current lobby membership. Player names, teams, frames, and outcome are saved together and survive restarts. The replay uses embedded HTML/SVG with no external JavaScript dependency.

Soldier groups have a persisted `ClashRadius`, defaulting to **0.25 world units**, which is copied to subsequent generations. Enemies clash when their center-to-center distance reaches either group's radius (the larger radius, rather than their sum). Contacts resolve in time order using troop counts before casualties. If one group has strictly less than half the other's soldiers, the survivor completes its movement. Otherwise the groups stop at the midpoint between their contact positions for the generation; exactly half also stops. In a simultaneous connected clash, any comparable opposing pair causes a stop at the average participant position. Paths are recalculated after a stop so survivors can still fight later enemies. Dead and empty groups cannot participate in later clashes. Simultaneous connected contacts resolve together; each team loses up to the combined opposing force, with losses assigned in logical group order.

`models.ClashStopGenerations` controls the pause duration and defaults to **1**, including the clash generation. Set it before running games: **2** also skips the next generation, while **0** disables pauses. Each group persists its remaining `MovementPauseGenerations`, which is copied across generations and decremented when it stays still. Smaller attacks do not restart or extend an existing pause. Existing database rows default to no remaining pause.

Groups leave field combat when they reach their destination, including arrivals exactly tied with contact. Already-arrived groups cannot clash with newly spawned troops. Short trips retain normal movement speed until arrival. At the end of the turn, castle arrivals resolve in logical group order against the updated castle state, preserving earlier damage, captures, and reinforcements. Sending zero soldiers or sending to the same castle is rejected.

The replay draws troop circles using their stored radius and castle circles with a visual-only radius of **0.5 units**, with the same world scale on both axes. Existing troop rows receive the default radius during migration; saved replays retain their original simulation results.

The Island Battles adapter wraps the generic start operation. Generic lobby start freezes the roster first; the game then locks its bound row and commits all simulated generations and its replay in one transaction. Concurrent or repeated starts reuse the saved result. A failed simulation rolls back its generations; retry start (or the endpoint below) to finish preparing the same game. Other game keys are unaffected.

### Retry or explicitly bind a started lobby

`POST /game/from-lobby` accepts the guest bearer credential and body:

```json
{"game_key":"island-battles","lobby_id":"<started-lobby-uuid>"}
```

The active host receives `game_id`, `lobby_id` and a stable team UUID/order/color mapping after the automatic simulation completes. Replays return the same game after reauthorizing the caller. The binding transaction creates the game and all mappings together; simulation and saved replay are committed together in a subsequent transaction.

`lobbyadapter` selects the Island Battles game key, connects game persistence to the shared binding service, and registers the game endpoint. It uses types directly from `models`, without aliases. Authorization and HTTP handling are delegated to the [shared game-binding helpers](../lobby/README.md#binding-a-game-to-a-started-lobby). Island Battles persistence lives in `dal/lobby_binding.go`; its only translation is from generic team UUID/order to simulation colors 1..N, with zero reserved for neutral.

## Schema and recovery

Startup initializes the nine generic lobby tables, current game tables, and game-team bindings for a fresh schema. It does not import retired connection records or create compatibility data. New games use the nullable unique `lobby_id` link and a JSONB `replay` column. Creating or joining a lobby does not create a game; starting an Island Battles lobby does.

The outbox retries publication after Centrifugo failure. Wire events contain only invalidation identifiers/version, never roster details. Subscribe and refresh proxies authorize current membership with a server-only shared header. Clients subscribe before fetching a snapshot and reconcile periodically, so bounded history is an optimization rather than durable state storage. Existing unrelated game command authorization has not been redesigned in this extraction.

## Verification

```sh
go test ./... ./lobby/...
go vet ./... ./lobby/...
go test -race ./lobby/... ./backend/lobbyadapter/... ./backend/dal/...
./scripts/test-lobby-acceptance.sh
```

Database tests require a Docker-compatible runtime and use disposable PostgreSQL. The live runner supports Linux Docker or Podman, creates unique test containers, and preserves failure logs. It never uses the configured developer database.
