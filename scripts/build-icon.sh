#!/bin/sh
# Convert the existing artwork with Apple's ICNS encoder. MyGo 0.4.8's
# PNG payloads for icp4/icp5 decode incorrectly in macOS small-icon views.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/Voicemore.iconset"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$root/resources/icon.png" --out "$stage/Voicemore.iconset/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$root/resources/icon.png" --out "$stage/Voicemore.iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$stage/Voicemore.iconset" -o "$root/resources/Voicemore.icns"
