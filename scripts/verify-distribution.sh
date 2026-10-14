#!/bin/sh
# Read-only acceptance of already signed/notarized artifacts. Never grants exceptions.
set -eu
app=${1:?Usage: verify-distribution.sh /path/Voicemore.app /path/Voicemore.dmg /path/evidence}
image=${2:?Provide the final DMG}
evidence_root=${3:?Provide an evidence directory}
[ -d "$app" ] && [ -f "$image" ] || { echo 'App or DMG missing.' >&2; exit 1; }
mkdir -p "$evidence_root"
evidence=$(mktemp -d "$evidence_root/distribution.XXXXXX")
passed=false
finish() {
 if [ "$passed" = true ]; then
  echo 'PASS: supplied app and DMG pass distribution checks; cross-Mac installation and runtime E2E still required.' > "$evidence/result.txt"
 else
  echo 'FAIL: artifact is not approved for stable distribution; inspect the evidence files.' > "$evidence/result.txt"
 fi
 cat "$evidence/result.txt"
 printf 'Evidence: %s\n' "$evidence"
}
trap finish EXIT
sw_vers > "$evidence/system.txt"
shasum -a 256 "$image" > "$evidence/SHA256SUMS.txt"
codesign --verify --deep --strict --verbose=2 "$app" > "$evidence/signature-verification.txt" 2>&1
codesign -d -r- --verbose=4 "$app" > "$evidence/signature-details.txt" 2>&1
[ "$(plutil -extract CFBundleIdentifier raw "$app/Contents/Info.plist")" = dev.steve.voice-input ]
if ! grep -q '^Authority=Developer ID Application:' "$evidence/signature-details.txt"; then
 echo 'Developer ID Application signing is required; development signatures are not accepted.' > "$evidence/rejection.txt"
 exit 1
fi
grep -q '^TeamIdentifier=T695TT4HT9$' "$evidence/signature-details.txt" || {
 echo 'The approved Skylance distribution team is required.' > "$evidence/rejection.txt"
 exit 1
}
spctl --assess --type execute --verbose=4 "$app" > "$evidence/app-gatekeeper.txt" 2>&1
xcrun stapler validate "$app" > "$evidence/app-ticket.txt" 2>&1
hdiutil verify "$image" > "$evidence/dmg-integrity.txt" 2>&1
codesign --verify --strict --verbose=2 "$image" > "$evidence/dmg-signature.txt" 2>&1
codesign -d --verbose=4 "$image" > "$evidence/dmg-signature-details.txt" 2>&1
grep -q '^TeamIdentifier=T695TT4HT9$' "$evidence/dmg-signature-details.txt"
xcrun stapler validate "$image" > "$evidence/dmg-ticket.txt" 2>&1
spctl --assess --type open --context context:primary-signature --verbose=4 "$image" > "$evidence/dmg-gatekeeper.txt" 2>&1
# Compare every sealed file and symlink with the app actually inside the DMG.
python3 "$(dirname "$0")/verify-distribution-content.py" "$app" "$image" > "$evidence/dmg-content.txt" 2>&1
passed=true
