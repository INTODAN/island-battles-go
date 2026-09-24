#!/usr/bin/env bash
# Start local dependencies and run the backend, including the lobby browser UI.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

if [[ ! -f .env ]]; then
  cp .env.example .env
fi
set -a
source .env
set +a

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
  if podman container exists island-battles-dev-centrifugo; then
    podman rm -f island-battles-dev-centrifugo >/dev/null
  fi
  podman run -d --name island-battles-dev-centrifugo \
    --network host --add-host host.docker.internal:127.0.0.1 \
    -v "$repository_root/lobby/centrifugo:/centrifugo:ro,Z" \
    -e CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY="$ISLAND_BATTLES_CONNECTION_TOKEN_HMAC_SECRET" \
    -e CENTRIFUGO_HTTP_API_KEY="$ISLAND_BATTLES_CENTRIFUGO_API_KEY" \
    -e CENTRIFUGO_VAR_LOBBY_PROXY_SECRET="$ISLAND_BATTLES_LOBBY_PROXY_SECRET" \
    -e CENTRIFUGO_ADMIN_ENABLED=false \
    -e CENTRIFUGO_CLIENT_ALLOWED_ORIGINS="${ISLAND_BATTLES_CENTRIFUGO_ALLOWED_ORIGINS:-http://localhost:8888 http://127.0.0.1:8888}" \
    docker.io/centrifugo/centrifugo:v6.9.6 centrifugo -c /centrifugo/centrifugo-config.toml >/dev/null
else
  echo "Install Docker with Compose, or Podman on Linux, to run local dependencies." >&2
  exit 1
fi

echo "Starting backend on $ISLAND_BATTLES_HTTP_ADDR; open /lobby/ui/ in your browser."
exec go run ./backend
