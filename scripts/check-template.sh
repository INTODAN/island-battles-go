#!/usr/bin/env bash
# Checks the Python bot template the way a player gets it: copies
# templates/python-bot as the bot tutorial does
# (clients/island-battles-web/src/ui/lobby/tutorial.ts), installs it with the
# tutorial's command, and runs the template's scripts/check.sh. The template
# has no uv.lock, so the install takes the newest of everything: the newest
# island-battles-python, and the newest ruff, ty and pytest that its
# pyproject.toml allows.
#
# Usage: scripts/check-template.sh [version]
#        scripts/check-template.sh --published
#
# With a version, or none, the SDK comes from this working copy, built at the
# given version (the release's, such as v1.4.0) and offered next to PyPI, as
# PyPI offers it once the release publishes it. Without a version it is
# 999.0.0, newer than any release, so the install takes it just the same. The
# script fails if the install takes any other SDK, or if the template's checks
# fail: the bot's tests, and `island-battles-bot check` on the SDK's fixture
# turn. That fixture is a sample turn, not one this commit's server made: the
# backend's tests keep only its upgrade table equal to the server's
# (backend/bots/bots_test.go, on the Go SDK's fixture, which the Python SDK's
# tests keep the same as its own).
#
# --published installs what a player who copies the template gets today: the
# newest SDK on PyPI, nothing from this working copy, and no version check. It
# fails when the template needs an SDK that is not released yet; such a change
# waits for the release.
#
# Needs uv and PyPI. It changes nothing in the repository.
set -euo pipefail

[[ $# -le 1 ]] || { echo "Usage: $0 [version | --published]" >&2; exit 2; }
published=false
if [[ "${1:-}" == --published ]]; then
  published=true
else
  version="${1:-999.0.0}"
  version="${version#v}"
fi
root="$(cd "$(dirname "$0")/.." && pwd)"

# The tutorial's install command, run in the copied template.
install=(uv sync)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Copies a directory as a fresh clone has it: no environments, builds, caches or
# the uv.lock that a local `uv sync` writes in the template (.gitignore).
copy() {
  mkdir -p "$2"
  tar -C "$1" -c \
    --exclude=.venv --exclude=uv.lock --exclude=dist --exclude=__pycache__ \
    --exclude=.pytest_cache --exclude=.ruff_cache --exclude=turn.json --exclude=bot.log \
    . | tar -C "$2" -x
}

# The island-battles-python that the copy installed.
sdk_version() {
  uv run --frozen python -c 'import importlib.metadata as m; print(m.version("island-battles-python"))'
}

if $published; then
  echo "Installing the template as the tutorial does, from PyPI alone: ${install[*]}"
  copy "$root/templates/python-bot" "$work/my-bot"
  cd "$work/my-bot"
  unset UV_FIND_LINKS
  "${install[@]}"
  echo "The template has island-battles-python $(sdk_version) from PyPI"
  scripts/check.sh
  exit
fi

echo "Building island-battles-python $version from this working copy"
copy "$root/clients/island-battles-python" "$work/sdk"
uv version --directory "$work/sdk" --frozen "$version" >/dev/null
uv build --quiet --wheel "$work/sdk" --out-dir "$work/wheels"

echo "Installing the template as the tutorial does: ${install[*]}"
copy "$root/templates/python-bot" "$work/my-bot"
cd "$work/my-bot"
# The built wheel stands in for the release on PyPI. Every uv command below
# sees it, so `uv run` keeps the install instead of going back to PyPI's.
export UV_FIND_LINKS="$work/wheels"
"${install[@]}" --refresh-package island-battles-python

installed="$(sdk_version)"
if [[ "$installed" != "$version" ]]; then
  echo "The tutorial's install took island-battles-python $installed, not $version, the version this release publishes." >&2
  exit 1
fi
echo "The template has island-battles-python $installed"

scripts/check.sh
