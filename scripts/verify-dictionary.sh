#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
identity='Apple Development: geekmooc@gmail.com (5QLDC77G39)'
security find-identity -v -p codesigning | grep -Fq "\"$identity\""
dir=artifacts/dictionary-fix
mkdir -p "$dir/Fixture.app/Contents/MacOS"
swiftc cmd/dictionary-acceptance/fixture.swift -o "$dir/Fixture.app/Contents/MacOS/fixture"
cat > "$dir/Fixture.app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.steve.voice-input.dictionary-fixture</string><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleName</key><string>Dictionary Acceptance</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
go build -o "$dir/acceptance" ./cmd/dictionary-acceptance
codesign --force --sign "$identity" "$dir/Fixture.app"
codesign --force --sign "$identity" "$dir/acceptance"
codesign --verify --strict "$dir/Fixture.app"
codesign --verify --strict "$dir/acceptance"
codesign -dr - "$dir/acceptance" > "$dir/runner-signature.txt" 2>&1
shasum -a 256 "$dir/acceptance" > "$dir/runner-sha256.txt"
"$dir/acceptance"
