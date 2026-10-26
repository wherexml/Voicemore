"""Shared helpers for the paired replay runners: WAV input, hashing, JSONL run logs, pacing.

Run log contract (one JSON object per line):
  {"event":"run", ...identity...}            first line: engine, args, wav sha256, model files, versions
  {"event":"sent", "audio_s", "t_s"}          every pacing chunk actually submitted
  {"event":"partial"|"final", "segment", "text", "t_s", "audio_sent_s", ...engine fields}
  {"event":"stop", "t_s", "audio_sent_s"}     end-of-audio signal (Qwen stop / sherpa input_finished)
  {"event":"done"|"error", "t_s", ...}
t_s is monotonic seconds since the first audio chunk was submitted. audio_sent_s is how much of
the WAV had been submitted when the event was received; it is NOT the time a word was spoken.
"""
import hashlib
import json
import platform
import sys
import time
import wave
from pathlib import Path

SR = 16000
CHUNK_S = 0.1  # pacing granularity: 100 ms of PCM16 per send (3200 bytes)
CHUNK_BYTES = int(SR * CHUNK_S) * 2


def read_wav(path):
    with wave.open(str(path)) as f:
        fmt = (f.getnchannels(), f.getsampwidth(), f.getframerate())
        if fmt != (1, 2, SR):
            sys.exit(f'{path}: expected mono PCM16 16 kHz, got channels/width/rate {fmt}')
        pcm = f.readframes(f.getnframes())
    return pcm


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for block in iter(lambda: f.read(1 << 20), b''):
            h.update(block)
    return h.hexdigest()


def file_identity(paths):
    return [{'path': str(p), 'bytes': Path(p).stat().st_size, 'sha256': sha256_file(p)} for p in paths]


def host_info():
    return {'python': sys.version.split()[0], 'platform': platform.platform(), 'machine': platform.machine()}


class RunLog:
    def __init__(self, path):
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        self.f = open(path, 'w', encoding='utf-8')
        self.t0 = None

    def start_clock(self):
        self.t0 = time.monotonic()

    def now(self):
        return 0.0 if self.t0 is None else time.monotonic() - self.t0

    def emit(self, event, **fields):
        rec = {'event': event, 't_s': round(self.now(), 4), **fields}
        self.f.write(json.dumps(rec, ensure_ascii=False) + '\n')
        self.f.flush()
        if event in ('partial', 'final', 'error', 'done', 'stop'):
            short = {k: v for k, v in rec.items() if k in ('event', 't_s', 'audio_sent_s', 'segment', 'text', 'message')}
            print(json.dumps(short, ensure_ascii=False), flush=True)
        return rec

    def close(self):
        self.f.close()


def chunks(pcm):
    for start in range(0, len(pcm), CHUNK_BYTES):
        yield start, pcm[start:start + CHUNK_BYTES]
