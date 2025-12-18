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
