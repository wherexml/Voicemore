#!/usr/bin/env python3
"""Race-enabled native exit acceptance; never installs or publishes an app."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser()
parser.add_argument('--output', required=True, help='fresh isolated evidence directory')
args = parser.parse_args()
output = Path(args.output).resolve()
output.mkdir(mode=0o700, parents=True, exist_ok=False)
os.umask(0o077)
policy = json.loads((ROOT / 'internal/distribution/policy.json').read_text())
app = output / (policy['debugName'] + '.app')
binary = app / 'Contents/MacOS/persistence-exit-acceptance'
binary.parent.mkdir(parents=True)
with (output / 'build.log').open('w') as log:
    subprocess.run(['go', 'build', '-race', '-tags', 'integrityacceptance', '-o', str(binary), './cmd/persistence-exit-acceptance'], cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, check=True)
with (app / 'Contents/Info.plist').open('wb') as plist:
    plistlib.dump({'CFBundleIdentifier': policy['debugBundleID'], 'CFBundleName': policy['debugName'], 'CFBundleExecutable': binary.name, 'CFBundlePackageType': 'APPL', 'CFBundleVersion': '1', 'CFBundleShortVersionString': '0.0.0', 'LSUIElement': True}, plist)
with (output / 'signing.log').open('w') as log:
    subprocess.run(['codesign', '--force', '--sign', policy['debugIdentity'], str(app)], stdout=log, stderr=subprocess.STDOUT, check=True)
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], stdout=log, stderr=subprocess.STDOUT, check=True)
rows = []
for fault in ('index', 'audio', 'both', 'success', 'private'):
    with (output / (fault + '.log')).open('w') as log:
        run = subprocess.run([str(binary), '-output', str(output / fault), '-fault', fault], cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, timeout=20)
    result = output / fault / 'results.json'
    evidence = json.loads(result.read_text()) if result.exists() else {'passed': False, 'missingResult': True}
    row = {'fault': fault, 'exitCode': run.returncode, 'passed': run.returncode == 0 and evidence.get('passed') is True, 'evidence': str(result)}
    rows.append(row)
    print(json.dumps(row), flush=True)
sources = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in ('internal/session/privacy.go', 'internal/session/controller.go', 'internal/session/persistence_exit_acceptance.go', 'cmd/persistence-exit-acceptance/main.go')}
summary = {'passed': all(row['passed'] for row in rows), 'rows': rows, 'sourceSHA256': sources, 'debugBundleID': policy['debugBundleID'], 'signingIdentity': policy['debugIdentity'], 'binarySHA256': hashlib.sha256(binary.read_bytes()).hexdigest()}
(output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
sys.exit(0 if summary['passed'] else 1)
