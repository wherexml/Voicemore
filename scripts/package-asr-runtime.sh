#!/bin/sh
# Build an independent, relocatable ASR runtime from the verified local environment.
# Does not install into or modify the source environment.
set -eu
source_env=${1:?Usage: package-asr-runtime.sh /path/to/verified/python3.11/venv}
source_python=$("$source_env/bin/python" -c 'import sys; print(sys.base_prefix)')
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/runtime"
ditto "$source_python" "$stage/runtime"
ditto "$source_env/lib/python3.11/site-packages" "$stage/runtime/lib/python3.11/site-packages"
# Python CLI entrypoints copied from virtualenvs can contain absolute shebangs;
# only invoke the relocated interpreter with -I, never those entrypoints.
"$stage/runtime/bin/python3.11" -I -c 'import mlx.core, mlx_audio, onnxruntime, fastapi, uvicorn; print("Standalone ASR imports passed")'
mkdir -p "$project_dir/resources/local-asr"
COPYFILE_DISABLE=1 tar -czf "$project_dir/resources/local-asr/runtime.tar.gz" -C "$stage" runtime
shasum -a 256 "$project_dir/resources/local-asr/runtime.tar.gz" | awk '{print $1}' > "$project_dir/internal/localasr/assets/runtime.sha256"
