# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and an outbox; Centrifugo handles WebSocket transport and history recovery. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). All seven backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret and internal proxy secret. Startup rejects missing values. Public clients receive only their guest credential and short-lived connection JWT.

Use `go run ./backend` from the repository root. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`api/lobby/`](../api/lobby/README.md).

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`. An existing volume from an older PostgreSQL layout needs its own database upgrade or backup/restore; the lobby schema migration does not move PostgreSQL data files.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. Registered example games are `island-battles` and `quiz-demo`. Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. A five-second sweeper and command-time normalization apply persisted deadlines after restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

The host is the earliest active joiner. Team changes clear readiness. At least two members are required to start; all active members must choose a team, become ready and have a live lease. Starting freezes the roster; only roster players can subsequently rejoin that started room. An empty waiting room closes. Expired guest credentials require an explicit new session and do not transfer ownership.

## Bind a started lobby to Island Battles

`POST /game/from-lobby` accepts the guest bearer credential and body:

```json
{"game_key":"island-battles","lobby_id":"<started-lobby-uuid>"}
```

The active host receives `game_id`, `lobby_id` and a stable team UUID/order/color mapping. Replays return the same game after reauthorizing the caller. The transaction creates the game and all mappings together. Generic rooms create no Island Battles rows. Map generation and simulation are separate existing game operations.

## Upgrade and recovery

Startup initializes generic tables, archives legacy connection records, then initializes current game tables and bindings. Legacy players are preserved as JSON with checksums, not converted into authenticated identities. Existing game graphs remain intact. Old `room_name` values remain archived and the legacy column permits nulls; new games use nullable unique `lobby_id`. See [migration procedure and verification](migrations/README.md).

The outbox retries publication after Centrifugo failure. Wire events contain only invalidation identifiers/version, never roster details. Subscribe and refresh proxies authorize current membership with a server-only shared header. Clients subscribe before fetching a snapshot and reconcile periodically, so bounded history is an optimization rather than durable state storage. Existing unrelated game command authorization has not been redesigned in this extraction.

## Verification

```sh
go test ./...
go vet ./...
go test -race ./lobby/... ./backend/lobbyadapter/...
./scripts/test-lobby-acceptance.sh
```

Database tests require a Docker-compatible runtime and use disposable PostgreSQL. The live runner supports Linux Docker or Podman, creates unique test containers, and preserves failure logs. It never uses the configured developer database.
