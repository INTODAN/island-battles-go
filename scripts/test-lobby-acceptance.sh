#!/usr/bin/env bash
# Exercise the shared lobby suite against the Island Battles host application.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec "$repository_root/lobby/scripts/test-acceptance.sh" \
  --server-root "$repository_root" --server-package ./backend \
  --env-prefix ISLAND_BATTLES_ --extra-tests "$repository_root/integration/island_battles"
