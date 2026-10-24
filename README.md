# Island Battles Go

## Project Overview
`island-battles-go` is a monorepo containing the backend and a reference client for "Island Battles", a real-time multiplayer game. The architecture utilizes a language-agnostic backend that leverages **PostgreSQL** for persistent state and **Centrifugo** for real-time WebSocket communication.

### Key Technologies
*   **Language:** Go 1.25
*   **API Framework:** [Huma v2](https://github.com/danielgtaylor/huma) with [Chi](https://github.com/go-chi/chi) router.
*   **Real-time Messaging:** [Centrifugo](https://centrifugal.dev/) (v6).
*   **Database:** PostgreSQL (v18-alpine) with [GORM](https://gorm.io/).
*   **Clients:** Python lobby and gameplay SDKs generated from checked-in OpenAPI documents, with asyncio Centrifugo runtimes, a language-agnostic client conformance suite, and a Go connection example.

## Project Structure

- `lobby/`: Independent generic lobby component: Go module, clients, API artifacts, transport configuration, docs, and shared test tooling. See [its README](lobby/README.md).
- `backend/`: Island Battles game logic and wiring to the generic lobby module. `backend/api/openapi.json` is the player API that gameplay clients are generated from.
- `clients/island-battles-python/`: The Python gameplay SDK. Its models are generated; only the turn runtime and model utilities are hand-written.
- `clients/conformance/`: Language-agnostic tests that verify any gameplay client through a JSON Lines adapter.
- `integration/island_battles/`: Host-specific lobby/game binding acceptance checks.
- `scripts/test-lobby-acceptance.sh`: Runs the shared acceptance suite against the Island Battles host, including its game-specific checks.
- `go.work`: Connects the host and lobby Go modules for local development.

## Development Setup

Requires Go 1.25+ and either Docker with Compose or Podman on Linux. Python 3.14+ with uv is only needed for the Python client and acceptance tests.

```sh
./scripts/dev.sh
```

The script creates `.env` from `.env.example` if needed, exports it, starts the dependencies, waits for PostgreSQL, and runs the backend. Open **http://localhost:8888/lobby/ui/** in two fresh tabs to test the lobby with two players. No frontend build is required.

PostgreSQL listens on port 5432, Centrifugo on 8000, and the backend on 8888. The native Podman path uses these local defaults, stores database data in the `island-battles-dev-postgres` volume, and disables the Centrifugo admin UI. `/healthz` reports HTTP readiness; Huma serves API documentation at `/docs`.

Ctrl+C stops the backend. Dependencies remain running for the next launch. Stop them with `docker compose stop` or, for native Podman, `podman stop island-battles-dev-db island-battles-dev-centrifugo`.

Use the [Python lobby client walkthrough](lobby/clients/lobby-python/README.md) for two-player team selection, the [Island Battles Python client](clients/island-battles-python/README.md) for gameplay turns, or the [Go example](lobby/clients/README.md). Backend settings and the game endpoints are documented in [backend/README.md](backend/README.md). The implementation contract is recorded in the [Issue #6 task plan](docs/tasks/issue-6-player-generation-commands.md).

## Architecture Notes

The [multiplayer architecture decision](lobby/docs/multiplayer-architecture.md) explains the design, and the [task pack](lobby/docs/tasks/generic-lobby/README.md) records contracts, edge cases and agent assignments. Guest credentials identify players; opening a socket does not create or join a room. Generic lobby HTTP commands commit their state and durable invalidations together. Island Battles generation notifications use a separate durable game event queue. Centrifugo reconnects and recovers transport history; clients reconcile authorized HTTP snapshots after subscribing and periodically.

Teams and readiness belong to the generic lobby. Starting freezes a roster. The Island Battles adapter initializes generation one with a castle and 30 soldiers for each occupied team. Every player in that frozen roster registers commands and completes their turn; the game advances once all roster players have completed. Disconnected players remain part of the turn barrier and must reconnect to submit or complete their turn. The generic lobby has no dependency on the game simulation.

## Commands

*   **Lint/Verify (Go):** `go vet ./... ./lobby/...`
*   **Format:** `go fmt ./... ./lobby/...`
*   **Test (disposable PostgreSQL containers):** `go test ./... ./lobby/...`
*   **Check OpenAPI:** `go run ./lobby/cmd/export-lobby-openapi --output lobby/api/openapi.json --events-output lobby/api/events.json --check` and `go run ./backend/cmd/export-openapi --output backend/api/openapi.json --check`
*   **Check generated gameplay models:** `uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py --check`
*   **Python unit tests:** `uv run --project lobby/clients/lobby-python --locked pytest lobby/clients/lobby-python/tests`
*   **Gameplay client conformance (offline):** `uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py`
*   **Gameplay client conformance (live, Linux Docker or Podman):** `./scripts/test-client-conformance.sh ["<adapter command>"]`
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

To check the lobby UI, the demo game or the replay in a real browser through Playwright, follow the [browser-testing skill](.agents/skills/browser-testing/SKILL.md).

## Code philosophies

1. ALWAYS use go structs when performing queries, never use strings for queries (DON'T DO "game_id = ?"), and same for OrderBy,
   etc.
2. If the user deletes a function that an agent has added, the agent SHOULD NOT add the function back in a later message.
3. use verbose variable names, no abbreviations.
4. when changing any function, make sure to update the summaries in the huma routes if necessary.
5. Prefer `Limit(1).Find(&slice)` over `First(&struct)` to gracefully handle non-existent records without checking for `gorm.ErrRecordNotFound`.
6. Prefer clean breaking changes to compatibility code. Change the API, events and schema directly and have users upgrade their clients; do not add aliases, fallbacks or data migrations for old clients or old game data. Lobby data and map templates are the exception: they are never reset, so change their schema only through versioned migrations that keep existing rows. See the [breaking-changes skill](.agents/skills/breaking-changes/SKILL.md).
