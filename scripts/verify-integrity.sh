#!/bin/sh
# Synthetic file/HTTP/controller acceptance, never installs or publishes.
set -eu
umask 077
cd "$(dirname "$0")/.."
output=${1:-artifacts/review-fixes-20261017/repeat-$(date +%s)}
[ ! -e "$output" ] || { echo 'Evidence output must be a new directory.' >&2; exit 1; }
mkdir -p "$output"
go build . ./internal/... ./cmd/... > "$output/build.log" 2>&1
go vet ./internal/config ./internal/store ./internal/dict ./internal/cleanup > "$output/vet.log" 2>&1
go build -race -tags integrityacceptance -o "$output/acceptance" ./cmd/integrity-acceptance > "$output/acceptance-build.log" 2>&1
"$output/acceptance" -output "$output/cases" > "$output/acceptance.log" 2>&1
go run ./cmd/cleanup-acceptance > "$output/cleanup-transport.json"
go run ./cmd/cleanup-guard-acceptance > "$output/cleanup-contract.json"
go run ./cmd/cleanup-thinking-acceptance > "$output/cleanup-thinking.json"
go run ./cmd/custom-thinking-acceptance > "$output/custom-thinking.json"
go run ./cmd/dictionary-acceptance --observations > "$output/dictionary-observations.log"
printf 'PASS: file, HTTP and controller acceptance. Evidence: %s/cases/results.json\n' "$output"
