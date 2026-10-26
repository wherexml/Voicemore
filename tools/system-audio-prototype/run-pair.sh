#!/usr/bin/env bash
# Sequential, real-time paced replay of ONE captured WAV through each engine, then the report.
# usage: run-pair.sh /abs/capture.wav /abs/reference.{txt,srt} /abs/out-dir [--with-short]
# Engines never run concurrently; a cool-down separates runs. Everything stays on 127.0.0.1.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HERE="$ROOT/tools/system-audio-prototype"
WAV="$1"; REF="$2"; OUT="$3"; WITH_SHORT="${4:-}"
VENV="${VENV:-$ROOT/artifacts/system-audio-prototype-20261024/venv}"
MODEL="${SHERPA_MODEL:-$ROOT/artifacts/system-audio-prototype-20261024/models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20}"
COOLDOWN="${COOLDOWN:-10}"
PY="$VENV/bin/python"

for p in "$WAV" "$REF" "$OUT"; do [[ "$p" == /* ]] || { echo "paths must be absolute: $p" >&2; exit 2; }; done
mkdir -p "$OUT"
shasum -a 256 "$WAV" | tee "$OUT/wav.sha256"
"$PY" -c 'import sherpa_onnx, numpy, websockets, sys; print(sys.version.split()[0], "sherpa-onnx", getattr(sherpa_onnx, "__version__", "?"), "numpy", numpy.__version__, "websockets", websockets.__version__)' | tee "$OUT/versions.txt"

RUNS=("$OUT/qwen-baseline.jsonl")
"$PY" "$HERE/asr/replay_qwen.py" --wav "$WAV" --out "$OUT/qwen-baseline.jsonl" --variant baseline --port 48138
sleep "$COOLDOWN"

if [[ "$WITH_SHORT" == "--with-short" ]]; then
  "$PY" "$HERE/asr/replay_qwen.py" --wav "$WAV" --out "$OUT/qwen-subtitle-short.jsonl" --variant subtitle-short --port 48138
  RUNS+=("$OUT/qwen-subtitle-short.jsonl")
  sleep "$COOLDOWN"
fi

"$PY" "$HERE/asr/replay_sherpa.py" --wav "$WAV" --out "$OUT/sherpa-zipformer.jsonl" --model-dir "$MODEL"
RUNS+=("$OUT/sherpa-zipformer.jsonl")

"$PY" "$HERE/asr/report.py" --reference "$REF" --out-dir "$OUT" "${RUNS[@]}"
