#!/usr/bin/env python3
"""Prepare a Developer ID candidate in an isolated source copy; never publish.

The production configuration and source runtime are not modified.
A candidate is NOT approved for distribution until notarization and final-artifact checks pass.
"""
import subtitle_capture
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import time
import distribution_cache as runtime_cache

POLICY = json.loads((Path(__file__).resolve().parent.parent / 'internal/distribution/policy.json').read_text())
IDENTITY = POLICY['identity']
MAGIC = {bytes.fromhex(x) for x in ('feedface', 'cefaedfe', 'feedfacf', 'cffaedfe', 'cafebabe', 'bebafeca', 'cafebabf', 'bfbafeca')}
ROOT = Path(__file__).resolve().parent.parent

def run(*args, **kwargs):
    if args[0] == 'codesign' and '--timestamp' in args:
        # Apple occasionally returns a transient timestamp-service error.
        # Retry only that error; never drop the secure timestamp requirement.
        for attempt in range(5):
            result = subprocess.run([str(a) for a in args], capture_output=True, text=True)
            for name in ('stdout', 'stderr'):
                if kwargs.get(name):
                    kwargs[name].write(getattr(result, name))
                    kwargs[name].flush()
            if result.returncode == 0:
                return result
            if not any(message in result.stderr for message in ('timestamp service is not available', 'timestamp was expected but was not found')) or attempt == 4:
                raise subprocess.CalledProcessError(result.returncode, result.args)
            time.sleep(3 * (attempt + 1))
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
    parser.add_argument('--resume', type=Path, help='Resume an interrupted signing workspace')
    parser.add_argument('--cache-dir', type=Path, default=runtime_cache.DEFAULT_CACHE)
    args = parser.parse_args()
    config = json.loads((ROOT / 'mygo.json').read_text())
    if (config['identifier'] != POLICY['bundleID'] or config['name'] != 'Voicemore' or config['macos']['signingIdentity'] != IDENTITY or config['updates']['publicKey'] != POLICY['publicKey'] or config['updates'].get('github') != 'wherexml/Voicemore'):
        raise SystemExit('Production configuration differs from signing/update policy; refusing identity drift.')
    identities = subprocess.check_output(['security', 'find-identity', '-v', '-p', 'codesigning'], text=True)
    if f'"{IDENTITY}"' not in identities:
        raise SystemExit('Approved Developer ID identity unavailable; no fallback permitted.')
    archive = ROOT / 'resources/local-asr/runtime.tar.gz'
    expected = (ROOT / 'internal/localasr/assets/runtime.sha256').read_text().strip()
    if digest(archive) != expected:
        raise SystemExit('Source runtime checksum mismatch.')
    args.output_parent.mkdir(parents=True, exist_ok=True)
    work = args.resume.resolve() if args.resume else Path(tempfile.mkdtemp(prefix='voicemore-distribution-', dir=args.output_parent.resolve()))
    if args.resume and (work / 'candidate.json').exists():
        raise SystemExit('Completed candidates cannot be rebuilt in place.')
    print(f'Candidate workspace: {work}', flush=True)
    source = work / 'source'
    if args.resume and not (work / 'source-sha256.json').exists():
        raise SystemExit('Resume requires an existing source inventory.')
    source.mkdir(exist_ok=True)
    # Explicit source/resource scope avoids copying .git, credentials or user data.
    names = subprocess.check_output(['git', 'ls-files', '-co', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
    inventory = {}
    for raw in ([] if args.resume else sorted(set(names))):
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
    if args.resume:
        inventory = json.loads((work / 'source-sha256.json').read_text())
        for rel, checksum in inventory.items():
            # These two files are rewritten only inside the distribution copy.
            if rel not in ('mygo.json', 'internal/localasr/assets/runtime.sha256', 'internal/localasr/assets/runtime-manifest.json') and digest(source / rel) != checksum:
                raise SystemExit('Resume source changed: ' + rel)
    else:
        (work / 'source-sha256.json').write_text(json.dumps(inventory, indent=2))
    runtime_stage = work / 'runtime-stage'
    runtime_stage.mkdir(exist_ok=True)
    target = source / 'resources/local-asr/runtime.tar.gz'
    target.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, COPYFILE_DISABLE='1')
    env.pop('MYGO_UPDATER_PRIVATE_KEY', None)
    spec = runtime_cache.specification(expected)
    cached = None if args.resume else runtime_cache.restore(
        args.cache_dir, spec, runtime_stage, target, work / 'runtime-notary.json')
    if cached is None:
        print('Runtime cache MISS/resume: signing runtime.', flush=True)
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
            if args.resume:
                pass  # Preserve successfully timestamped files from this workspace.
            elif hasattr(tarfile, 'data_filter'):
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
                metadata = subprocess.run(['codesign', '-d', '--verbose=4', str(path)], capture_output=True, text=True)
                valid = subprocess.run(['codesign', '--verify', '--strict', str(path)], capture_output=True).returncode == 0
                if not (valid and ('Authority=' + IDENTITY) in metadata.stderr and 'Timestamp=' in metadata.stderr and 'runtime' in metadata.stderr):
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
    # Keep the runtime outside the app; it is a separate, pinned release asset.
    cache_archive = work / 'runtime-cache.tar.gz'
    shutil.copy2(target, cache_archive)
    cache_runtime_hash = digest(cache_archive)
    compact = target.with_suffix('.bz2')
    run('python3', ROOT / 'scripts/compress-runtime.py', target, compact)
    target.unlink()
    runtime_hash = digest(compact)
    runtime_name = f'voicemore-asr-runtime-{runtime_hash}.tar.bz2'
    target = work / runtime_name
    compact.rename(target)
    runtime_spec = dict(url=f'https://github.com/wherexml/Voicemore/releases/download/v{config["version"]}/{runtime_name}',
                        bytes=target.stat().st_size, sha256=runtime_hash, format='bzip2', compatible_installed_sha256=[cache_runtime_hash])
    descriptor = json.dumps(runtime_spec, indent=2) + '\n'
    (source / 'internal/localasr/assets/runtime-manifest.json').write_text(descriptor)
    (source / 'resources/local-asr/runtime-manifest.json').write_text(descriptor)
    (source / 'internal/localasr/assets/runtime.sha256').write_text(runtime_hash + '\n')
    # Built after the source inventory, from committed Swift source, so it never enters source_sha256.
    subtitle_capture.build(source, IDENTITY, json.loads((source / 'mygo.json').read_text())['identifier'] + '.audiotap', release=True)
    config_path = source / 'mygo.json'
    config = json.loads(config_path.read_text())
    config['out'] = str(work / 'build')
    config['macos']['signingIdentity'] = IDENTITY
    config['macos'].pop('notarize', None)
    config.get('updates', {}).pop('privateKey', None)
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    with (work / 'build.log').open('w') as log:
        for attempt in range(5):
            result = subprocess.run(['go', 'tool', 'mygo', 'build', '-platform', 'darwin/arm64', '-skip-dmg', '-sign', IDENTITY], cwd=source, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            log.write(result.stdout)
            log.flush()
            if result.returncode == 0:
                break
            if not any(message in result.stdout for message in ('timestamp service is not available', 'timestamp was expected but was not found')) or attempt == 4:
                raise subprocess.CalledProcessError(result.returncode, result.args)
            time.sleep(3 * (attempt + 1))
    app = work / 'build/darwin-arm64/Voicemore.app'
    run('go', 'run', './cmd/distribution-check', '--candidate', app, cwd=ROOT)
    with (work / 'app-signature.txt').open('w') as log:
        run('codesign', '--verify', '--deep', '--strict', app, stdout=log, stderr=log)
        subtitle_capture.verify_bundled(app)
        run('codesign', '-d', '-r-', '--verbose=4', app, stdout=log, stderr=log)
    (work / 'candidate.json').write_text(json.dumps({'status': 'signed-not-notarized', 'app': str(app), 'runtime_sha256': runtime_hash, 'runtime_archive': target.name, 'runtime_remote': True, 'runtime_asset': str(target), 'runtime_manifest': runtime_spec, 'cache_runtime_sha256': cache_runtime_hash, 'identity': IDENTITY, 'version': config['version'], 'source_sha256': inventory, 'runtime_cache_spec': spec, 'runtime_cache_hit': cached is not None}, indent=2))
    print(f'Signed candidate ready, NOT notarized or published: {app}', flush=True)

if __name__ == '__main__':
    main()
