#!/usr/bin/env bash
# Smoke-test the deployment: the game's CloudFront and Lambda, the lobby at its
# own name, Postgres and Centrifugo together (deploy/smoke.py). It reuses the
# saved guest credential in deploy/local/smoke-session.json, and reads the
# game's and the lobby's URLs from Terraform's outputs. With
# ISLAND_BATTLES_EXPECTED_VERSION set, e.g. v1.1.0, it also fails unless
# /healthz reports that version. With ISLAND_BATTLES_EXPECTED_WEB_BUNDLE set,
# e.g. assets/index-1a2b3c4d.js, it also fails unless /play/ loads that script
# (what scripts/deploy-web-client.sh just uploaded). See deploy/README.md.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
local_directory="$repository_root/deploy/local"

if [[ $# -ne 0 ]]; then
  echo "Usage: scripts/smoke.sh" >&2
  exit 2
fi

deploy_files() {
  python3 -I "$repository_root/deploy/deploy_files.py" "$@"
}

# Pushes a new guest credential, even when a later check failed.
finish() {
  local status=$?
  if ! deploy_files push; then
    echo "deploy/local/ isn't in the deploy bucket yet. Run scripts/deploy-files.sh push." >&2
    [[ $status -ne 0 ]] || status=1
  fi
  exit "$status"
}

for command in aws python3 terraform uv; do
  if ! command -v "$command" >/dev/null; then
    echo "Install $command first. See deploy/README.md." >&2
    exit 1
  fi
done
deploy_files pull
trap finish EXIT
game_url="$("$repository_root/scripts/aws.sh" output game_url)"
lobby_url="$("$repository_root/scripts/aws.sh" output lobby_url)"
# The smoke test needs httpx and websockets, which the Python SDK's locked
# dependencies include.
uv run --project "$repository_root/clients/island-battles-python" --locked --no-dev \
  python "$repository_root/deploy/smoke.py" \
  --game-url "$game_url" --lobby-url "$lobby_url" --session-file "$local_directory/smoke-session.json" \
  ${ISLAND_BATTLES_EXPECTED_VERSION:+--expected-version "$ISLAND_BATTLES_EXPECTED_VERSION"} \
  ${ISLAND_BATTLES_EXPECTED_WEB_BUNDLE:+--expected-web-bundle "$ISLAND_BATTLES_EXPECTED_WEB_BUNDLE"}
