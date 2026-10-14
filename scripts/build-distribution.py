#!/usr/bin/env python3
"""Prepare a Developer ID candidate in an isolated source copy; never publish.

The fixed local-development configuration and runtime are not modified.
A candidate is NOT approved for distribution until notarization and E2E pass.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

IDENTITY = 'Developer ID Application: Skylance LLC (Apps) (T695TT4HT9)'
MAGIC = {bytes.fromhex(x) for x in ('feedface', 'cefaedfe', 'feedfacf', 'cffaedfe', 'cafebabe', 'bebafeca', 'cafebabf', 'bfbafeca')}
ROOT = Path(__file__).resolve().parent.parent

def run(*args, **kwargs):
    return subprocess.run([str(a) for a in args], check=True, **kwargs)

def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            result.update(chunk)
    return result.hexdigest()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-parent', type=Path, required=True)
    args = parser.parse_args()
    identities = subprocess.check_output(['security', 'find-identity', '-v', '-p', 'codesigning'], text=True)
    if f'"{IDENTITY}"' not in identities:
        raise SystemExit('Approved Developer ID identity unavailable; no fallback permitted.')
    archive = ROOT / 'resources/local-asr/runtime.tar.gz'
    expected = (ROOT / 'internal/localasr/assets/runtime.sha256').read_text().strip()
    if digest(archive) != expected:
        raise SystemExit('Source runtime checksum mismatch.')
    args.output_parent.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix='voicemore-distribution-', dir=args.output_parent.resolve()))
    print(f'Candidate workspace: {work}', flush=True)
    source = work / 'source'
    source.mkdir()
    # Explicit source/resource scope avoids copying .git, credentials or user data.
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
    inventory = {}
    for raw in sorted(set(names)):
        if not raw:
            continue
        rel = Path(os.fsdecode(raw))
        if not (rel.suffix == '.go' or rel.as_posix() in ('go.mod', 'go.sum', 'mygo.json', 'CHANGELOG.md') or rel.parts[0] in ('internal', 'resources')):
            continue
        original = ROOT / rel
        if not original.is_file():
            continue
        if original.is_symlink():
            raise SystemExit(f'Unexpected source symlink: {rel}')
        dest = source / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(original, dest)
        inventory[str(rel)] = digest(original)
    (work / 'source-sha256.json').write_text(json.dumps(inventory, indent=2))
    runtime_stage = work / 'runtime-stage'
    runtime_stage.mkdir()
    # Verify paths and symlinks before extracting this already checksummed archive.
    with tarfile.open(archive) as tar:
        for member in tar.getmembers():
            rel = Path(member.name)
            if rel.is_absolute() or '..' in rel.parts or not rel.parts or rel.parts[0] != 'runtime':
                raise SystemExit(f'Unsafe runtime entry: {member.name}')
            if member.isdev() or member.isfifo() or member.islnk():
                raise SystemExit(f'Unsupported runtime entry: {member.name}')
            if member.issym():
                target = (runtime_stage / rel.parent / member.linkname).resolve()
                if not target.is_relative_to(runtime_stage / 'runtime'):
                    raise SystemExit(f'Unsafe runtime symlink: {member.name}')
        if hasattr(tarfile, 'data_filter'):
            tar.extractall(runtime_stage, filter='data')
        else:
            tar.extractall(runtime_stage)  # Paths/types/links validated above.
    signed = []
    files = sorted(runtime_stage.rglob('*'), key=lambda p: len(p.parts), reverse=True)
    with (work / 'runtime-signing.log').open('w') as log:
        for path in files:
            if path.is_symlink() or not path.is_file():
                continue
            with path.open('rb') as f:
                magic = f.read(4)
            if magic not in MAGIC:
                continue
            run('codesign', '--force', '--options', 'runtime', '--timestamp', '--sign', IDENTITY, path, stdout=log, stderr=log)
            run('codesign', '--verify', '--strict', path, stdout=log, stderr=log)
            signed.append(str(path.relative_to(runtime_stage)))
        for path in files:
            if path.is_dir() and not path.is_symlink() and path.suffix in ('.app', '.framework', '.xpc', '.bundle'):
                if any((runtime_stage / rel).is_relative_to(path) for rel in signed):
                    run('codesign', '--force', '--options', 'runtime', '--timestamp', '--sign', IDENTITY, path, stdout=log, stderr=log)
    (work / 'runtime-signed-files.json').write_text(json.dumps(signed, indent=2))
    target = source / 'resources/local-asr/runtime.tar.gz'
    target.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, COPYFILE_DISABLE='1')
    # Never generate an updater manifest from this not-yet-notarized candidate.
    env.pop('MYGO_UPDATER_PRIVATE_KEY', None)
    run('tar', '-czf', target, '-C', runtime_stage, 'runtime', env=env)
    runtime_hash = digest(target)
    (source / 'internal/localasr/assets/runtime.sha256').write_text(runtime_hash + '\n')
    config_path = source / 'mygo.json'
    config = json.loads(config_path.read_text())
    config['out'] = str(work / 'build')
    config['macos']['signingIdentity'] = IDENTITY
    config['macos'].pop('notarize', None)
    config.get('updates', {}).pop('privateKey', None)
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    with (work / 'build.log').open('w') as log:
        run('go', 'tool', 'mygo', 'build', '-platform', 'darwin/arm64', '-sign', IDENTITY, cwd=source, env=env, stdout=log, stderr=log)
    app = work / 'build/darwin-arm64/Voicemore.app'
    with (work / 'app-signature.txt').open('w') as log:
        run('codesign', '--verify', '--deep', '--strict', app, stdout=log, stderr=log)
        run('codesign', '-d', '-r-', '--verbose=4', app, stdout=log, stderr=log)
    (work / 'candidate.json').write_text(json.dumps({'status': 'signed-not-notarized', 'app': str(app), 'runtime_sha256': runtime_hash, 'identity': IDENTITY, 'version': config['version'], 'source_sha256': inventory}, indent=2))
    print(f'Signed candidate ready, NOT notarized or published: {app}', flush=True)

if __name__ == '__main__':
    main()
