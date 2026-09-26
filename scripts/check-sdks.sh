#!/usr/bin/env bash
# Reproducible contracts, generated clients, and every supported SDK's tests.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
export GOCACHE="${GOCACHE:-/tmp/island-lobby-go-cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/island-lobby-uv-cache}"

go test ./cmd/export-game-openapi ./backend/models ./lobby/httpapi ./lobby/cmd/export-lobby-openapi
go run ./cmd/export-game-openapi --output api/game/openapi.json --events-output api/game/events.json --check
go run ./lobby/cmd/export-lobby-openapi --output lobby/api/openapi.json --events-output lobby/api/events.json --check

# Both Python suites include fresh generation/drift checks. Game tests also run
# the shared JSON scenarios and validate their format and requirement references.
uv run --project lobby/clients/lobby-python --locked pytest -q --disable-warnings lobby/clients/lobby-python/tests
uv run --project clients/island-battles-python --locked pytest -q --disable-warnings clients/island-battles-python/tests
(
  cd clients/island-battles-python
  uv run --locked ruff check .
  uv run --locked ruff format --check .
  uv run --locked ty check --error-on-warning
)

npm --prefix clients/island-battles-typescript run generate:check
npm --prefix clients/island-battles-typescript run check
npm --prefix clients/island-battles-typescript run typecheck
npm --prefix clients/island-battles-typescript test
