"""Content-addressed cache of signed, Apple-accepted distribution runtimes."""
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile

IDENTITY = json.loads((Path(__file__).resolve().parent.parent / 'internal/distribution/policy.json').read_text())['identity']
POLICY = 'runtime-v1:hardened:secure-timestamp:no-extra-entitlements'
MAGIC = {bytes.fromhex(x) for x in ('feedface', 'cefaedfe', 'feedfacf', 'cffaedfe', 'cafebabe', 'bebafeca', 'cafebabf', 'bfbafeca')}
DEFAULT_CACHE = Path.home() / 'Library/Caches/voicemore-release/runtime-cache'


def sha(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def specification(source_hash):
    identities = subprocess.check_output(['security', 'find-identity', '-v', '-p', 'codesigning'], text=True)
    found = re.findall(r'\b([A-Fa-f0-9]{40}) "' + re.escape(IDENTITY) + '"', identities)
    if len(set(found)) != 1:
        raise SystemExit('Exactly one approved Developer ID identity is required.')
    return dict(source_sha256=source_hash, certificate_sha1=found[0].upper(),
                identity=IDENTITY, platform='darwin-' + platform.machine(), policy=POLICY)


def key(spec):
    return hashlib.sha256(json.dumps(spec, sort_keys=True).encode()).hexdigest()


def extract(archive, destination):
    destination = Path(destination).resolve()
    with tarfile.open(archive) as tar:
        for m in tar.getmembers():
            rel = Path(m.name)
            if rel.is_absolute() or '..' in rel.parts or not rel.parts or rel.parts[0] != 'runtime':
                raise SystemExit('Unsafe runtime path: ' + m.name)
            if not (m.isfile() or m.isdir() or m.issym()):
                raise SystemExit('Unsupported runtime entry: ' + m.name)
            if m.issym() and not (destination / rel.parent / m.linkname).resolve().is_relative_to(destination / 'runtime'):
                raise SystemExit('Unsafe runtime link: ' + m.name)
        if hasattr(tarfile, 'data_filter'):
            tar.extractall(destination, filter='data')
        else:
            tar.extractall(destination)


def verify_runtime(root, certificate_sha1):
    count = 0
    with tempfile.TemporaryDirectory(prefix='voicemore-cert-check-') as temp:
        prefix = str(Path(temp) / 'cert')
        for p in Path(root).rglob('*'):
            if p.is_symlink() or not p.is_file():
                continue
            with p.open('rb') as f:
                if f.read(4) not in MAGIC:
                    continue
            subprocess.run(['codesign', '--verify', '--strict', str(p)], check=True, capture_output=True)
            leaf = Path(prefix + '0')
            if leaf.exists():
                leaf.unlink()
            detail = subprocess.check_output(['codesign', '-d', '--extract-certificates=' + prefix, '--verbose=4', str(p)], stderr=subprocess.STDOUT, text=True)
            if 'Authority=' + IDENTITY not in detail or 'Timestamp=' not in detail or 'runtime' not in detail:
                raise SystemExit('Cached runtime signature policy mismatch: ' + str(p))
            if not leaf.exists() or hashlib.sha1(leaf.read_bytes()).hexdigest().upper() != certificate_sha1:
                raise SystemExit('Cached runtime certificate fingerprint mismatch: ' + str(p))
            count += 1
    if count == 0:
        raise SystemExit('No signed Mach-O files found in runtime.')
    return count


def restore(cache, spec, stage, target, report):
    entry = Path(cache) / key(spec)
    if not entry.exists():
        return None
    meta = json.loads((entry / 'receipt.json').read_text())
    archive = entry / 'runtime.tar.gz'
    if meta['spec'] != spec or meta['notarization'].get('status') != 'Accepted' or sha(archive) != meta['runtime_sha256']:
        raise SystemExit('Runtime cache invalid; refusing reuse: ' + str(entry))
    if any(Path(stage).iterdir()):
        raise SystemExit('Cache restore requires an empty runtime stage.')
    extract(archive, stage)
    count = verify_runtime(Path(stage) / 'runtime', spec['certificate_sha1'])
    if count != meta['mach_o_count']:
        raise SystemExit('Runtime cache Mach-O inventory mismatch.')
    Path(target).parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(archive, target)
    Path(report).write_text(json.dumps(meta['notarization'], indent=2) + '\n')
    print('Runtime cache HIT:', key(spec), '(' + str(count) + ' signatures verified; no re-sign or re-submit)', flush=True)
    return meta


def store(work, cache=DEFAULT_CACHE, profile='voicemore-skylance'):
    work = Path(work)
    c = json.loads((work / 'candidate.json').read_text())
    report = json.loads((work / 'runtime-notary.json').read_text())
    live = json.loads(subprocess.check_output(['xcrun', 'notarytool', 'info', report['id'], '--keychain-profile', profile, '--output-format', 'json']))
    if live.get('status') != 'Accepted':
        raise SystemExit('Only Apple-accepted runtimes may enter the cache.')
    source_hash = (work / 'source-sha256.json')
    inventory = json.loads(source_hash.read_text())
    # The pinned digest is in the ORIGINAL source inventory; the copy was rewritten.
    # New candidates record the actual input hash directly. Older candidates can
    # be seeded only when their pinned digest matches the current repository.
    if 'runtime_cache_spec' in c:
        spec = c['runtime_cache_spec']
    else:
        root = Path(__file__).resolve().parent.parent
        pin = root / 'internal/localasr/assets/runtime.sha256'
        if sha(pin) != inventory['internal/localasr/assets/runtime.sha256']:
            raise SystemExit('Legacy candidate runtime pin changed; cannot seed.')
        spec = specification(pin.read_text().strip())
    if spec != specification(spec['source_sha256']):
        raise SystemExit('Signing identity/policy changed; do not seed stale runtime.')
    archive = work / 'source/resources/local-asr/runtime.tar.gz'
    if sha(archive) != c['runtime_sha256']:
        raise SystemExit('Candidate runtime archive changed.')
    cache = Path(cache)
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    entry = cache / key(spec)
    if entry.exists():
        meta = json.loads((entry / 'receipt.json').read_text())
        if meta['runtime_sha256'] != sha(archive) or sha(entry / 'runtime.tar.gz') != sha(archive):
            raise SystemExit('Divergent cache entry; inspect instead of overwriting.')
        return entry
    with tempfile.TemporaryDirectory(prefix='.verify-', dir=cache) as tmp:
        extract(archive, tmp)
        count = verify_runtime(Path(tmp) / 'runtime', spec['certificate_sha1'])
    temp = Path(tempfile.mkdtemp(prefix='.entry-', dir=cache))
    try:
        shutil.copy2(archive, temp / 'runtime.tar.gz')
        (temp / 'receipt.json').write_text(json.dumps(dict(spec=spec, runtime_sha256=sha(archive), notarization=live, mach_o_count=count), indent=2) + '\n')
        temp.rename(entry)
    finally:
        if temp.exists():
            shutil.rmtree(temp)
    print('Cached accepted runtime:', entry, flush=True)
    return entry


if __name__ == '__main__':
    import argparse
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--seed-workspace', type=Path, required=True)
    p.add_argument('--cache-dir', type=Path, default=DEFAULT_CACHE)
    p.add_argument('--keychain-profile', default='voicemore-skylance')
    a = p.parse_args()
    store(a.seed_workspace, a.cache_dir, a.keychain_profile)
