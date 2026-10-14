#!/usr/bin/env python3
"""Verify the mounted DMG contains the supplied, accepted app byte for byte."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def inventory(root):
    result = {}
    for path in sorted(root.rglob('*')):
        rel = str(path.relative_to(root))
        if path.is_symlink():
            result[rel] = ('link', os.readlink(path))
        elif path.is_file():
            h = hashlib.sha256()
            with path.open('rb') as f:
                for chunk in iter(lambda: f.read(1024 * 1024), b''):
                    h.update(chunk)
            result[rel] = ('file', path.stat().st_mode & 0o777, h.hexdigest())
    return result


app, image = map(Path, sys.argv[1:])
with tempfile.TemporaryDirectory(prefix='voicemore-distribution-mount-') as mount:
    subprocess.run(['hdiutil', 'attach', str(image), '-readonly', '-nobrowse', '-mountpoint', mount], check=True)
    try:
        included = Path(mount) / 'Voicemore.app'
        assert os.readlink(Path(mount) / 'Applications') == '/Applications'
        assert inventory(app) == inventory(included), 'DMG app differs from supplied app'
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(included)], check=True)
        subprocess.run(['xcrun', 'stapler', 'validate', str(included)], check=True)
        subprocess.run(['spctl', '--assess', '--type', 'execute', '--verbose=4', str(included)], check=True)
        print('PASS: DMG contains identical signed and stapled app and Applications link.')
    finally:
        subprocess.run(['hdiutil', 'detach', mount], check=True)
