#!/bin/sh
# All distributable Voicemore builds use the approved Developer ID identity.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "${1:-}" = --development-preview ]; then
 echo 'Development apps are isolated and have no public update feed. Build one with scripts/build-development.py; use the notarized stable pipeline for distribution.' >&2
 exit 1
fi
exec python3 "$project_dir/scripts/release-stable.py" "$@"
