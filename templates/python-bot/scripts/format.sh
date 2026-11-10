#!/usr/bin/env bash
# Format the code and apply the safe lint fixes.
set -euo pipefail
cd "$(dirname "$0")/.."
uv run ruff format
uv run ruff check --fix
