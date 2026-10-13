#!/bin/sh
# Fetch the versioned release runtime and verify it before use.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
expected=$(tr -d '\r\n' < "$project_dir/internal/localasr/assets/runtime.sha256")
target="$project_dir/resources/local-asr/runtime.tar.gz"
if [ -f "$target" ] && [ "$(shasum -a 256 "$target" | awk '{print $1}')" = "$expected" ]; then
  echo 'ASR runtime already verified.'
  exit 0
fi
mkdir -p "$(dirname -- "$target")"
tmp=$(mktemp "${target}.XXXXXX")
trap 'rm -f "$tmp"' EXIT
curl --fail --location --retry 3 \
  'https://github.com/wherexml/Voicemore/releases/download/v0.2.0/voicemore-asr-runtime-arm64.tar.gz' -o "$tmp"
actual=$(shasum -a 256 "$tmp" | awk '{print $1}')
[ "$actual" = "$expected" ] || { echo 'ASR runtime checksum mismatch.' >&2; exit 1; }
mv "$tmp" "$target"
echo 'ASR runtime downloaded and verified.'
