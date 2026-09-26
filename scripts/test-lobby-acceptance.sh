#!/usr/bin/env bash
# Exercise the shared lobby suite against the Island Battles host application.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${GOCACHE:-/tmp/lobby-go-cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/lobby-uv-cache}"
# The host suite exercises Python and TypeScript players together. Install the
# pinned Node dependencies with npm ci before invoking this acceptance runner.
npm --prefix "$repository_root/clients/island-battles-typescript" run build
# The host suite imports both Python clients and their locked dependencies.
exec uv run --project "$repository_root/clients/island-battles-python" --locked --group integration \
  python "$repository_root/lobby/integration/run.py" \
  --server-root "$repository_root" --server-package ./backend \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/integration/island_battles"
