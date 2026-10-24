#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
: "${1:?Supply a new absolute output directory}"
case "$1" in /*) ;; *) exit 2;; esac
[ ! -e "$1" ] || { echo 'Output directory must be new'; exit 2; }
dir="$1"
bundle="dev.steve.voice-input.manual-edit-fixture.$(basename "$dir")"
identity='Apple Development: geekmooc@gmail.com (5QLDC77G39)'
mkdir -p "$dir/Fixture.app/Contents/MacOS"
swiftc cmd/manual-edit-acceptance/fixture.swift -o "$dir/Fixture.app/Contents/MacOS/fixture"
cat > "$dir/Fixture.app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>$bundle</string><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleName</key><string>Manual Edit Acceptance</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
go build -o "$dir/acceptance" ./cmd/manual-edit-acceptance
codesign --force --sign "$identity" "$dir/Fixture.app"
codesign --force --sign "$identity" "$dir/acceptance"
codesign --verify --strict "$dir/Fixture.app"
codesign --verify --strict "$dir/acceptance"
shasum -a 256 "$dir/acceptance" > "$dir/runner-sha256.txt"
"$dir/acceptance" --output "$dir" --bundle "$bundle"
