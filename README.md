# Island Battles Go

## Project Overview
`island-battles-go` is a monorepo containing the backend and a reference client for "Island Battles", a real-time multiplayer game. The architecture utilizes a language-agnostic backend that leverages **PostgreSQL** for persistent state and **Centrifugo** for real-time WebSocket communication.

### Key Technologies
*   **Language:** Go 1.25
*   **API Framework:** [Huma v2](https://github.com/danielgtaylor/huma) with [Chi](https://github.com/go-chi/chi) router.
*   **Real-time Messaging:** [Centrifugo](https://centrifugal.dev/) (v6).
*   **Database:** PostgreSQL (v18-alpine) with [GORM](https://gorm.io/).
*   **Clients:** A Go SDK for writing bots; Python lobby and gameplay SDKs generated from checked-in OpenAPI documents, with asyncio Centrifugo runtimes, a language-agnostic client conformance suite, and a Go connection example.

## Project Structure

- `lobby/`: Independent generic lobby component: Go module, clients, API artifacts, transport configuration, docs, and shared test tooling. See [its README](lobby/README.md).
- `backend/`: Island Battles game logic and wiring to the generic lobby module. `backend/api/openapi.json` is the player API that gameplay clients are generated from.
- `clients/island-battles-python/`: The Python gameplay SDK and the `island-battles-bot` command (serve, check and run a bot function). Its models are generated; only the turn runtime, the bot helpers and model utilities are hand-written.
- `clients/island-battles-go/`: The Go SDK for writing bots, its own Go module (`github.com/INTODAN/island-battles-go/clients/island-battles-go`): a bot is a function, and `islandbattles.Main` gives its program `serve`, `check`, `fixture` and `run`, which plays a seat without a browser. Its `examples/` hold the three default bots, which the backend plays from there. See [its README](clients/island-battles-go/README.md).
- `templates/python-bot/`: A starter project for a Python bot (uv, ruff, ty, pytest, scripts and skills for coding agents) that depends on the published SDK. Copy it out to start a bot. See [its README](templates/python-bot/README.md).
- `clients/island-battles-web/`: The 3D browser client (TypeScript, Babylon.js, Bun and Biome). Currently a scaffold. See [its README](clients/island-battles-web/README.md).
- `clients/conformance/`: Language-agnostic tests that verify any gameplay client through a JSON Lines adapter.
- `integration/island_battles/`: Host-specific lobby/game binding acceptance checks.
- `scripts/test-lobby-acceptance.sh`: Runs the shared acceptance suite against the Island Battles host, including its game-specific checks.
- `go.work`: Connects the host, lobby and Go SDK modules for local development.
- `deploy/`: Deployment to AWS: the Terraform configuration for the AWS resources, the Ansible setup for the Postgres and Centrifugo VM, and the backend's Lambda deploy. See [deploy/README.md](deploy/README.md).

## Development Setup

Requires Go 1.25+ and either Docker with Compose or Podman on Linux. Python 3.14+ with uv is only needed for the Python client and acceptance tests.

```sh
./scripts/dev.sh
```

The script creates `.env` from `.env.example` if needed, exports it, starts the dependencies, waits for PostgreSQL, runs the migrate command (`go run ./backend/cmd/migrate`), and runs the lobby and the backend as separate processes: the lobby ([`lobby/cmd/lobby`](lobby/README.md#running-the-lobby)) on 8890, and the backend on 8888, which reaches the lobby at `ISLAND_BATTLES_LOBBY_URL`. The backend never changes the schema itself, locally or on [AWS Lambda](backend/README.md#hosts-and-deploys). In production the backend still runs the lobby in its own process, until the lobby is deployed on its own. An `.env` made before the lobby ran on its own lacks the `LOBBY_*` settings and `ISLAND_BATTLES_LOBBY_URL`; the script says so, and they can be copied from `.env.example`.

Both UIs live on one origin through the 3D client's dev server: run `bun run dev` in [`clients/island-battles-web`](clients/island-battles-web/README.md), then open **http://localhost:3000/lobby/ui/** in two fresh tabs to test the lobby page with two players, or **http://localhost:3000/** for the 3D client. The dev server sends `/lobby/` to the lobby and the game's paths to the backend.

PostgreSQL listens on port 5432, Centrifugo on 8000, the lobby on 8890, the backend on 8888 and the 3D client's dev server on 3000. The native Podman path uses these local defaults, stores database data in the `island-battles-dev-postgres` volume, and disables the Centrifugo admin UI. `/healthz` reports HTTP readiness; Huma serves API documentation at `/docs`. Creating a default map with `POST /map/create` is for admins: send `ISLAND_BATTLES_ADMIN_API_KEY` from `.env` in the `X-Admin-API-Key` header (see [map files](backend/README.md#map-files)). The local backend also serves the development page `GET /game/{id}/graph`, which the public Lambda host leaves out. To ask Claude Code or another MCP client about lobbies, players and games, register the read-only [admin MCP server](backend/README.md#admin-mcp-server).

Ctrl+C stops the backend and the lobby. Dependencies remain running for the next launch. Stop them with `docker compose stop` or, for native Podman, `podman stop island-battles-dev-db island-battles-dev-centrifugo`.

Use the [Python lobby client walkthrough](lobby/clients/lobby-python/README.md) for two-player team selection, the [Island Battles Python client](clients/island-battles-python/README.md) or the [Go SDK](clients/island-battles-go/README.md) for writing a bot, or the [Go example](lobby/clients/README.md). Backend settings and the game endpoints are documented in [backend/README.md](backend/README.md). The implementation contract is recorded in the [Issue #6 task plan](docs/tasks/issue-6-player-generation-commands.md).

## Architecture Notes

The [multiplayer architecture decision](lobby/docs/multiplayer-architecture.md) explains the design, and the [task pack](lobby/docs/tasks/generic-lobby/README.md) records contracts, edge cases and agent assignments. Guest credentials identify players; opening a socket does not create or join a room. PostgreSQL holds only state, and the backend runs no work between requests. A request that commits a lobby change or completes a turn pushes the result straight to Centrifugo before it responds, retrying briefly and logging an error if Centrifugo stays unreachable. Every lobby request, including the 15-second heartbeat, and every Centrifugo subscription refresh applies due lease and grace expiry. Centrifugo keeps a short history on lobby and player channels for reconnecting clients; clients also reconcile authorized HTTP snapshots after subscribing, which recovers any push that failed.

Teams and readiness belong to the generic lobby. Starting freezes a roster. The Island Battles adapter initializes generation one with a castle and 30 soldiers for each team. Every player in that frozen roster registers commands and completes their turn, or the backend plays their [default bot](backend/README.md#default-bots) as soon as each generation is created; the game advances once all roster players have completed. Disconnected players remain part of the turn barrier and must reconnect to submit or complete their turn before the turn deadline the host set, or their team loses. The generic lobby has no dependency on the game simulation.

## Commands

*   **Lint/Verify (Go):** `go vet ./... ./lobby/... ./clients/island-battles-go/...` and `golangci-lint run --timeout=5m ./... ./lobby/... ./clients/island-battles-go/...`
*   **Format:** `go fmt ./... && go -C lobby fmt ./... && go -C clients/island-battles-go fmt ./...` (`go fmt` doesn't read `go.work`)
*   **Test (disposable PostgreSQL containers):** `go test ./... ./lobby/... ./clients/island-battles-go/...`. On rootless Podman, enable the Podman socket and disable Ryuk first; see [database tests on rootless Podman](backend/README.md#database-tests-on-rootless-podman).
*   **Check OpenAPI:** `go run ./lobby/cmd/export-lobby-openapi --output lobby/api/openapi.json --events-output lobby/api/events.json --game-api-output lobby/api/game-api.openapi.json --check` and `go run ./backend/cmd/export-openapi --output backend/api/openapi.json --check`
*   **Check generated gameplay models:** `uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py --check`
*   **Regenerate the web client's API types:** `bun run generate` in `clients/island-battles-web/` (`bun run generate --check` verifies they are current; `web-client.yml` runs it on any change to `lobby/api/` or `backend/api/`)
*   **Python unit tests:** `uv run --project lobby/clients/lobby-python --locked pytest lobby/clients/lobby-python/tests`
*   **Python bot client tests:** `uv run --project clients/island-battles-python --locked pytest clients/island-battles-python/tests`
*   **Python bot runner tests:** `uv run --no-project --python 3.14 python -I -m unittest discover -s backend/botrunner/python` (the end-to-end test needs PyPI; see [Bot runners](backend/README.md#bot-runners))
*   **Gameplay client conformance (offline):** `uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py`
*   **Go SDK conformance (offline):** `ISLAND_BATTLES_CLIENT_COMMAND="go run ./clients/island-battles-go/conformance" uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py`; live: `./scripts/test-client-conformance.sh "go -C $PWD run ./clients/island-battles-go/conformance"`
*   **Gameplay client conformance (live, Linux Docker or Podman):** `./scripts/test-client-conformance.sh ["<adapter command>"]`
*   **Live acceptance (Linux Docker or Podman):** `./scripts/test-lobby-acceptance.sh`
*   **Web client** (in `clients/island-battles-web/`, needs [Bun](https://bun.sh/)): `bun install`, then `bun run dev` (dev server on `:3000`, proxying the backend from `./scripts/dev.sh`), `bun run generate` (regenerate the TypeScript API types from the OpenAPI files and `events.json` after an API change), `bun run check` (Biome, `generate --check`, `tsc`, `bun test`) and `bun run build`

### Checks on every pull request

[`.github/workflows/checks.yml`](.github/workflows/checks.yml) runs these checks on every pull request to `main`, as five parallel jobs. Each shows as its own check on the pull request:

| Job | Runs |
|---|---|
| Go | `go vet`, `golangci-lint` (pinned to the version in the workflow), a `gofmt -l` check of the three modules (the host, `lobby/` and the Go SDK in `clients/island-battles-go/`), and `go test` with Testcontainers on the runner's Docker |
| API contract | both OpenAPI `--check` commands and the generated gameplay models' `--check` |
| Python | the lobby client's unit tests, the bot client's tests, the Python bot runner's tests and the offline gameplay client conformance tests, with `uv --locked`, for the Python client and the Go SDK's adapter, and the [Python bot template](templates/python-bot/README.md)'s `scripts/check.sh` against the locked PyPI SDK and against `clients/island-battles-python` |
| JS | `node --test` for the tests in `backend/demo`, `backend/demoviewer`, `backend/mapbuilder` and `lobby/frontend` |
| Deploy | `python3 -I -m unittest discover -s deploy -p 'test_*.py'` |

The workflow leaves out the live acceptance and live client conformance scripts, which start the whole stack. It also has a `workflow_call` trigger, so the release workflow can run the same checks with `uses: ./.github/workflows/checks.yml` before it deploys. Go comes from `go.mod`; when you change the golangci-lint version in the workflow, change the one you run locally to match.

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

## Licence

[MIT](LICENSE).
