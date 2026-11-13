#!/usr/bin/env bash
# Exercise the shared lobby suite against the Island Battles host application.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${GOCACHE:-/tmp/lobby-go-cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/lobby-uv-cache}"
# The runner passes its environment to the disposable backend, and the host
# suite sends this key to create default maps.
export ISLAND_BATTLES_ADMIN_API_KEY=acceptance-admin-api-key
# Signs the submit tokens of turn deliveries, which the host suite's localhost
# bot submits with.
export ISLAND_BATTLES_SUBMIT_TOKEN_SECRET=acceptance-submit-token-secret
# Signs the lobby's calls of Island Battles' hooks, and Island Battles' calls
# of the lobby's game API: one secret by its two names, the lobby's and the
# game's. The runner starts the lobby (lobby/cmd/lobby) and the backend as
# separate processes, the backend with ISLAND_BATTLES_LOBBY_URL, and the lobby
# with the LOBBY_GAMES that backend/cmd/lobby-games prints for the backend's
# address. Clients reach both through one front URL.
export ISLAND_BATTLES_LOBBY_HOOK_SECRET=acceptance-lobby-hook-secret-of-32-bytes
export LOBBY_GAME_SECRET_ISLAND_BATTLES="$ISLAND_BATTLES_LOBBY_HOOK_SECRET"
# Island Battles players choose a bot before getting ready, which the generic
# suite knows nothing about, so it plays lobbies of the lobby's mock game,
# which the runner serves without its rules (--mock-game) and adds to the
# lobby's LOBBY_GAMES. The host suite in integration/island_battles/ covers
# Island Battles lobbies.
export LOBBY_TEST_GAME_KEY=mock-game
# The host suite imports both Python clients and their locked dependencies.
exec uv run --project "$repository_root/clients/island-battles-python" --locked --group integration \
  python "$repository_root/lobby/integration/run.py" \
  --server-root "$repository_root" --server-package ./backend --migrate-package ./backend/cmd/migrate \
  --lobby-games-command "go run ./backend/cmd/lobby-games" \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/integration/island_battles" --mock-game --pytest-timeout 600
