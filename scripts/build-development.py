#!/usr/bin/env python3
"""Build a separate development app, with no production data or update feed."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
POLICY = json.loads((ROOT / 'internal/distribution/policy.json').read_text())
identity = POLICY['debugIdentity']
if os.environ.get('SIGNING_IDENTITY', identity) != identity:
    raise SystemExit('Unexpected development signing identity; no fallback allowed.')
identities = subprocess.check_output(['security', 'find-identity', '-v', '-p', 'codesigning'], text=True)
if f'"{identity}"' not in identities:
    raise SystemExit('Fixed development identity unavailable.')
parent = Path.home() / 'Library/Caches/voicemore-development'
parent.mkdir(parents=True, exist_ok=True)
work = Path(tempfile.mkdtemp(prefix='source-', dir=parent))
names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
for raw in sorted(set(names)):
    if not raw:
        continue
    rel = Path(os.fsdecode(raw))
    if not (rel.suffix == '.go' or rel.as_posix() in ('go.mod', 'go.sum', 'mygo.json') or rel.parts[0] in ('internal', 'resources')):
        continue
    if rel.parts[:2] == ('resources', 'local-asr') and rel.name.endswith(('.tar.gz', '.tar.bz2')):
        continue
    original = ROOT / rel
    if not original.is_file():
        continue
    if original.is_symlink():
        raise SystemExit(f'Unexpected source symlink: {rel}')
    dest = work / rel
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(original, dest)
config = json.loads((work / 'mygo.json').read_text())
config.update(name=POLICY['debugName'], identifier=POLICY['debugBundleID'], out=str(ROOT / 'build/development'))
config.pop('updates', None)
config['macos']['signingIdentity'] = identity
config['macos'].pop('notarize', None)
(work / 'mygo.json').write_text(json.dumps(config, indent=2) + '\n')
env = dict(os.environ)
env.pop('MYGO_UPDATER_PRIVATE_KEY', None)
subprocess.run(['go', 'tool', 'mygo', 'build', '-platform', 'darwin/arm64', '-skip-dmg', '-sign', identity], cwd=work, env=env, check=True)
app = ROOT / 'build/development/darwin-arm64/Voicemore Dev.app'
subprocess.run(['codesign', '--verify', '--deep', '--strict', app], check=True)
actual = subprocess.check_output(['plutil', '-extract', 'CFBundleIdentifier', 'raw', app / 'Contents/Info.plist'], text=True).strip()
if actual != POLICY['debugBundleID']:
    raise SystemExit('Development bundle isolation failed.')
print(f'Development app: {app}\nData: ~/Library/Application Support/Voice Input Dev\nStable updates: disabled')
