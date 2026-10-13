#!/bin/sh
# SIGNING_IDENTITY='Developer ID Application: ...' ./scripts/build-macos.sh
# Explicit local-only builds may use SIGNING_IDENTITY=-.
set -eu
: "${SIGNING_IDENTITY:?Set SIGNING_IDENTITY to a code-signing identity (or - for a local ad-hoc build)}"
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
./scripts/fetch-asr-runtime.sh
go tool mygo build -platform darwin/arm64 -sign "$SIGNING_IDENTITY"
version=$(plutil -extract version raw mygo.json)
dmg="build/darwin-arm64/Voicemore $version arm64.dmg"
(cd "$(dirname "$dmg")" && shasum -a 256 "$(basename "$dmg")" > SHA256SUMS.txt)
echo "Built: $dmg"
