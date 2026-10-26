#!/usr/bin/env bash
# Build and sign the isolated capture helper app (macOS 14.2+, Apple silicon or Intel host arch).
# Uses the fixed development identity from internal/distribution/policy.json (debugIdentity) with a
# prototype-only bundle ID and name. Never touches Voicemore.app, Voicemore Dev.app, their data or
# update feed. Output: build/system-audio-prototype/Voicemore SysAudio Proto.app
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HERE="$ROOT/tools/system-audio-prototype"
OUT_DIR="${OUT_DIR:-$ROOT/build/system-audio-prototype}"
NAME="Voicemore SysAudio Proto"
BUNDLE_ID="dev.steve.voice-input.debug.sysaudio-proto"
EXE="SysAudioCapture"
ARCH="${ARCH:-$(uname -m)}"
POLICY="$ROOT/internal/distribution/policy.json"

read_policy() { /usr/bin/plutil -extract "$1" raw -o - "$POLICY"; }
IDENTITY="$(read_policy debugIdentity)"
PROD_ID="$(read_policy bundleID)"
DEBUG_ID="$(read_policy debugBundleID)"
if [[ "$BUNDLE_ID" == "$PROD_ID" || "$BUNDLE_ID" == "$DEBUG_ID" ]]; then
  echo "refusing: prototype bundle ID collides with an app identity" >&2
  exit 1
fi
if ! /usr/bin/security find-identity -v -p codesigning | grep -Fq "\"$IDENTITY\""; then
  echo "signing identity not found in keychain: $IDENTITY" >&2
  exit 1
fi

APP="$OUT_DIR/$NAME.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS"

xcrun swiftc -O -swift-version 5 \
  -target "$ARCH-apple-macos14.2" \
  -framework CoreAudio -framework AudioToolbox -framework AVFoundation -framework CryptoKit \
  "$HERE/capture/main.swift" \
  -o "$APP/Contents/MacOS/$EXE"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
  <key>CFBundleName</key><string>$NAME</string>
  <key>CFBundleDisplayName</key><string>$NAME</string>
  <key>CFBundleExecutable</key><string>$EXE</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>LSMinimumSystemVersion</key><string>14.2</string>
  <key>LSUIElement</key><true/>
  <key>NSAudioCaptureUsageDescription</key><string>Records system audio output for a local speech-recognition comparison prototype. Audio stays on this Mac.</string>
</dict>
</plist>
PLIST
/usr/bin/plutil -lint "$APP/Contents/Info.plist"

# Development signature only (no hardened runtime, no notarization); not for distribution.
/usr/bin/codesign --force --sign "$IDENTITY" --timestamp=none --identifier "$BUNDLE_ID" "$APP"
/usr/bin/codesign --verify --strict --verbose=2 "$APP"
/usr/bin/codesign -dv --verbose=2 "$APP" 2>&1 | grep -E '^(Identifier|Authority|TeamIdentifier|Format)='
echo "built: $APP"
