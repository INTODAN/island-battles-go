#!/usr/bin/env bash
# The one command to run before playing: format, lint, types, tests, then the SDK check.
set -euo pipefail
cd "$(dirname "$0")/.."
uv run ruff format --check
uv run ruff check
uv run ty check
uv run pytest
uv run island-battles-bot check bot:play
