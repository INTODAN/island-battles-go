#!/usr/bin/env bash
# Verify a player client against the real backend, PostgreSQL and Centrifugo.
# Usage: scripts/test-client-conformance.sh ["<adapter command>"]
# Without a command, the in-repository Python client is tested. See
# clients/conformance/README.md for the adapter protocol.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${GOCACHE:-/tmp/lobby-go-cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/lobby-uv-cache}"
# The backend requires it, though the clients under test submit with their
# player's session.
export ISLAND_BATTLES_SUBMIT_TOKEN_SECRET=conformance-submit-token-secret
if [[ $# -gt 0 ]]; then
  export ISLAND_BATTLES_CLIENT_COMMAND="$1"
fi
exec uv run --project "$repository_root/clients/conformance" --locked \
  python "$repository_root/lobby/integration/run.py" \
  --server-root "$repository_root" --server-package ./backend --migrate-package ./backend/cmd/migrate \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/clients/conformance/tests" \
  --skip-lobby-suite --pytest-timeout 900
