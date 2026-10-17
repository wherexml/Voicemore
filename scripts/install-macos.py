#!/usr/bin/env python3
"""Install a notarized candidate only when the installed permission identity matches.

Quit Voicemore first. This command never migrates a development-signed production
bundle implicitly; such a migration can require one final macOS authorization.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('candidate', type=Path, help='completed release workspace')
p.add_argument('--target', type=Path, default=Path.home() / 'Applications/Voicemore.app')
a = p.parse_args()
work = a.candidate.resolve()
final = json.loads((work / 'final-candidate.json').read_text())
app = Path(final['app'])
target = a.target.absolute()
if target.name != 'Voicemore.app' or target.is_symlink():
    raise SystemExit('Target must be a real Voicemore.app path.')
if final.get('status') != 'notarized-local-gate-passed':
    raise SystemExit('Candidate has not passed final distribution checks.')
subprocess.run([ROOT / 'scripts/verify-distribution.sh', app, final['dmg'], work / 'install-evidence'], check=True)
check = ['go', 'run', './cmd/distribution-check', '--candidate', str(app)]
if target.exists():
    check += ['--installed', str(target)]
subprocess.run(check, cwd=ROOT, check=True)
processes = subprocess.run(['pgrep', '-f', '^' + re.escape(str(target / 'Contents/MacOS')) + '/'], capture_output=True)
if processes.returncode == 0:
    raise SystemExit('Quit the installed Voicemore before replacing it.')
target.parent.mkdir(parents=True, exist_ok=True)
stage = Path(tempfile.mkdtemp(prefix='.Voicemore-install-', dir=target.parent))
try:
    staged = stage / 'Voicemore.app'
    subprocess.run(['ditto', app, staged], check=True)
    staged_check = ['go', 'run', './cmd/distribution-check', '--candidate', str(staged)]
    if target.exists():
        staged_check += ['--installed', str(target)]
    subprocess.run(staged_check, cwd=ROOT, check=True)
    subprocess.run(['spctl', '--assess', '--type', 'execute', staged], check=True)
    backup = stage.with_name(stage.name + '.previous.app')
    had_old = target.exists()
    if had_old:
        os.rename(target, backup)
    try:
        os.rename(staged, target)
    except BaseException:
        if had_old:
            os.rename(backup, target)
        raise
    print(f'Installed and verified: {target}')
    if had_old:
        print(f'Previous app retained: {backup}')
finally:
    shutil.rmtree(stage)
