#!/usr/bin/env python3
"""Prepare notarized releases, then publish with automatic artifact verification.

Fail closed on missing tickets, mismatched app/DMG, changed source, failed E2E,
wrong updater key, or existing release. Compare against committed source so
unrelated work in progress is never silently included. Never re-sign at publish.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import distribution_cache as runtime_cache
from datetime import datetime, timezone

ROOT = Path(__file__).resolve().parent.parent
REPO = 'wherexml/Voicemore'

def run(*args, **kw):
    return subprocess.run([str(a) for a in args], check=True, **kw)

def sha(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def read(path):
    return json.loads(Path(path).read_text())

def submit(work, name, path, profile):
    report = work / (name + '-notary.json')
    print('Checking/submitting Apple notarization:', name, flush=True)
    if report.exists() and report.stat().st_size:
        result = read(report)
        result = json.loads(subprocess.check_output(['xcrun', 'notarytool', 'info', result['id'], '--keychain-profile', profile, '--output-format', 'json']))
    else:
        with report.open('w') as f:
            run('xcrun', 'notarytool', 'submit', path, '--keychain-profile', profile, '--wait', '--output-format', 'json', stdout=f)
        result = read(report)
    if result.get('status') != 'Accepted':
        raise SystemExit(f'{name} not accepted: inspect {report}; do not publish.')
    print('Apple notarization accepted:', name, result['id'], flush=True)

def main():
    p = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    p.add_argument('--candidate', type=Path)
    p.add_argument('--source-ref', help='Committed source for an existing candidate; publication requires it already on the remote default branch')
    p.add_argument('--output-parent', type=Path, default=Path.home() / 'Library/Caches/voicemore-release')
    p.add_argument('--keychain-profile', default='voicemore-skylance')
    p.add_argument('--publish', action='store_true')
    p.add_argument('--e2e', type=Path, help='Optional native E2E report for this exact DMG')
    p.add_argument('--full-e2e', action='store_true', help='Require a complete native cross-Mac report')
    p.add_argument('--verify-only', action='store_true', help='Verify existing candidate without publishing')
    p.add_argument('--cache-dir', type=Path, default=runtime_cache.DEFAULT_CACHE)
    a = p.parse_args()
    if os.environ.get("SIGNING_IDENTITY", runtime_cache.IDENTITY) != runtime_cache.IDENTITY:
        raise SystemExit("Stable preparation requires the approved Developer ID; no signing fallback permitted.")
    if a.source_ref and not a.candidate:
        p.error('--source-ref requires --candidate')
    if a.verify_only and (not a.candidate or a.publish):
        p.error('--verify-only requires --candidate and cannot publish')
    if a.full_e2e and not a.e2e and (a.publish or a.verify_only):
        p.error('--full-e2e publication requires --e2e')
    os.chdir(ROOT)
    if a.candidate:
        work = a.candidate.resolve()
    else:
        if a.publish:
            # Fail before signing/uploading when the build would include WIP.
            changed = subprocess.check_output(['git', 'diff', '--name-only', 'HEAD', '-z']).split(b'\0')
            changed += subprocess.check_output(['git', 'ls-files', '--others', '--exclude-standard', '-z']).split(b'\0')
            pending = [os.fsdecode(n) for n in changed if n and (n.endswith(b'.go') or n in (b'go.mod', b'go.sum', b'mygo.json') or n.startswith((b'internal/', b'resources/')))]
            if pending:
                raise SystemExit('Commit the intended release source before automatic publication; pending: ' + ', '.join(sorted(set(pending))))
        a.output_parent.mkdir(parents=True, exist_ok=True)
        before = set(a.output_parent.glob('voicemore-distribution-*'))
        run('python3', ROOT / 'scripts/build-distribution.py', '--output-parent', a.output_parent, '--cache-dir', a.cache_dir)
        created = set(a.output_parent.glob('voicemore-distribution-*')) - before
        if len(created) != 1:
            raise SystemExit('Cannot uniquely identify candidate.')
        work = created.pop().resolve()
    c = read(work / 'candidate.json')
    app = Path(c['app'])
    run('go', 'run', './cmd/distribution-check', '--candidate', app, cwd=ROOT)
    prepared = False
    if not (work / 'final-candidate.json').exists():
        if a.verify_only:
            raise SystemExit('Final candidate missing; verification never builds.')
        for name, folder in [('runtime', work / 'runtime-stage/runtime'), ('app', app)]:
            archive = work / (name + '-notary.zip')
            if not archive.exists() and not (work / (name + '-notary.json')).exists():
                run('ditto', '-c', '-k', '--keepParent', folder, archive)
            submit(work, name, archive, a.keychain_profile)
            if name == 'runtime':
                runtime_cache.store(work, a.cache_dir, a.keychain_profile)
        run('python3', ROOT / 'scripts/finalize-distribution.py', work, '--keychain-profile', a.keychain_profile)
        prepared = True
    if not (a.publish or a.verify_only):
        print('Notarized candidate ready. Publish with --candidate', work, '--publish; use --full-e2e --e2e REPORT when native retesting is needed.')
        return
    final = read(work / 'final-candidate.json')
    dmg = Path(final['dmg'])
    version = c['version']
    source_commit = subprocess.check_output(['git', 'rev-parse', '--verify', (a.source_ref or 'HEAD') + '^{commit}'], text=True).strip()
    config = json.loads(subprocess.check_output(['git', 'show', source_commit + ':mygo.json']))
    if version != config['version']:
        raise SystemExit('Candidate version differs from source.')
    for rel, digest in c['source_sha256'].items():
        if rel == 'CHANGELOG.md':
            continue  # Release notes are not compiled into the application.
        committed = subprocess.check_output(['git', 'show', source_commit + ':' + rel])
        if hashlib.sha256(committed).hexdigest() != digest:
            raise SystemExit('Candidate differs from committed source: ' + rel)
    names = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', source_commit], text=True).splitlines()
    compiled = {n for n in names if n.endswith('.go') or n in ('go.mod', 'go.sum', 'mygo.json') or n.startswith(('internal/', 'resources/'))}
    if compiled != set(c['source_sha256']) - {'CHANGELOG.md'}:
        raise SystemExit('Committed source inventory differs from candidate.')
    if sha(app / 'Contents/Resources/local-asr/runtime.tar.gz') != c['runtime_sha256']:
        raise SystemExit('Bundled runtime differs from signed candidate.')
    report = read(a.e2e) if a.e2e else None
    if a.e2e and not isinstance(report, dict):
        raise SystemExit('Native E2E report must be a JSON object.')
    required = ('quarantine', 'gatekeeper', 'installed_app_matches', 'launch', 'permissions', 'model', 'dictation', 'history', 'relaunch')
    if report is not None and (report.get('dmg_sha256') != sha(dmg) or report.get('version') != version or not report.get('host') or any(report.get('checks', {}).get(k) is not True for k in required)):
        raise SystemExit('Native E2E report missing, failed, or for a different artifact.')
    for name in ('app', 'runtime', 'dmg'):
        result = read(work / (name + '-notary.json'))
        live = result if prepared else json.loads(subprocess.check_output(['xcrun', 'notarytool', 'info', result['id'], '--keychain-profile', a.keychain_profile, '--output-format', 'json']))
        if live.get('status') != 'Accepted':
            raise SystemExit(name + ' notarization not accepted.')
    if not prepared:
        run(ROOT / 'scripts/verify-distribution.sh', app, dmg, work / 'publish-evidence')
    verification = dict(version=version, commit=source_commit, dmg_sha256=sha(dmg), status='passed', mode='full-e2e' if report else 'artifact', runtime_cache_hit=c.get('runtime_cache_hit', False))
    (work / 'verification.json').write_text(json.dumps(verification, indent=2) + '\n')
    if a.verify_only:
        print('PASS: automatic release verification; no publication performed.')
        return
    tag = 'v' + version
    if subprocess.run(['gh', 'release', 'view', tag, '--repo', REPO], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise SystemExit('Release exists; never overwrite published assets.')
    stage = work / 'release-assets'
    stage.mkdir(exist_ok=True)
    target = stage / f'Voicemore-{version}-arm64.dmg'
    shutil.copy2(dmg, target)
    archive = stage / f'voicemore-{version}-darwin-arm64.tar.gz'
    with tarfile.open(archive, 'w:gz', format=tarfile.PAX_FORMAT) as tar:
        tar.add(app, arcname=app.name)
    # Check the exact update payload, including its stapled ticket, after extraction.
    with tempfile.TemporaryDirectory(prefix='voicemore-update-check-') as temp:
        with tarfile.open(archive) as tar:
            tar.extractall(temp)
        run('go', 'run', './cmd/distribution-check', '--candidate', Path(temp) / app.name, cwd=ROOT)
        run('xcrun', 'stapler', 'validate', Path(temp) / app.name)
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey
    from cryptography.hazmat.primitives import serialization
    key_file = Path(os.environ.get('VOICEMORE_UPDATE_KEY_FILE', str(Path.home() / '.config/voicemore/update-keys/mygo-update.key')))
    raw = base64.b64decode(os.environ.get('MYGO_UPDATER_PRIVATE_KEY') or key_file.read_text().strip())
    if len(raw) != 64:
        raise SystemExit('Invalid updater key length.')
    key = Ed25519PrivateKey.from_private_bytes(raw[:32])
    public = base64.b64decode(config['updates']['publicKey'])
    if key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw) != public:
        raise SystemExit('Updater key does not match pinned public key.')
    signature = key.sign(bytes.fromhex(sha(archive)))
    Ed25519PublicKey.from_public_bytes(public).verify(signature, bytes.fromhex(sha(archive)))
    changelog = subprocess.check_output(['git', 'show', source_commit + ':CHANGELOG.md'], text=True)
    notes = changelog.split('## ' + version + '\n', 1)[1].split('\n## ', 1)[0].strip()
    manifest = dict(version=version, notes=notes, date=datetime.now(timezone.utc).isoformat(), url=f'https://github.com/{REPO}/releases/download/{tag}/{archive.name}', size=archive.stat().st_size, signature=base64.b64encode(signature).decode())
    manifest_path = stage / 'update-darwin-arm64.json'
    manifest_path.write_text(json.dumps(manifest, indent=2) + '\n')
    checksums = stage / 'SHA256SUMS.txt'
    checksums.write_text(''.join(f'{sha(f)}  {f.name}\n' for f in (target, archive, manifest_path)))
    notes_file = work / 'release-notes.md'
    notes_file.write_text(notes + '\n\nApple Silicon / macOS 26.2+. Developer ID signed and Apple notarized, with stapled app and DMG tickets. Update archive authenticated with the existing Ed25519 key.\n\n首次安装下载 DMG，拖入 Applications。此版本已完成 Developer ID 正式签名与 Apple 公证。由开发签名旧版升级后，macOS 可能要求重新确认麦克风、辅助功能与输入监控权限。\n')
    head = source_commit
    if a.source_ref:
        # A concurrent main may have moved ahead. Never move it backwards or
        # publish an unpushed source commit through the explicit-ref path.
        run('git', 'fetch', '--no-tags', 'origin', 'HEAD')
        run('git', 'merge-base', '--is-ancestor', head, 'FETCH_HEAD')
    existing = subprocess.run(['git', 'rev-list', '-n', '1', tag], capture_output=True, text=True)
    if existing.returncode == 0:
        if existing.stdout.strip() != head:
            raise SystemExit('Existing tag differs from selected source commit.')
    else:
        run('git', 'tag', '-a', tag, head, '-m', 'Voicemore ' + version + ' notarized release')
    if a.source_ref:
        run('git', 'push', 'origin', tag)
    else:
        run('git', 'push', 'origin', 'HEAD', tag)
    run('gh', 'release', 'create', tag, '--repo', REPO, '--verify-tag', '--draft', '--title', 'Voicemore ' + version, '--notes-file', notes_file, target, archive, manifest_path, checksums)
    # Verify GitHub stored the exact assets before exposing the stable update feed.
    with tempfile.TemporaryDirectory(prefix='voicemore-upload-check-') as temp:
        run('gh', 'release', 'download', tag, '--repo', REPO, '--dir', temp)
        for f in (target, archive, manifest_path, checksums):
            if sha(Path(temp) / f.name) != sha(f):
                raise SystemExit('Uploaded asset differs; release stays draft.')
    run('gh', 'release', 'edit', tag, '--repo', REPO, '--draft=false', '--prerelease=false', '--latest')
    receipt = dict(status='published', version=version, commit=head,
                   url=f'https://github.com/{REPO}/releases/tag/{tag}',
                   dmg_sha256=sha(dmg), acceptance_mode=verification['mode'], e2e_report_sha256=sha(a.e2e) if a.e2e else None)
    (work / 'published-release.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print('Published notarized stable release:', tag)

if __name__ == '__main__':
    main()
