#!/usr/bin/env bash
# Start local dependencies, then run the lobby and the backend as separate
# processes on separate origins, as in production: the lobby
# (lobby/cmd/lobby) on :8890, and the backend on ISLAND_BATTLES_HTTP_ADDR,
# which reaches the lobby at ISLAND_BATTLES_LOBBY_URL. The lobby page,
# http://localhost:8890/lobby/ui/, loads Island Battles' screens from the
# backend; the 3D client's dev server (clients/island-battles-web: bun run
# dev), http://localhost:3000/, calls the lobby on :8890.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

if [[ ! -f .env ]]; then
  cp .env.example .env
fi
set -a
source .env
set +a

missing=()
for name in LOBBY_DATABASE_URL LOBBY_PUBLIC_URL LOBBY_CENTRIFUGO_API_URL LOBBY_CENTRIFUGO_API_KEY LOBBY_CENTRIFUGO_PUBLIC_URL \
  LOBBY_CONNECTION_TOKEN_HMAC_SECRET LOBBY_PROXY_SECRET LOBBY_ALLOWED_ORIGINS ISLAND_BATTLES_LOBBY_URL ISLAND_BATTLES_LOBBY_ORIGINS; do
  if [[ -z "${!name:-}" ]]; then
    missing+=("$name")
  fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "Your .env lacks ${missing[*]}, which the lobby and the backend on their own origins need. Copy them from .env.example." >&2
  exit 1
fi

if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
  docker compose up -d --wait db centrifugo
elif command -v podman >/dev/null && [[ "$(uname -s)" == Linux ]]; then
  # Native Podman needs no Compose provider. Match the local Compose database
  # settings and retain its data across script/backend restarts.
  if podman container exists island-battles-dev-db; then
    podman start island-battles-dev-db >/dev/null
  else
    podman run -d --name island-battles-dev-db \
      -e POSTGRES_USER=islandBattles -e POSTGRES_PASSWORD=islandBattles \
      -e POSTGRES_DB=islandBattles \
      -p 127.0.0.1:5432:5432 \
      -v island-battles-dev-postgres:/var/lib/postgresql \
      docker.io/library/postgres:18-alpine >/dev/null
  fi

  database_ready=false
  for attempt in {1..60}; do
    if podman exec island-battles-dev-db pg_isready -h 127.0.0.1 -U islandBattles -d islandBattles >/dev/null 2>&1; then
      database_ready=true
      break
    fi
    sleep 1
  done
  if [[ "$database_ready" != true ]]; then
    echo "PostgreSQL did not become ready. Check: podman logs island-battles-dev-db" >&2
    exit 1
  fi

  # Recreate only this stateless development service to pick up .env changes.
  # Centrifugo is the lobby's: it takes the lobby's secrets and sends its
  # proxy calls to the lobby.
  if podman container exists island-battles-dev-centrifugo; then
    podman rm -f island-battles-dev-centrifugo >/dev/null
  fi
  podman run -d --name island-battles-dev-centrifugo \
    --network host --add-host host.docker.internal:127.0.0.1 \
    -v "$repository_root/lobby/centrifugo:/centrifugo:ro,Z" \
    -e CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY="$LOBBY_CONNECTION_TOKEN_HMAC_SECRET" \
    -e CENTRIFUGO_HTTP_API_KEY="$LOBBY_CENTRIFUGO_API_KEY" \
    -e CENTRIFUGO_VAR_LOBBY_PROXY_SECRET="$LOBBY_PROXY_SECRET" \
    -e CENTRIFUGO_ADMIN_ENABLED=false \
    -e CENTRIFUGO_CLIENT_ALLOWED_ORIGINS="${ISLAND_BATTLES_CENTRIFUGO_ALLOWED_ORIGINS:-http://localhost:3000 http://127.0.0.1:3000 http://localhost:8890 http://127.0.0.1:8890 tauri://localhost http://tauri.localhost}" \
    docker.io/centrifugo/centrifugo:v6.9.6 centrifugo -c /centrifugo/centrifugo-config.toml >/dev/null
else
  echo "Install Docker with Compose, or Podman on Linux, to run local dependencies." >&2
  exit 1
fi

# The backend only connects, so bring the schema up to date first, as a
# deploy does. It migrates the lobby's tables too, which share its database
# for now.
go run ./backend/cmd/migrate

# The lobby calls Island Battles' hooks on the backend's port, and its page
# loads Island Battles' screens from there; it lists the games of LOBBY_GAMES
# in .env too.
backend_origin="http://localhost:${ISLAND_BATTLES_HTTP_ADDR##*:}"
LOBBY_GAMES="$(go run ./backend/cmd/lobby-games "$backend_origin")"
export LOBBY_GAMES

go build -o "${TMPDIR:-/tmp}/island-battles-dev-lobby" ./lobby/cmd/lobby
"${TMPDIR:-/tmp}/island-battles-dev-lobby" &
lobby_pid=$!
trap 'kill "$lobby_pid" 2>/dev/null || true' EXIT
for attempt in {1..60}; do
  if curl -fsS -o /dev/null "$ISLAND_BATTLES_LOBBY_URL/healthz" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$lobby_pid" 2>/dev/null; then
    echo "The lobby stopped; see its output above." >&2
    exit 1
  fi
  sleep 0.5
done

echo "Starting the backend on $ISLAND_BATTLES_HTTP_ADDR and the lobby on ${LOBBY_HTTP_ADDR:-:8890}."
echo "Open the lobby page at $ISLAND_BATTLES_LOBBY_URL/lobby/ui/. For the 3D client, run bun run dev in clients/island-battles-web and open http://localhost:3000/."
go run ./backend
