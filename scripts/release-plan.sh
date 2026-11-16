#!/usr/bin/env bash
# Work out the next release for the release workflow (.github/workflows/release.yml):
# its version. Prints version=, previous= and bump= lines (for $GITHUB_OUTPUT)
# on stdout, and explains itself in Markdown on stderr (for the run's summary).
#
# Usage: scripts/release-plan.sh <patch|minor|major> [commit]
#
# Run it in a clone that has all tags. The commit defaults to HEAD.
#
# The bump is the one asked for. This repository doesn't deploy the VM since
# the cutover (#232): INTODAN/mpl does, so nothing here makes a release major
# by itself any more. Mark a release major when the pull requests since the
# previous one break clients, and say so in its notes.
set -euo pipefail

[[ $# -ge 1 && $# -le 2 ]] || { echo "Usage: $0 <patch|minor|major> [commit]" >&2; exit 2; }
requested="$1"
commit="$(git rev-parse --verify "${2:-HEAD}^{commit}")"
case "$requested" in
  patch | minor | major) ;;
  *) echo "The bump must be patch, minor or major, not '$requested'." >&2; exit 2 ;;
esac

previous="$(git tag --list 'v[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)"

if [[ -z "$previous" ]]; then
  # The owner decided the first version.
  version="v0.1.0"
  bump="first"
  reason="There is no earlier release, so this is the first one: $version."
else
  if [[ "$(git rev-parse "$previous^{commit}")" == "$commit" ]]; then
    echo "$previous is already this commit (${commit:0:7}). Merge something first." >&2
    exit 1
  fi
  bump="$requested"
  IFS=. read -r major minor patch <<<"${previous#v}"
  case "$bump" in
    major) version="v$((major + 1)).0.0" ;;
    minor) version="v$major.$((minor + 1)).0" ;;
    patch) version="v$major.$minor.$((patch + 1))" ;;
  esac
  reason="The bump is $bump, as you asked. The VM is deployed from INTODAN/mpl, so the release doesn't deploy it."
fi

# The release tags these two; the Go SDK module, in a subdirectory, needs its
# directory as the tag's prefix. Go refuses a v2 or later tag for a module
# path without /v2, so a major release past v1 needs the SDK's module path
# moved first. The lobby is released from INTODAN/mpl.
if [[ "$version" =~ ^v([0-9]+)\. ]] && ((BASH_REMATCH[1] >= 2)); then
  echo "$version would tag the Go SDK clients/island-battles-go/$version, which Go refuses for the module path github.com/INTODAN/island-battles-go/clients/island-battles-go. Move the SDK's module path to .../v${BASH_REMATCH[1]} first, or release a minor version." >&2
  exit 1
fi
for tag in "$version" "clients/island-battles-go/$version"; do
  if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    echo "The tag $tag already exists." >&2
    exit 1
  fi
done

{
  echo "## Release $version"
  echo
  echo "Commit \`${commit:0:7}\`, previous release: ${previous:-none}."
  echo
  echo "$reason"
} >&2

echo "version=$version"
echo "previous=$previous"
echo "bump=$bump"
