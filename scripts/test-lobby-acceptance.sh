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
# Island Battles players choose a bot before getting ready, which the generic
# suite knows nothing about, so it plays quiz-demo lobbies. The host suite in
# integration/island_battles/ covers Island Battles lobbies.
export LOBBY_TEST_GAME_KEY=quiz-demo
# The host suite imports both Python clients and their locked dependencies.
exec uv run --project "$repository_root/clients/island-battles-python" --locked --group integration \
  python "$repository_root/lobby/integration/run.py" \
  --server-root "$repository_root" --server-package ./backend --migrate-package ./backend/cmd/migrate \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/integration/island_battles"
