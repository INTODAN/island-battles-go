#!/usr/bin/env bash
# Write the SDK's fixture turn to a file (default turn.json) for experiments.
# Edit the copy, then replay it: uv run island-battles-bot check bot:play --turn turn.json
set -euo pipefail
cd "$(dirname "$0")/.."
output="${1:-turn.json}"
uv run island-battles-bot fixture > "$output"
echo "wrote $output"
