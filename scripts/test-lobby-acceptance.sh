#!/usr/bin/env bash
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
export GOCACHE="${GOCACHE:-/tmp/island-lobby-go-cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/island-lobby-uv-cache}"
exec uv run --project clients/lobby-python --locked python integration/lobby/run.py
