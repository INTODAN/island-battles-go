#!/usr/bin/env bash
# Copy the lobby's API documents from the lobby module that go.mod pins,
# github.com/INTODAN/mpl, into api/lobby/, where the web client's and other
# generators read them. With --check, change nothing and fail when the copies
# differ from the pinned module's, as CI's API contract job does.
#
# The pin is go.mod's requirement of github.com/INTODAN/mpl (README.md, "The
# lobby's version"). After changing it, run this script and regenerate the web
# client's types (bun run generate in clients/island-battles-web).
#
# Usage: scripts/sync-lobby-api.sh [--check]
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

check=false
case "${1:-}" in
  "") ;;
  --check) check=true ;;
  *)
    echo "Usage: scripts/sync-lobby-api.sh [--check]" >&2
    exit 2
    ;;
esac

module=github.com/INTODAN/mpl
# The documents that this repository generates code from: the player API and
# the event schema.
documents=(openapi.json events.json)

go mod download "$module"
version="$(go list -m -f '{{.Version}}' "$module")"
module_dir="$(go list -m -f '{{.Dir}}' "$module")"
if [[ -z "$module_dir" || ! -d "$module_dir/api" ]]; then
  echo "Can't find the API documents of $module $version (go list -m printed '$module_dir')." >&2
  exit 1
fi

target=api/lobby
if [[ "$check" == true ]]; then
  stale=()
  for document in "${documents[@]}"; do
    if ! cmp -s "$module_dir/api/$document" "$target/$document"; then
      stale+=("$target/$document")
    fi
  done
  if [[ ${#stale[@]} -gt 0 ]]; then
    echo "${stale[*]} differ from $module $version. Run scripts/sync-lobby-api.sh, then bun run generate in clients/island-battles-web, and commit the result." >&2
    exit 1
  fi
  echo "$target is current with $module $version."
  exit 0
fi

mkdir -p "$target"
for document in "${documents[@]}"; do
  # The module cache is read-only; the copy must not be.
  cp "$module_dir/api/$document" "$target/$document"
  chmod u+w "$target/$document"
done
echo "Copied ${documents[*]} of $module $version into $target."
