#!/usr/bin/env bash
# Print the directory of the lobby module that go.mod pins,
# github.com/INTODAN/mpl, downloading it first. It is in Go's module cache,
# which is read-only.
#
# With --copy, copy it to a new temporary directory that can be written to,
# and print that one instead: the lobby's live test runner
# (integration/run.py) builds the lobby there and runs pytest and uv in it,
# which write beside the sources. The caller deletes the copy.
#
# Usage: scripts/lobby-module.sh [--copy]
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

module=github.com/INTODAN/mpl
go mod download "$module"
module_dir="$(go list -m -f '{{.Dir}}' "$module")"
if [[ -z "$module_dir" || ! -f "$module_dir/go.mod" ]]; then
  echo "Can't find $module in the module cache (go list -m printed '$module_dir')." >&2
  exit 1
fi

case "${1:-}" in
  "")
    echo "$module_dir"
    ;;
  --copy)
    copy="$(mktemp -d "${TMPDIR:-/tmp}/mpl-XXXXXX")"
    cp -R "$module_dir/." "$copy"
    chmod -R u+w "$copy"
    echo "$copy"
    ;;
  *)
    echo "Usage: scripts/lobby-module.sh [--copy]" >&2
    exit 2
    ;;
esac
