#!/usr/bin/env bash
# The league's checks: format, lint, types and tests of the tools and of every
# bot. A bot that fails them doesn't play.
set -euo pipefail
cd "$(dirname "$0")/.."
uv run --locked ruff format --check
uv run --locked ruff check
uv run --locked ty check
uv run --locked pytest -q
