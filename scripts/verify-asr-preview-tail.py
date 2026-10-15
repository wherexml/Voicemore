"""Real-model WebSocket E2E for previews after speech ends.

Failure cases: the last partial omits the audio tail; silence repeatedly
redecodes unchanged audio; a pause commits the segment; Stop changes an
already settled preview; pure silence produces text. No mock recognizer.
Run with the bundled ASR Python; saves input WAV and all protocol events.
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
    args = parser.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    with wave.open(args.sample) as wav:
        assert (wav.getnchannels(), wav.getsampwidth(), wav.getframerate()) == (1, 2, 16000)
        original = wav.readframes(wav.getnframes())
    results = []
    for name, pcm in [('speech', original), ('silence', bytes(32000))]:
        pcm += bytes(3 * 32000)
        with wave.open(str(out / (name + '.wav')), 'wb') as wav:
            wav.setparams((1, 2, 16000, 0, 'NONE', 'not compressed'))
            wav.writeframes(pcm)
        events = []
        started = time.monotonic()
        async with websockets.connect(args.url) as ws:
            assert json.loads(await ws.recv())['type'] == 'ready'

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
                chunk = pcm[pos:pos + 3200]
                await ws.send(chunk)
                await asyncio.sleep(len(chunk) / 32000)
            # Allow inference to settle while recording remains open.
            await asyncio.sleep(2)
            before_stop = list(events)
            stopped = time.monotonic() - started
            await ws.send(json.dumps({'type': 'stop'}))
            await asyncio.wait_for(receiver, 60)
        partials = [e for e in before_stop if e['type'] == 'partial']
        finals = [e for e in events if e['type'] == 'final']
        checks = {'no_early_final': not any(e['type'] == 'final' for e in before_stop)}
        if name == 'speech':
            checks.update(
                one_final=len(finals) == 1,
                preview_contains_final_audio=bool(partials and finals) and abs(partials[-1]['audio_s'] - finals[-1]['audio_s']) < 0.001,
                preview_equals_final=bool(partials and finals) and bool(finals[-1]['text']) and partials[-1]['text'] == finals[-1]['text'],
                no_silence_loop=not any(e['received_s'] > stopped - 1 for e in partials),
            )
        else:
            checks['no_hallucination'] = not partials and not finals
        result = dict(name=name, passed=all(checks.values()), checks=checks,
                      stopped_s=stopped, audio_sha256=hashlib.sha256(pcm).hexdigest(), events=events)
        results.append(result)
        print(json.dumps({k: v for k, v in result.items() if k != 'events'}, ensure_ascii=False), flush=True)
        (out / 'report.json').write_text(json.dumps(dict(url=args.url, results=results), ensure_ascii=False, indent=2))
    if not all(r['passed'] for r in results):
        raise SystemExit(1)


asyncio.run(main())
