#!/usr/bin/env bash
# Build the 3D web client and publish it at https://<game_hostname>/play/. See
# deploy/README.md, "The web client".
#
# Usage: scripts/deploy-web-client.sh
#
# Uploads the hashed files under assets/ first, with a year-long immutable
# Cache-Control, then the runtime config, config.json, which names the lobby's
# URL (#228, #232), and index.html, both with no-cache, so a page never names a
# file that isn't there yet. Old hashed files stay in the bucket: tabs that are
# open keep working. Then it invalidates /play/, /play/index.html and
# /play/config.json at CloudFront. To roll back, check out the earlier commit
# and run it again.
#
# If ISLAND_BATTLES_WEB_BUNDLE_FILE names a file, the script writes the entry
# script that index.html loads there (such as assets/index-1a2b3c4d.js), so the
# smoke test can check that /play/ serves this build (deploy/smoke.py).
#
# Needs bun, python3, the AWS CLI and Terraform, and the AWS credentials of
# scripts/aws.sh. The bucket, the distribution and the lobby's URL come from
# Terraform's outputs, so the resources must have been applied first.
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
client_directory="$repository_root/clients/island-battles-web"

if [[ $# -ne 0 ]]; then
  echo "Usage: scripts/deploy-web-client.sh" >&2
  exit 2
fi
for command in bun aws; do
  if ! command -v "$command" >/dev/null; then
    echo "Install $command first. See deploy/README.md." >&2
    exit 1
  fi
done

bucket="$("$repository_root/scripts/aws.sh" output web_client_bucket)"
distribution_id="$("$repository_root/scripts/aws.sh" output distribution_id)"
lobby_url="$("$repository_root/scripts/aws.sh" output lobby_url)"

(cd "$client_directory" && bun install --frozen-lockfile && bun run build)
if [[ ! -f "$client_directory/dist/index.html" ]]; then
  echo "The build left no dist/index.html." >&2
  exit 1
fi

bundle="$(grep -oE 'src="/play/assets/[^"]+\.js"' "$client_directory/dist/index.html" | head -n 1 \
  | sed -E 's|^src="/play/(.*)"$|\1|' || true)"
if [[ -z "$bundle" ]]; then
  echo "dist/index.html loads no script from /play/assets/." >&2
  exit 1
fi

# Only hashed files get the year-long cache. Anything else in dist/ would be
# cached for a year under a name that never changes, so refuse it.
unexpected="$(find "$client_directory/dist" -mindepth 1 -maxdepth 1 \
  ! -name index.html ! -name assets)"
if [[ -n "$unexpected" ]]; then
  echo "dist/ holds files other than index.html and assets/; they would be cached for a year:" >&2
  echo "$unexpected" >&2
  exit 1
fi

# No --delete: the files of earlier builds stay for open tabs.
aws s3 sync "$client_directory/dist/assets/" "s3://$bucket/play/assets/" \
  --cache-control "public, max-age=31536000, immutable" \
  --only-show-errors
# The browser build's runtime config (clients/island-battles-web/src/platform.ts):
# the lobby at its own name, while the game's calls stay on the page's origin.
config_file="$(mktemp)"
trap 'rm -f "$config_file"' EXIT
python3 -I -c 'import json, sys; print(json.dumps({"lobby_url": sys.argv[1]}))' "$lobby_url" >"$config_file"
aws s3 cp "$config_file" "s3://$bucket/play/config.json" \
  --cache-control "no-cache" \
  --content-type "application/json" \
  --only-show-errors
aws s3 cp "$client_directory/dist/index.html" "s3://$bucket/play/index.html" \
  --cache-control "no-cache" \
  --content-type "text/html; charset=utf-8" \
  --only-show-errors

aws cloudfront create-invalidation --distribution-id "$distribution_id" \
  --paths "/play/" "/play/index.html" "/play/config.json" --query Invalidation.Id --output text >&2

if [[ -n "${ISLAND_BATTLES_WEB_BUNDLE_FILE:-}" ]]; then
  echo "$bundle" >"$ISLAND_BATTLES_WEB_BUNDLE_FILE"
fi

game_url="$("$repository_root/scripts/aws.sh" output game_url)"
echo "Published the web client to $game_url/play/, with the lobby at $lobby_url"
