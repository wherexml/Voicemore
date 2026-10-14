#!/bin/sh
# Local builds always use the fixed Apple Development identity from AGENTS.md.
set -eu
fixed_identity='Apple Development: geekmooc@gmail.com (5QLDC77G39)'
SIGNING_IDENTITY=${SIGNING_IDENTITY:-$fixed_identity}
if [ "$SIGNING_IDENTITY" != "$fixed_identity" ]; then
  echo "Local builds require $fixed_identity; refusing a different or ad-hoc identity." >&2
  exit 1
fi
if ! security find-identity -v -p codesigning | grep -Fq "\"$fixed_identity\""; then
  echo "The fixed Apple Development signing identity is unavailable; build stopped." >&2
  exit 1
fi
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
./scripts/fetch-asr-runtime.sh
go tool mygo build -platform darwin/arm64 -sign "$SIGNING_IDENTITY"
version=$(plutil -extract version raw mygo.json)
dmg="build/darwin-arm64/Voicemore $version arm64.dmg"
(cd "$(dirname "$dmg")" && shasum -a 256 "$(basename "$dmg")" > SHA256SUMS.txt)
echo "Built: $dmg"
