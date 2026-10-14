#!/bin/sh
# This pipeline builds development-signed previews, not public distribution builds.
set -eu
if [ "$#" -ne 1 ] || [ "$1" != --development-preview ]; then
 echo 'Stopped: this pipeline uses Apple Development signing and does not notarize.' >&2
 echo 'For an explicitly intended prerelease, use --development-preview.' >&2
 echo 'Stable distribution requires the separate Developer ID/notarization workflow in docs/public/DISTRIBUTION.md.' >&2
 exit 1
fi
: "${SIGNING_IDENTITY:?Set the macOS signing identity}"
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
[ -z "$(git status --porcelain)" ] || { echo 'Commit changes before publishing.' >&2; exit 1; }
if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
 key_file=${VOICEMORE_UPDATE_KEY_FILE:-"$HOME/.config/voicemore/update-keys/mygo-update.key"}
 [ -f "$key_file" ] || { echo 'Provide the original updater signing key.' >&2; exit 1; }
 MYGO_UPDATER_PRIVATE_KEY=$(cat "$key_file")
 export MYGO_UPDATER_PRIVATE_KEY
fi
version=$(plutil -extract version raw mygo.json)
tag="v$version"
if gh release view "$tag" --repo wherexml/Voicemore >/dev/null 2>&1; then
 echo "Release $tag already exists; refusing to overwrite published updates." >&2; exit 1
fi
./scripts/build-macos.sh
out=build/darwin-arm64
manifest="$out/update-darwin-arm64.json"
[ "$(plutil -extract version raw "$manifest")" = "$version" ]
archive="voicemore-$version-darwin-arm64.tar.gz"
[ -s "$out/$archive" ]
./scripts/verify-dmg.sh "$out/Voicemore $version arm64.dmg" "artifacts/release-$version/dmg-check"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
cp "$out/Voicemore $version arm64.dmg" "$stage/Voicemore-$version-arm64.dmg"
cp "$out/$archive" "$manifest" "$stage/"
(cd "$stage" && shasum -a 256 ./*.dmg ./*.tar.gz ./*.json > SHA256SUMS.txt)
python3 - "$manifest" "$stage/notes.md" <<'PY'
import json,sys
from pathlib import Path
m=json.loads(Path(sys.argv[1]).read_text())
Path(sys.argv[2]).write_text(m.get('notes','')+'\n\nApple Silicon / macOS 26.2+. Download the DMG for first installation; drag Voicemore to Applications. Existing 0.3.0+ clients offer this update on Home.\n\n首次安装请下载 DMG 并拖入 Applications；0.3.0 起可在首页一键更新并重启。0.2.0 需先手动升级一次。\n\nDistribution is currently development-signed and not Apple-notarized. Update archives have a separate Ed25519 signature. 开发签名版本尚未完成 Apple 公证；更新包另有 Ed25519 签名校验。\n')
PY
if git rev-parse "$tag" >/dev/null 2>&1; then
 [ "$(git rev-list -n 1 "$tag")" = "$(git rev-parse HEAD)" ] || { echo 'Tag differs from HEAD.' >&2; exit 1; }
else
 git tag -a "$tag" -m "Voicemore $version"
fi
git push origin HEAD "$tag"
gh release create "$tag" --repo wherexml/Voicemore --verify-tag --draft --prerelease --title "Voicemore $version (Development Preview)" --notes-file "$stage/notes.md" "$stage/Voicemore-$version-arm64.dmg" "$stage/$archive" "$stage/update-darwin-arm64.json" "$stage/SHA256SUMS.txt"
# Upload all assets before making the development prerelease visible.
gh release edit "$tag" --repo wherexml/Voicemore --draft=false --prerelease --latest=false
