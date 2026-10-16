#!/bin/sh
# Package an already signed app; never modifies its sealed contents.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
env_dir="$root/build/dmg-tools"
if [ -x "$env_dir/bin/python" ] && ! "$env_dir/bin/python" -c 'import sys; assert sys.version_info >= (3,10)' 2>/dev/null; then
  mv "$env_dir" "$env_dir.previous-$(date +%Y%m%d%H%M%S)"
fi
if [ ! -x "$env_dir/bin/python" ]; then
  if command -v uv >/dev/null 2>&1; then
    uv venv --python '>=3.10' "$env_dir"
  else
    python_cmd=python3
    for candidate in python3.13 python3.12 python3.11 python3.10 python3; do
      if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import sys; assert sys.version_info >= (3,10)' 2>/dev/null; then
        python_cmd=$candidate
        break
      fi
    done
    "$python_cmd" -c 'import sys; assert sys.version_info >= (3,10), "DMG packaging requires Python 3.10+ or uv"'
    "$python_cmd" -m venv "$env_dir"
  fi
fi
if ! "$env_dir/bin/python" -c 'import importlib.metadata as m; assert m.version("dmgbuild")=="1.6.7" and m.version("ds-store")=="1.3.3" and m.version("mac-alias")=="2.2.3"' 2>/dev/null; then
  if command -v uv >/dev/null 2>&1; then
    uv pip install --python "$env_dir/bin/python" -r "$root/scripts/dmg-requirements.txt"
  else
    "$env_dir/bin/python" -m pip install --disable-pip-version-check -r "$root/scripts/dmg-requirements.txt"
  fi
fi
exec "$env_dir/bin/python" "$root/scripts/build-dmg.py" "$@"
