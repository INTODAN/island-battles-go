# Island Battles Go

## Project Overview
`island-battles-go` is a monorepo containing the backend and a reference client for "Island Battles", a real-time multiplayer game. The architecture utilizes a language-agnostic backend that leverages **PostgreSQL** for persistent state and **Centrifugo** for real-time WebSocket communication.

### Key Technologies
*   **Language:** Go 1.25
*   **API Framework:** [Huma v2](https://github.com/danielgtaylor/huma) with [Chi](https://github.com/go-chi/chi) router.
*   **Real-time Messaging:** [Centrifugo](https://centrifugal.dev/) (v6).
*   **Database:** PostgreSQL (v18-alpine) with [GORM](https://gorm.io/).
*   **Client:** Go-based CLI client using `centrifuge-go`.

## Project Structure

*   **`backend/`**: The core game server API.
    *   `main.go`: Entry point, initializes DB, Centrifugo client, and HTTP server (port 8888).
    *   `routes/`: Defines HTTP API endpoints (`/ping`, `/connect`, `/create_map_template`).
    *   `handlers/`: Implementation of route logic.
    *   `dal/`: Data Access Layer (database interactions).
    *   `models/`: GORM data models.
*   **`client/`**: A reference Go CLI client.
    *   Connects to Centrifugo via WebSocket (`ws://localhost:8000/connection/websocket`).
    *   Demonstrates subscribing to channels and making RPC calls (e.g., `proxy_ping`).
*   **`centrifugo/`**: Configuration for the Centrifugo server.
*   **`docker-compose.yml`**: Orchestrates the infrastructure services (Postgres and Centrifugo).

## Development Setup

### Prerequisites
*   Go 1.25+
*   Docker & Docker Compose

### Running the Project

1.  **Start Infrastructure:**
    Launch PostgreSQL and Centrifugo.
    ```bash
    docker compose up -d db centrifugo
    ```
    *   **Postgres:** Port `5432` (User/Pass/DB: `islandBattles`)
    *   **Centrifugo:** Port `8000` (Dashboard/Client API)

2.  **Run Backend:**
    The backend listens on port `8888` and acts as the proxy/auth for Centrifugo.
    ```bash
    cd backend
    go run main.go
    ```

    Or for hot relaod:
    ```bash
    air
    ```

3.  **Run Client:**
    The client connects to Centrifugo, which proxies requests to the backend.
    ```bash
    cd client
    go run main.go
    ```

## Architecture Notes

*   **Centrifugo Proxy:** The backend is configured as a proxy for Centrifugo. Client events (like connection attempts or RPC calls) are forwarded from Centrifugo to the backend's HTTP endpoints (`/connect`, `/ping`).
*   **Environment Variables:**
    *   The `docker-compose.yml` configures Centrifugo to proxy requests to `http://host.docker.internal:8888`.
    *   Ensure the backend is running locally on port `8888` for this connection to succeed.

## Commands

*   **Lint/Verify (Go):** `go vet ./...`
*   **Format:** `go fmt ./...`
*   **Test:** `go test ./...`

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
3. Avoid using the make function.
4. use verbose variable names, no abbreviations.
5. when changing any function, make sure to update the summaries in the huma routes if necessary.
6. Prefer `Limit(1).Find(&slice)` over `First(&struct)` to gracefully handle non-existent records without checking for `gorm.ErrRecordNotFound`.
