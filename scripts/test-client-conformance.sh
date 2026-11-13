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
# Signs the lobby's calls of Island Battles' hooks, and Island Battles' calls
# of the lobby's game API: one secret by its two names, the lobby's and the
# game's. The runner starts the lobby (cmd/lobby of github.com/INTODAN/mpl,
# the version go.mod pins) and the backend as
# separate processes, the backend with ISLAND_BATTLES_LOBBY_URL, and the lobby
# with the LOBBY_GAMES that backend/cmd/lobby-games prints for the backend's
# address. They are separate origins: clients get the lobby's URL and the
# game's, and send each route to its own.
export ISLAND_BATTLES_LOBBY_HOOK_SECRET=conformance-lobby-hook-secret-of-32-bytes
export LOBBY_GAME_SECRET_ISLAND_BATTLES="$ISLAND_BATTLES_LOBBY_HOOK_SECRET"
if [[ $# -gt 0 ]]; then
  export ISLAND_BATTLES_CLIENT_COMMAND="$1"
fi
# The lobby's live test runner, integration/run.py, comes with the lobby
# module that go.mod pins. It builds the lobby and runs pytest in the lobby's
# directory, which must be writable, so it runs in a copy of the module.
lobby_root="$("$repository_root/scripts/lobby-module.sh" --copy)"
trap 'rm -rf "$lobby_root"' EXIT
uv run --project "$repository_root/clients/conformance" --locked \
  python "$lobby_root/integration/run.py" \
  --server-root "$repository_root" --server-package ./backend --migrate-package ./backend/cmd/migrate \
  --lobby-games-command "go run ./backend/cmd/lobby-games" \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/clients/conformance/tests" \
  --skip-lobby-suite --pytest-timeout 900
