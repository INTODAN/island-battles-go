#!/usr/bin/env bash
# Build the 3D web client and publish it at https://<game_hostname>/play/. See
# deploy/README.md, "The web client".
#
# Usage: scripts/deploy-web-client.sh
#
# Uploads the hashed files under assets/ first, with a year-long immutable
# Cache-Control, then index.html with no-cache, so a page never names a file
# that isn't there yet. Old hashed files stay in the bucket: tabs that are open
# keep working. Then it invalidates /play/ and /play/index.html at CloudFront.
# To roll back, check out the earlier commit and run it again.
#
# Needs bun, python3, the AWS CLI and Terraform, and the AWS credentials of
# scripts/aws.sh. The bucket and the distribution come from Terraform's outputs,
# so the resources must have been applied first.
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

(cd "$client_directory" && bun install --frozen-lockfile && bun run build)
if [[ ! -f "$client_directory/dist/index.html" ]]; then
  echo "The build left no dist/index.html." >&2
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
aws s3 cp "$client_directory/dist/index.html" "s3://$bucket/play/index.html" \
  --cache-control "no-cache" \
  --content-type "text/html; charset=utf-8" \
  --only-show-errors

aws cloudfront create-invalidation --distribution-id "$distribution_id" \
  --paths "/play/" "/play/index.html" --query Invalidation.Id --output text >&2

game_url="$("$repository_root/scripts/aws.sh" output game_url)"
echo "Published the web client to $game_url/play/"
