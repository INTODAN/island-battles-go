#!/usr/bin/env bash
# Zip the bot and its locked dependencies for Linux arm64, for the server's runners.
# Writes dist/island-battles-bot.zip. Extra arguments go to `island-battles-bot package`
# (for example --output or --entry). It needs network access to download the wheels.
set -euo pipefail
cd "$(dirname "$0")/.."
exec uv run island-battles-bot package --entry bot:play "$@"
