# Island Battles Go

## Project Overview
`island-battles-go` is a monorepo containing the backend and a reference client for "Island Battles", a real-time multiplayer game. The architecture utilizes a language-agnostic backend that leverages **PostgreSQL** for persistent state and **Centrifugo** for real-time WebSocket communication.

### Key Technologies
*   **Language:** Go 1.25
*   **API Framework:** [Huma v2](https://github.com/danielgtaylor/huma) with [Chi](https://github.com/go-chi/chi) router.
*   **Real-time Messaging:** [Centrifugo](https://centrifugal.dev/) (v6).
*   **Database:** PostgreSQL (v18-alpine) with [GORM](https://gorm.io/).
*   **Clients:** Generated Python HTTP SDK with an asyncio Centrifugo companion, plus a Go connection example.

## Project Structure

- `lobby/`: Generic identities, rooms, teams, memberships, reconnect leases, PostgreSQL persistence, Huma HTTP API and durable event delivery. It has no Island Battles dependency.
- `backend/`: Game logic and an explicit adapter that binds a started lobby to an Island Battles game.
- `clients/lobby-python/`: Generated HTTP SDK, realtime wrapper and interactive CLI.
- `clients/main.go`: Go guest/create/join connection example.
- `api/lobby/`: Deterministically exported Huma OpenAPI and invalidation schema.
- `centrifugo/`: Authorized subscriptions and transport recovery configuration.

## Development Setup

Requires Go 1.25+, Docker with Compose, and Python 3.14+ with uv for the Python client.

```sh
cp .env.example .env
# Review local configuration before starting services.
set -a
. ./.env
set +a
docker compose up -d db centrifugo
go run ./backend
```

PostgreSQL listens on port 5432, Centrifugo on 8000, and the backend on 8888. Compose loads `.env`; the Go process requires the same variables exported into its environment. Centrifugo proxies subscription authorization to the host backend on port 8888. `/healthz` reports HTTP readiness; Huma serves API documentation at `/docs`.

Use the [Python client walkthrough](clients/lobby-python/README.md) for two-player team selection, or the [Go example](clients/README.md). Backend settings and the explicit game-binding endpoint are documented in [backend/README.md](backend/README.md).

## Architecture Notes

The [multiplayer architecture decision](docs/multiplayer-architecture.md) explains the design, and the [task pack](docs/tasks/generic-lobby/README.md) records contracts, edge cases and agent assignments. Guest credentials identify players; opening a socket does not create or join a room. HTTP commands commit state and durable invalidations together. Centrifugo reconnects and recovers transport history; clients reconcile authorized HTTP snapshots after subscribing and periodically.

Teams and readiness belong to the generic lobby. Starting freezes a roster. An explicit `POST /game/from-lobby` binds a started `island-battles` lobby to a game; it does not generate a map or start simulation. Lambda routing, round timers and C#/TypeScript clients remain future work.

## Commands

*   **Lint/Verify (Go):** `go vet ./...`
*   **Format:** `go fmt ./...`
*   **Test (disposable PostgreSQL containers):** `go test ./...`
*   **Check OpenAPI:** `go run ./cmd/export-lobby-openapi --output api/lobby/openapi.json --events-output api/lobby/events.json --check`
*   **Python unit tests:** `uv run --project clients/lobby-python --locked pytest clients/lobby-python/tests`
*   **Live acceptance (Linux Docker or Podman):** `./scripts/test-lobby-acceptance.sh`

## Testing & Visualization

The project uses a specialized methodology for testing complex game state transitions and verifying them visually.

### Game Graph Tests
Tests in `backend/services/game_graph_test.go` simulate game sessions by:
1.  Initializing a transient PostgreSQL instance via **Testcontainers**.
2.  Creating map templates and initializing games.
3.  Migrating the game through multiple generations (steps) using `services.MigrateGeneration`.
4.  Injecting specific commands (e.g., `SendSoldierGroup`) to test edge cases.

### Visual Verification
Since Go tests cannot natively verify the correctness of the interactive JavaScript-based visualization (ECharts), we use an "Export-to-Browser" methodology:

1.  **Run with Export:** Set the `EXPORT_GAME_GRAPH` environment variable to `true`.
    ```bash
    export EXPORT_GAME_GRAPH=true && go test ./backend/services/... -v -run TestGameGraphWithSoldierGroups
    ```
2.  **Locate Output:** The test will log the path to a temporary HTML file (e.g., `/tmp/game_graph_*.html`).
3.  **Inspect:** Open this file in any web browser.
    *   **Visual Check:** Verify that soldier groups are moving correctly, castle amounts update, and nodes do not overlap.
    *   **JavaScript Console:** Check for any ECharts or browser-side errors (e.g., duplicate node IDs, rendering order issues) that the Go test might pass silently.

This loop (Modify Code -> Run Test with Export -> Refresh Browser) is the recommended way to debug visualization logic.

## Code philosophies

1. ALWAYS use go structs when performing queries, never use strings for queries (DON'T DO "game_id = ?"), and same for OrderBy,
   etc.
2. If the user deletes a function that an agent has added, the agent SHOULD NOT add the function back in a later message.
3. use verbose variable names, no abbreviations.
4. when changing any function, make sure to update the summaries in the huma routes if necessary.
5. Prefer `Limit(1).Find(&slice)` over `First(&struct)` to gracefully handle non-existent records without checking for `gorm.ErrRecordNotFound`.
