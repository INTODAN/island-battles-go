#!/usr/bin/env bash
# Build the bot package (a wheel and a source archive) into dist/.
set -euo pipefail
cd "$(dirname "$0")/.."
uv build
echo "package files:"
ls -1 "$PWD"/dist/*
