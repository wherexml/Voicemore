"""Engine A: replay a captured WAV in real time through the managed Qwen worker's /ws protocol.

Starts an isolated worker child on its own port (default 48138) using the installed runtime,
worker source and model read-only (VOICE_INPUT_PARENT = this process, so it exits with us).
--variant baseline   installed worker as-is (current 30 s target / 32 s hard segment limit)
--variant subtitle-short   a COPY of the installed worker directory placed under --work-dir with only
                     MAX_SEGMENT / MAX_SEGMENT_EXTENSION changed; the installed worker is never edited.
Protocol (internal/localasr/assets/server.py): ready → PCM16 binary chunks → {"type":"stop"} →
partial/final/done/error. Audio stays on 127.0.0.1.
"""
import argparse
import asyncio
import json
import os
import re
import secrets
import shutil
import socket
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path

import websockets

sys.path.insert(0, str(Path(__file__).resolve().parent))
from common import CHUNK_S, SR, RunLog, chunks, file_identity, host_info, read_wav, sha256_file  # noqa: E402

DEFAULT_ROOT = Path.home() / 'Library/Application Support/Voice Input/local-asr'


def port_free(port):
    with socket.socket() as s:
        return s.connect_ex(('127.0.0.1', port)) != 0


def health(port, timeout=2):
    with urllib.request.urlopen(f'http://127.0.0.1:{port}/health', timeout=timeout) as f:
        return json.load(f)


def prepare_variant(root, work, target_s, extension_s):
    """Copy the installed worker and patch only the segment bounds. Aborts if the source differs."""
    src = root / 'worker'
    dst = work / 'qwen-subtitle-short-worker'
    if dst.exists():
        shutil.rmtree(dst)
    shutil.copytree(src, dst, ignore=shutil.ignore_patterns('__pycache__'))
    server = dst / 'server.py'
    text = server.read_text(encoding='utf-8')
    patched, n1 = re.subn(r'^MAX_SEGMENT=30\b', f'MAX_SEGMENT={target_s}', text, flags=re.M)
    patched, n2 = re.subn(r'^MAX_SEGMENT_EXTENSION=2\b', f'MAX_SEGMENT_EXTENSION={extension_s}', patched, flags=re.M)
    if (n1, n2) != (1, 1):
        sys.exit('installed server.py does not contain the expected MAX_SEGMENT=30 / MAX_SEGMENT_EXTENSION=2 lines; refusing to guess')
    server.write_text(patched, encoding='utf-8')
    return dst


def start_worker(root, worker_dir, port, logdir):
    py = root / 'runtime/bin/python3.11'
    for p in (py, worker_dir / 'run.py', worker_dir / 'server.py', root / 'model'):
        if not p.exists():
            sys.exit(f'missing {p}')
    if not port_free(port):
        sys.exit(f'port {port} is in use; refusing to share it')
    token = secrets.token_hex(16)
    env = {'HOME': os.environ['HOME'], 'PATH': '/usr/bin:/bin', 'LANG': 'en_US.UTF-8',
           'HF_HUB_OFFLINE': '1', 'TRANSFORMERS_OFFLINE': '1', 'TOKENIZERS_PARALLELISM': 'false',
           'PYTHONUNBUFFERED': '1', 'VOICE_INPUT_ASR_DATA': str(root), 'VOICE_INPUT_PORT': str(port),
           'VOICE_INPUT_PARENT': str(os.getpid()), 'VOICE_INPUT_INSTANCE': token}
    out = open(logdir / f'worker-{port}.stdout.log', 'w')
    err = open(logdir / f'worker-{port}.stderr.log', 'w')
    started = time.monotonic()
    proc = subprocess.Popen([str(py), '-I', str(worker_dir / 'run.py')], env=env, stdout=out, stderr=err,
                            cwd=str(worker_dir))
    while True:
        if proc.poll() is not None:
            sys.exit(f'worker exited with {proc.returncode}; see {err.name}')
        try:
            h = health(port)
            if h.get('instance') == token and h.get('ready'):
                return proc, h, time.monotonic() - started
        except OSError:
            pass
        if time.monotonic() - started > 180:
            proc.terminate()
            sys.exit('worker not ready within 180 s')
        time.sleep(0.25)


def stop_worker(proc):
    if proc and proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(10)
        except subprocess.TimeoutExpired:
            proc.kill()


async def session(url, pcm, log, flush_timeout):
    audio_total = len(pcm) / 2 / SR
    sent_bytes = 0
    async with websockets.connect(url, max_size=1 << 20) as ws:
        ready = json.loads(await ws.recv())
        if ready.get('type') != 'ready':
            log.emit('error', stage='ready', message=json.dumps(ready, ensure_ascii=False))
            return False
        log.emit('ready', worker=ready)
        done = asyncio.Event()
        ok = True

        async def receive():
            nonlocal ok
            async for raw in ws:
                e = json.loads(raw)
                kind = e.get('type')
                fields = {'audio_sent_s': round(sent_bytes / 2 / SR, 3)}
                if kind in ('partial', 'final'):
                    log.emit(kind, segment=e.get('segment'), text=e.get('text', ''), engine_audio_s=e.get('audio_s'),
                             decode_s=e.get('decode_s'), worker_elapsed_s=e.get('elapsed_s'),
                             hints_suppressed=e.get('hints_suppressed'), **fields)
                elif kind == 'done':
                    log.emit('done', worker_elapsed_s=e.get('elapsed_s'), **fields)
                    done.set()
                    return
                elif kind == 'error':
                    ok = False
                    log.emit('error', stage='worker', message=e.get('message'), **fields)
                    done.set()
                    return
            done.set()

        rx = asyncio.create_task(receive())
        log.start_clock()
        t0 = time.monotonic()
        for start, chunk in chunks(pcm):
            # Absolute schedule: chunk k leaves at t0 + k*CHUNK_S, so slow sends do not drift.
            delay = t0 + (start / 2 / SR) - time.monotonic()
            if delay > 0:
                await asyncio.sleep(delay)
            await ws.send(chunk)
            sent_bytes = start + len(chunk)
            log.emit('sent', audio_s=round(sent_bytes / 2 / SR, 3),
                     schedule_lag_s=round(time.monotonic() - t0 - start / 2 / SR, 4))
        # Wait out the final chunk's duration so the stop arrives when that audio would have ended.
        tail = t0 + audio_total - time.monotonic()
        if tail > 0:
            await asyncio.sleep(tail)
        await ws.send(json.dumps({'type': 'stop'}))
        log.emit('stop', audio_sent_s=round(sent_bytes / 2 / SR, 3))
        try:
            await asyncio.wait_for(done.wait(), flush_timeout)
        except asyncio.TimeoutError:
            ok = False
            log.emit('error', stage='flush', message=f'no done within {flush_timeout} s after stop')
        rx.cancel()
        return ok


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--wav', required=True)
    ap.add_argument('--out', required=True, help='run JSONL path')
    ap.add_argument('--root', default=str(DEFAULT_ROOT), help='managed local-asr directory (read-only)')
    ap.add_argument('--port', type=int, default=48138)
    ap.add_argument('--language', default='zh', choices=['zh', 'auto', 'en'])
    ap.add_argument('--variant', default='baseline', choices=['baseline', 'subtitle-short'])
    ap.add_argument('--short-target-s', type=int, default=8)
    ap.add_argument('--short-extension-s', type=int, default=1)
    ap.add_argument('--work-dir', help='where the subtitle-short worker copy is placed (default: next to --out)')
    ap.add_argument('--flush-timeout', type=float, default=120)
    args = ap.parse_args()
    if args.port == 48132:
        sys.exit('48132 is the production worker port; use an isolated port')

    root = Path(args.root).expanduser()
    out = Path(args.out).resolve()
    logdir = out.parent
    logdir.mkdir(parents=True, exist_ok=True)
    pcm = read_wav(args.wav)

    worker_dir = root / 'worker'
    if args.variant == 'subtitle-short':
        worker_dir = prepare_variant(root, Path(args.work_dir or logdir).resolve(), args.short_target_s, args.short_extension_s)

    log = RunLog(out)
    model_files = sorted(p for p in (root / 'model').iterdir() if p.is_file())
    log.emit('run', engine=f'qwen-{args.variant}', argv=sys.argv, wav=str(Path(args.wav).resolve()),
             wav_sha256=sha256_file(args.wav), audio_s=len(pcm) / 2 / SR, chunk_s=CHUNK_S, paced='realtime',
             language=args.language, port=args.port, worker_dir=str(worker_dir),
             worker_source=file_identity([worker_dir / 'server.py', worker_dir / 'run.py', worker_dir / 'speech_gate.py']),
             model_files=file_identity(model_files),
             installed_revision=(root / 'installed.json').read_text().strip() if (root / 'installed.json').exists() else None,
             host=host_info(), websockets=websockets.__version__)
    proc = None
    ok = False
    try:
        proc, h, load_s = start_worker(root, worker_dir, args.port, logdir)
        log.emit('worker_ready', health=h, load_s=round(load_s, 3), pid=proc.pid)
        url = f'ws://127.0.0.1:{args.port}/ws?' + urllib.parse.urlencode({'language': args.language})
        ok = asyncio.run(session(url, pcm, log, args.flush_timeout))
    except Exception as e:  # recorded, then re-raised for a non-zero exit
        log.emit('error', stage='runner', message=repr(e))
        raise
    finally:
        stop_worker(proc)
        log.emit('worker_stopped')
        log.close()
    sys.exit(0 if ok else 1)


if __name__ == '__main__':
    main()
