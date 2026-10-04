#!/usr/bin/env bash
# Builds the zip uploaded to both the Chrome Web Store and Firefox Add-ons.
set -euo pipefail

cd "$(dirname "$0")/.."
version=$(node -p 'require("./manifest.json").version')
mkdir -p dist
out="dist/run-with-vedocker-${version}.zip"
rm -f "$out"

zip -r -X -q "$out" manifest.json icons src options -x '*.DS_Store'
echo "$out"
