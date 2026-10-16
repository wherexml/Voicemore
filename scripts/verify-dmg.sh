#!/bin/sh
# Repeatable installation-artifact acceptance; no user settings are changed.
set -eu
image=${1:?Usage: verify-dmg.sh /path/to/Voicemore.dmg [evidence-directory]}
evidence=${2:-artifacts/dmg-verification}
source_app=${3:-}
mkdir -p "$evidence"
hdiutil verify "$image" > "$evidence/image-verification.txt"
mount_dir=$(mktemp -d /tmp/voicemore-dmg.XXXXXX)
install_dir=$(mktemp -d /tmp/voicemore-install.XXXXXX)
mounted=false
cleanup() {
  if [ "$mounted" = true ]; then hdiutil detach "$mount_dir" >/dev/null; fi
  rm -rf "$mount_dir" "$install_dir"
}
trap cleanup EXIT
hdiutil attach "$image" -readonly -nobrowse -mountpoint "$mount_dir" > "$evidence/mount.txt"
mounted=true
[ "$(readlink "$mount_dir/Applications")" = /Applications ]
[ -f "$mount_dir/.DS_Store" ]
"$(dirname "$0")/../build/dmg-tools/bin/python" "$(dirname "$0")/verify-dmg-layout.py" "$mount_dir" "$evidence" > "$evidence/layout-check.txt"
if [ -n "$source_app" ]; then
  diff -qr "$source_app" "$mount_dir/Voicemore.app" > "$evidence/app-content-comparison.txt"
  printf '%s\n' 'PASS: packaged app contents match the source app.' >> "$evidence/app-content-comparison.txt"
fi
ditto "$mount_dir/Voicemore.app" "$install_dir/Voicemore.app"
app="$install_dir/Voicemore.app"
codesign --verify --deep --strict "$app" 2> "$evidence/signature.txt"
[ "$(lipo -archs "$app/Contents/MacOS/Voicemore")" = arm64 ]
[ "$(plutil -extract CFBundleName raw "$app/Contents/Info.plist")" = Voicemore ]
[ "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")" = "$(plutil -extract version raw mygo.json)" ]
[ "$(plutil -extract LSMinimumSystemVersion raw "$app/Contents/Info.plist")" = 26.2 ]
expected=$(tr -d '\r\n' < internal/localasr/assets/runtime.sha256)
actual=$(shasum -a 256 "$app/Contents/Resources/local-asr/runtime.tar.gz" | awk '{print $1}')
[ "$expected" = "$actual" ]
shasum -a 256 "$image" > "$evidence/SHA256SUMS.txt"
printf '%s\n' 'PASS: DMG integrity, Applications shortcut, Finder layout and fixed-English background, copied app signature, arm64, product metadata, bundled runtime checksum.' | tee "$evidence/result.txt"
