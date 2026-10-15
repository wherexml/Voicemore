#!/bin/sh
# Native input E2E; takes foreground focus and temporarily uses the clipboard.
set -eu
cd "$(dirname "$0")/.."
identity='Apple Development: geekmooc@gmail.com (5QLDC77G39)'
if ! security find-identity -v -p codesigning | grep -Fq "\"$identity\""; then
  echo 'Required local development identity is unavailable.' >&2
  exit 1
fi
mkdir -p artifacts/current-input/Fixture.app/Contents/MacOS artifacts/remote-input
swiftc cmd/input-acceptance/fixture.swift -o artifacts/current-input/Fixture.app/Contents/MacOS/fixture
cat > artifacts/current-input/Fixture.app/Contents/Info.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.steve.voice-input.acceptance-fixture</string><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleName</key><string>Input Acceptance</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>
PLIST
codesign --force --sign "$identity" artifacts/current-input/Fixture.app
go build -o artifacts/remote-input/input-acceptance ./cmd/input-acceptance
codesign --force --sign "$identity" artifacts/remote-input/input-acceptance
codesign --verify --strict artifacts/current-input/Fixture.app
codesign --verify --strict artifacts/remote-input/input-acceptance
codesign -dr - artifacts/remote-input/input-acceptance > artifacts/remote-input/runner-signature.txt 2>&1
artifacts/remote-input/input-acceptance > artifacts/remote-input/native-results.log
cp artifacts/current-input/results.json artifacts/remote-input/native-results.json
printf 'Passed. Evidence: artifacts/remote-input/native-results.json\n'
