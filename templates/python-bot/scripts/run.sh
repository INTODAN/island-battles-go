#!/usr/bin/env bash
# Play one seat of a started lobby without a browser, until the game ends.
# Usage: scripts/run.sh [BASE_URL TOKEN LOBBY_ID]
# or set ISLAND_BATTLES_BASE_URL, ISLAND_BATTLES_TOKEN and ISLAND_BATTLES_LOBBY_ID.
# The log goes to stdout and is appended to bot.log.
set -euo pipefail
cd "$(dirname "$0")/.."
base_url="${1:-${ISLAND_BATTLES_BASE_URL:-}}"
token="${2:-${ISLAND_BATTLES_TOKEN:-}}"
lobby_id="${3:-${ISLAND_BATTLES_LOBBY_ID:-}}"
if [ -z "$base_url" ] || [ -z "$token" ] || [ -z "$lobby_id" ]; then
  echo "usage: scripts/run.sh BASE_URL TOKEN LOBBY_ID (or set ISLAND_BATTLES_BASE_URL, ISLAND_BATTLES_TOKEN, ISLAND_BATTLES_LOBBY_ID)" >&2
  exit 2
fi
exec uv run island-battles-bot run bot:play \
  --base-url "$base_url" --token "$token" --lobby-id "$lobby_id" --log-file bot.log
