#!/usr/bin/env bash
# Plays real workers through the headless match runner (backend/cmd/headless-match),
# so a change to the Python SDK's worker, the Go SDK's worker, or how the runner
# talks to a worker that breaks the two together fails here:
#
# - python: templates/python-bot's bot, its worker started through this
#   repository's Python SDK (uv run --project clients/island-battles-python);
# - go: clients/island-battles-go/examples/firstbot, built from this checkout and
#   run with its `worker` command.
#
# Each plays default level 3 from both sides on
# backend/cmd/headless-match/maps/crossing.json: four matches in one schedule.
# The script fails when the runner exits with an error, when a match has an
# `error`, or when a seat has a failed turn. Who wins doesn't matter.
#
# Usage: scripts/check-headless-match.sh
#
# Needs Go, uv and jq. It changes nothing in the repository.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "Building the runner and the Go example bot" >&2
go -C "$root" build -o "$work/headless-match" ./backend/cmd/headless-match
go -C "$root" build -o "$work/firstbot" ./clients/island-battles-go/examples/firstbot

# Installs the Python SDK's environment now, so the worker's first start
# doesn't spend the runner's 60 seconds on it.
echo "Installing the Python SDK" >&2
uv run --project "$root/clients/island-battles-python" --locked python -c 'import island_battles_client'

jq -n --arg root "$root" --arg firstbot "$work/firstbot" '{
  players: {
    level3: {default_level: 3},
    python: {
      worker: ["uv", "run", "--project", ($root + "/clients/island-battles-python"), "--locked",
               "python", "-m", "island_battles_client.worker", "bot:play"],
      directory: ($root + "/templates/python-bot")
    },
    go: {worker: [$firstbot, "worker"]}
  },
  maps: {crossing: ($root + "/backend/cmd/headless-match/maps/crossing.json")},
  matches: [
    {id: "python-first", map: "crossing", seats: ["python", "level3"]},
    {id: "python-second", map: "crossing", seats: ["level3", "python"]},
    {id: "go-first", map: "crossing", seats: ["go", "level3"]},
    {id: "go-second", map: "crossing", seats: ["level3", "go"]}
  ],
  turn_timeout_milliseconds: 5000
}' > "$work/schedule.json"

echo "Playing" >&2
if ! "$work/headless-match" "$work/schedule.json" > "$work/results.jsonl"; then
  cat "$work/results.jsonl"
  echo "The runner exited with an error" >&2
  exit 1
fi

jq -c '{id, winner, generations, error,
        failed_turns: [.seats[]? | {(.name): .failed_turns}] | add}' "$work/results.jsonl" >&2

played="$(jq -s 'length' "$work/results.jsonl")"
if [ "$played" -ne 4 ]; then
  echo "Expected 4 results, got $played" >&2
  exit 1
fi

# A match with an error, or a seat with a failed turn, with the first failed turn of each such seat.
problems="$(jq -c 'select(.error != null or any(.seats[]?; .failed_turns > 0))
  | {id, error, failures: [.seats[]? | select(.failed_turns > 0) | {name, failed_turns, first_failure: .failures[0]}]}' \
  "$work/results.jsonl")"
if [ -n "$problems" ]; then
  echo "$problems" | jq . >&2
  echo "A match failed, or a worker failed a turn" >&2
  exit 1
fi
echo "All four matches played without a failed turn" >&2
