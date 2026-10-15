"""Real-model WebSocket regression for bounded segment tails.

Failure cases (frozen before repair): prompts echoed from weak short tails;
short replies dropped; hard-limit overflow; repeated or missing finals;
empty/cancelled sessions leave the service busy; silent sessions hallucinate.
Inputs are local PCM16/16kHz mono WAVs. No microphone or native paste coverage.
"""
import argparse
import asyncio
import hashlib
import json
from pathlib import Path
import time
import urllib.parse
import urllib.request
import wave

import websockets


def read_pcm(path):
    with wave.open(str(path)) as f:
        assert (f.getnchannels(), f.getsampwidth(), f.getframerate()) == (1, 2, 16000)
        return f.readframes(f.getnframes())


async def replay(url, pcm, realtime=False, cancel=False):
    events = []
    async with websockets.connect(url) as ws:
        ready = json.loads(await ws.recv())
        assert ready['type'] == 'ready', ready
        events.append(ready)

        async def receive():
            async for raw in ws:
                event = json.loads(raw)
                events.append(event)
                if event['type'] == 'error':
                    raise RuntimeError(event)
                if event['type'] == 'done':
                    return

        task = asyncio.create_task(receive())
        try:
            for start in range(0, len(pcm), 3200):
                chunk = pcm[start:start + 3200]
                await ws.send(chunk)
                await asyncio.sleep(len(chunk) / 32000 if realtime else 0.002)
            if cancel:
                await ws.close()
            else:
                await ws.send(json.dumps({'type': 'stop'}))
            await asyncio.wait_for(task, 120)
        finally:
            if not task.done():
                task.cancel()
    return events


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--url', required=True)
    ap.add_argument('--sample', required=True, help='recording spanning the 30 second boundary')
    ap.add_argument('--short', required=True, help='real short reply WAV')
    ap.add_argument('--hotwords', required=True, help='JSON list of terms')
    ap.add_argument('--out', required=True)
    args = ap.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    terms = json.loads(Path(args.hotwords).read_text())
    url = args.url + '?' + urllib.parse.urlencode({'hotwords': ','.join(terms)})
    long_pcm, short_pcm = read_pcm(args.sample), read_pcm(args.short)
    scenarios = [('boundary', long_pcm, False, False),
                 ('short-reply', short_pcm, True, False),
                 ('silence', bytes(32000), False, False),
                 ('cancel', short_pcm[:3200], False, True),
                 ('after-cancel', short_pcm, False, False)]
    results = []
    for name, pcm, realtime, cancel in scenarios:
        started = time.monotonic()
        events = await replay(url, pcm, realtime, cancel)
        finals = [e for e in events if e['type'] == 'final']
        texts = [e['text'] for e in events if e['type'] in ('partial', 'final')]
        # Detect verbatim hints in bulk; a single legitimately spoken term is allowed.
        leaked = any(any(', '.join(terms[i:i + 4]).lower() in t.lower()
                         for i in range(max(0, len(terms) - 3))) for t in texts)
        checks = {'no_hint_list': not leaked,
                  'bounded_audio': all(e['audio_s'] <= 32.032 for e in events if 'audio_s' in e),
                  'unique_final_ids': len({e['segment'] for e in finals}) == len(finals)}
        if name == 'boundary':
            checks.update(one_final=len(finals) == 1,
                          substantive_output=bool(finals and 'icon' in finals[0]['text'].lower()))
        if name in ('short-reply', 'after-cancel'):
            checks['short_reply_preserved'] = bool(finals and any(e['text'].strip('。 .') == '有' for e in finals))
        if name == 'silence':
            checks['no_silence_text'] = not texts
        result = dict(name=name, checks=checks, passed=all(checks.values()),
                      seconds=time.monotonic() - started,
                      audio_sha256=hashlib.sha256(pcm).hexdigest(), events=events)
        results.append(result)
        (out / 'report.json').write_text(json.dumps({'results': results}, ensure_ascii=False, indent=2))
        print(json.dumps({k: v for k, v in result.items() if k != 'events'}, ensure_ascii=False), flush=True)
        # Closing may finish a pending decode; wait for the actual busy flag.
        health_url = args.url.replace('ws://', 'http://').replace('wss://', 'https://').rsplit('/ws', 1)[0] + '/health'
        for _ in range(100):
            with urllib.request.urlopen(health_url, timeout=5) as f:
                health = json.load(f)
            if not health['busy']:
                break
            await asyncio.sleep(.1)
        else:
            raise RuntimeError('service remained busy after session')
    assert all(r['passed'] for r in results)


asyncio.run(main())
