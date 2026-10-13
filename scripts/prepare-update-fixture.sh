#!/bin/sh
# Build a local older client for native end-to-end acceptance, never distribution.
set -eu
: "${SIGNING_IDENTITY:?Set a local code-signing identity}"
cd "$(dirname "$0")/.."
fixture=artifacts/updater-e2e
mkdir -p "$fixture"
ditto build/darwin-arm64/Voicemore.app "$fixture/Voicemore.app"
public_key=$(plutil -extract updates.publicKey raw mygo.json)
go build -ldflags "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Voicemore -X github.com/egoist/mygo.packageVersion=0.2.9 -X github.com/egoist/mygo.packageIdentifier=dev.steve.voice-input -X github.com/egoist/mygo.packageUpdateFeed=http://127.0.0.1:48139/update-darwin-arm64.json -X github.com/egoist/mygo.packageUpdateKey=$public_key" -o "$fixture/Voicemore.app/Contents/MacOS/Voicemore" .
plutil -replace CFBundleShortVersionString -string 0.2.9 "$fixture/Voicemore.app/Contents/Info.plist"
plutil -replace CFBundleVersion -string 0.2.9 "$fixture/Voicemore.app/Contents/Info.plist"
codesign --force --deep --options runtime --entitlements resources/VoiceInput.entitlements --sign "$SIGNING_IDENTITY" "$fixture/Voicemore.app"
printf corrupt > "$fixture/mode"
shasum -a 256 "$fixture/Voicemore.app/Contents/MacOS/Voicemore" > "$fixture/before.sha256"
echo 'Fixture prepared. Quit the regular app, run scripts/update-fixture.py, and launch artifacts/updater-e2e/Voicemore.app.'
