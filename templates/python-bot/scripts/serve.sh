#!/usr/bin/env bash
# Serve the bot on http://127.0.0.1:41414 for the lobby page's "localhost bot".
# Extra arguments go to `island-battles-bot serve` (for example --port,
# --allow-origin or --allow-submit-host).
# Saving a .py file reloads the bot by itself.
set -euo pipefail
cd "$(dirname "$0")/.."
exec uv run island-battles-bot serve bot:play "$@"
