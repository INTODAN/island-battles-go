#!/usr/bin/env bash
# Serve the bot on http://127.0.0.1:41414 for the lobby page's "localhost bot".
# Extra arguments go to `island-battles-bot serve` (for example --port,
# --allow-origin or --allow-submit-host).
# Saving a .py file reloads the bot by itself.
# The lobby page is on its own site, https://lobby.elidan.org (#232), whose
# origin island-battles-python 1.0.0 and older don't allow by default.
set -euo pipefail
cd "$(dirname "$0")/.."
exec uv run island-battles-bot serve bot:play --allow-origin https://lobby.elidan.org "$@"
