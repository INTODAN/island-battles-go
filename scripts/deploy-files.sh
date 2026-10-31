#!/usr/bin/env bash
# Sync deploy/local/ with the deploy bucket, or create the bucket. scripts/aws.sh
# and scripts/vm.sh pull and push on their own. See deploy/README.md.
#
# Usage:
#   scripts/deploy-files.sh bootstrap --region <region>
#       Create the deploy bucket, once per AWS account.
#   scripts/deploy-files.sh status|pull|push [--force]
#       Show, fetch or send the differences. A file changed on both sides is a
#       conflict, and --force makes the bucket's copy (pull) or this one (push)
#       win.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 -I "$repository_root/deploy/deploy_files.py" "$@"
