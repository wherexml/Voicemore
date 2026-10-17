#!/bin/sh
# Daily installs and stable releases share the same signing/notarization pipeline.
# This prepares a verified candidate only; publishing requires --publish separately.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixed_identity=$(plutil -extract identity raw "$project_dir/internal/distribution/policy.json")
if [ -n "${SIGNING_IDENTITY:-}" ] && [ "$SIGNING_IDENTITY" != "$fixed_identity" ]; then
 echo 'Production builds require the approved Developer ID. Use scripts/build-development.py for isolated development builds.' >&2
 exit 1
fi
for arg in "$@"; do
 case "$arg" in --publish|--development-preview) echo 'build-macos.sh never publishes; use publish-release.sh explicitly.' >&2; exit 1;; esac
done
exec python3 "$project_dir/scripts/release-stable.py" "$@"
