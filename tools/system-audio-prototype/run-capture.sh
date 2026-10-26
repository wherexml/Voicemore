#!/usr/bin/env bash
# Launch the capture helper through LaunchServices so macOS attributes system-audio recording
# permission to the prototype bundle (not Terminal). Blocks until capture ends.
# usage: run-capture.sh [/abs/output-dir] [duration_s] [extra helper args, e.g. --pid 1234]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
APP="${APP:-$ROOT/build/system-audio-prototype/Voicemore SysAudio Proto.app}"
OUT="${1:-$ROOT/artifacts/system-audio-prototype/capture-$(date -u +%Y%m%dT%H%M%SZ)}"
DURATION="${2:-60}"
shift $(( $# > 2 ? 2 : $# )) || true

[[ "$OUT" == /* ]] || { echo "output dir must be absolute" >&2; exit 2; }
[[ -d "$APP" ]] || { echo "missing $APP — run build-capture.sh first" >&2; exit 2; }
mkdir -p "$OUT"

set +e
/usr/bin/open -n -W \
  --stdout "$OUT/helper.stdout.log" --stderr "$OUT/helper.stderr.log" \
  "$APP" --args --out "$OUT/capture.wav" --log "$OUT/capture.jsonl" --duration "$DURATION" "$@"
OPEN_STATUS=$?
set -e

# `open -W` does not forward the helper's exit code; the JSONL summary is authoritative.
echo "open exit: $OPEN_STATUS"
tail -n 1 "$OUT/capture.jsonl" 2>/dev/null || echo "no timeline written (see $OUT/helper.stderr.log)"
if [[ -f "$OUT/capture.wav" ]]; then
  /usr/bin/afinfo "$OUT/capture.wav" | grep -E 'File type|Data format|estimated duration' || true
  /usr/bin/shasum -a 256 "$OUT/capture.wav"
fi
echo "output: $OUT"
