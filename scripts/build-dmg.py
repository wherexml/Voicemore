#!/usr/bin/env python3
"""Build the fixed-English Finder installer around an existing signed Voicemore.app."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

import dmgbuild

ROOT = Path(__file__).resolve().parent.parent


def run(*args):
    subprocess.run([str(a) for a in args], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('app', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--volume-name', default='Voicemore')
    args = parser.parse_args()
    app, output = args.app.resolve(), args.output.resolve()
    if app.name != 'Voicemore.app' or not app.is_dir():
        parser.error('Expected an existing signed Voicemore.app')
    if output.exists():
        parser.error('Output already exists; choose a new path')
    run('codesign', '--verify', '--deep', '--strict', app)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='voicemore-dmg-', dir=output.parent) as scratch:
        stage = Path(scratch)
        # A 1x + 2x TIFF prevents a blurred Retina background. This is mechanical
        # resampling of the reviewed imagegen artwork, not asset regeneration.
        run('sips', '-z', '512', '768', ROOT / 'resources/dmg/background@2x.png',
            '--out', stage / 'background.png')
        run('ditto', ROOT / 'resources/dmg/background@2x.png', stage / 'background@2x.png')
        settings = {
            'format': 'UDZO', 'filesystem': 'HFS+',
            'files': [str(app)],
            'symlinks': {'Applications': '/Applications'},
            'background': str(stage / 'background.png'),
            'window_rect': ((200, 200), (768, 512)),
            'icon_size': 160, 'text_size': 10, 'grid_spacing': 100,
            'icon_locations': {
                'Voicemore.app': (228, 196), 'Applications': (539, 196),
            },
            # Never set FinderInfo on a signed app (invalidates its seal).
            'hide_extensions': [],
            'show_status_bar': False, 'show_toolbar': False,
            'show_sidebar': False, 'show_pathbar': False,
        }
        temporary_output = stage / 'installer.dmg'
        dmgbuild.build_dmg(str(temporary_output), args.volume_name, settings=settings)
        run('hdiutil', 'verify', temporary_output)
        # Output is published only after successful image creation/verification.
        os.link(temporary_output, output)
    print(json.dumps({'dmg': str(output), 'volume': args.volume_name}, ensure_ascii=False))


if __name__ == '__main__':
    main()
