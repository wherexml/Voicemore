"""Real WebSocket ASR acceptance; run with the bundled ASR Python runtime.

Failure cases: thinking pauses lock fragments too early; previews stall;
speech disappears or duplicates; long pauses never commit; Stop waits for
the silence timer; pure silence hallucinates text. Saves audio and raw events.
"""
import argparse
import asyncio
import hashlib
import json
from pathlib import Path
import time
import wave

import websockets


async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', required=True)
    parser.add_argument('--sample', required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--expect-silence', type=float, default=float('inf'),
                        help='legacy silence commit threshold; omit for stop/window finalization')
    args = parser.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    with wave.open(args.sample) as wav:
        assert (wav.getnchannels(), wav.getsampwidth(), wav.getframerate()) == (1, 2, 16000)
        original = wav.readframes(wav.getnframes())
    results = []
    for name, pause in [('continuous', 0), ('thinking_1200ms', 1.2),
                        ('thinking_2000ms', 2.0), ('long_3200ms', 3.2), ('silence', None)]:
        # Split the same public utterance at 1.6 s; retain all original samples.
        cut = 51200
        pcm = bytes(96000) if pause is None else original[:cut] + bytes(int(pause * 32000)) + original[cut:]
        with wave.open(str(out / (name + '.wav')), 'wb') as wav:
            wav.setparams((1, 2, 16000, 0, 'NONE', 'not compressed'))
            wav.writeframes(pcm)
        events = []
        started = time.monotonic()
        async with websockets.connect(args.url) as ws:
            ready = json.loads(await ws.recv())
            assert ready['type'] == 'ready', ready

            async def receive():
                async for raw in ws:
                    event = json.loads(raw)
                    event['received_s'] = round(time.monotonic() - started, 3)
                    events.append(event)
                    if event['type'] == 'error':
                        raise RuntimeError(event)
                    if event['type'] == 'done':
                        return

            receiver = asyncio.create_task(receive())
            for pos in range(0, len(pcm), 3200):
                await ws.send(pcm[pos:pos + 3200])
                await asyncio.sleep(len(pcm[pos:pos + 3200]) / 32000)
            stopped = time.monotonic()
            await ws.send(json.dumps({'type': 'stop'}))
            await asyncio.wait_for(receiver, 60)
        finals = [e for e in events if e['type'] == 'final']
        text = ''.join(e['text'] for e in finals)
        expected = 0 if pause is None else (2 if pause >= args.expect_silence else 1)
        passed = len(finals) == expected and (not text if pause is None else bool(text))
        if pause is not None:
            passed = passed and any(e['type'] == 'partial' for e in events)
        result = dict(name=name, passed=passed, text=text, final_segments=len(finals),
                      expected_segments=expected, stop_to_done_s=round(time.monotonic() - stopped, 3),
                      audio_sha256=hashlib.sha256(pcm).hexdigest(), events=events)
        results.append(result)
        print(json.dumps({k: v for k, v in result.items() if k != 'events'}, ensure_ascii=False), flush=True)
        (out / 'report.json').write_text(json.dumps(dict(url=args.url, results=results), ensure_ascii=False, indent=2))
    if not all(r['passed'] for r in results):
        raise SystemExit(1)


asyncio.run(main())
