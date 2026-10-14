#!/usr/bin/env python3
"""Staple an accepted candidate, create/notarize its DMG, and verify it.

Requires app-notary.json and runtime-notary.json from notarytool submit --wait.
Never publishes, changes development configuration, or grants exceptions.
"""
import argparse
import json
from pathlib import Path
import subprocess

IDENTITY = 'Developer ID Application: Skylance LLC (Apps) (T695TT4HT9)'

def run(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('workspace', type=Path)
    parser.add_argument('--keychain-profile', required=True)
    args = parser.parse_args()
    work = args.workspace.resolve()
    candidate = json.loads((work / 'candidate.json').read_text())
    for name in ('app', 'runtime'):
        result = json.loads((work / (name + '-notary.json')).read_text())
        if result.get('status') != 'Accepted':
            raise SystemExit(name + ' notarization is not Accepted; stopping.')
    app = Path(candidate['app'])
    run('xcrun', 'stapler', 'staple', app)
    run('xcrun', 'stapler', 'validate', app)
    stage = work / 'final-dmg-stage'
    stage.mkdir()  # Fail rather than silently overwrite a previous finalization.
    run('ditto', app, stage / app.name)
    (stage / 'Applications').symlink_to('/Applications')
    image = work / ('Voicemore-' + candidate['version'] + '-arm64-notarized.dmg')
    run('hdiutil', 'create', '-volname', 'Voicemore', '-srcfolder', stage,
        '-format', 'UDZO', image)
    run('codesign', '--force', '--timestamp', '--sign', IDENTITY, image)
    with (work / 'dmg-notary.json').open('w') as output:
        run('xcrun', 'notarytool', 'submit', image, '--keychain-profile',
            args.keychain_profile, '--wait', '--output-format', 'json', stdout=output)
    result = json.loads((work / 'dmg-notary.json').read_text())
    if result.get('status') != 'Accepted':
        raise SystemExit('DMG notarization is not Accepted; stopping.')
    run('xcrun', 'stapler', 'staple', image)
    run(Path(__file__).with_name('verify-distribution.sh'), app, image, work / 'evidence')
    candidate.update(status='notarized-local-gate-passed-e2e-pending', dmg=str(image))
    (work / 'final-candidate.json').write_text(json.dumps(candidate, indent=2) + '\n')
    print('Notarized candidate ready for cross-Mac E2E; not published:', image)

if __name__ == '__main__':
    main()
