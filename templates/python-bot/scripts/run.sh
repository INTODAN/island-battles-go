#!/usr/bin/env bash
# Play one seat of a started lobby without a browser, until the game ends.
# Usage: scripts/run.sh [TOKEN LOBBY_ID]
# or set ISLAND_BATTLES_TOKEN and ISLAND_BATTLES_LOBBY_ID. The lobby and the
# game are separate sites: set ISLAND_BATTLES_GAME_URL and
# ISLAND_BATTLES_LOBBY_URL, or ISLAND_BATTLES_BASE_URL for one server that
# serves both. The log goes to stdout and is appended to bot.log.
set -euo pipefail
cd "$(dirname "$0")/.."
token="${1:-${ISLAND_BATTLES_TOKEN:-}}"
lobby_id="${2:-${ISLAND_BATTLES_LOBBY_ID:-}}"
if [ -n "${ISLAND_BATTLES_BASE_URL:-}" ]; then
  urls=(--base-url "$ISLAND_BATTLES_BASE_URL")
elif [ -n "${ISLAND_BATTLES_GAME_URL:-}" ] && [ -n "${ISLAND_BATTLES_LOBBY_URL:-}" ]; then
  urls=(--game-url "$ISLAND_BATTLES_GAME_URL" --lobby-url "$ISLAND_BATTLES_LOBBY_URL")
else
  urls=()
fi
if [ ${#urls[@]} -eq 0 ] || [ -z "$token" ] || [ -z "$lobby_id" ]; then
  echo "usage: scripts/run.sh TOKEN LOBBY_ID (or set ISLAND_BATTLES_TOKEN, ISLAND_BATTLES_LOBBY_ID), with" >&2
  echo "ISLAND_BATTLES_GAME_URL and ISLAND_BATTLES_LOBBY_URL set, or ISLAND_BATTLES_BASE_URL for one server" >&2
  exit 2
fi
exec uv run island-battles-bot run bot:play \
  "${urls[@]}" --token="$token" --lobby-id "$lobby_id" --log-file bot.log
