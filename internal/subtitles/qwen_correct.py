"""Background Qwen3-ASR correction for Voicemore live subtitles.

Run with the installed local-asr runtime (read-only) and its model directory. Loads the model once,
then serves one request at a time on stdin: a JSON header line {"id", "bytes", "language"} followed by
exactly that many bytes of 16 kHz mono PCM16 little-endian, the audio of one finished sherpa segment.
Writes one JSON object per line to stdout: ready, result (id, text, decode_s, audio_s) and error
(with id for a failed request, without id for a failed load, which then exits non-zero). It
recognizes the audio again with the same options as the dictation worker's transcribe, without
hotwords; it never sees sherpa's text. stdin EOF exits. No port, no files written.
"""
import argparse
import json
import re
import sys
import time

SR = 16000
LANGUAGES = {'zh': 'Chinese', 'auto': None, 'en': 'English'}
LANGUAGE_TAG = re.compile(r'^\s*language\s+[^<\n]{0,40}<asr_text>\s*')


def out(**rec):
    sys.stdout.write(json.dumps(rec, ensure_ascii=False) + '\n')
    sys.stdout.flush()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--model-dir', required=True)
    args = ap.parse_args()
    try:
        import numpy as np
        import mlx.core as mx
        from mlx_audio.stt.utils import load_model
        t = time.monotonic()
        model = load_model(args.model_dir)
        model.generate(np.zeros(SR, dtype=np.float32), language='Chinese', max_tokens=8, verbose=False)
        mx.eval(model.parameters())
    except BaseException as e:  # reported, then a non-zero exit
        out(type='error', message=repr(e))
        raise SystemExit(1)
    out(type='ready', load_s=round(time.monotonic() - t, 3))

    src = sys.stdin.buffer
    while True:
        line = src.readline()
        if not line:
            return
        try:
            req = json.loads(line)
            n = int(req['bytes'])
            rid = req['id']
        except (ValueError, KeyError, TypeError) as e:
            out(type='error', message='bad header: ' + repr(e))
            raise SystemExit(1)
        data = src.read(n)
        if len(data) != n:
            return
        try:
            samples = np.frombuffer(data[:n // 2 * 2], dtype='<i2').astype(np.float32) / 32768
            t = time.monotonic()
            r = model.generate(samples, language=LANGUAGES.get(req.get('language'), None), hotwords=None,
                               system_prompt=None, max_tokens=512, temperature=0, verbose=False)
            text = LANGUAGE_TAG.sub('', r.text.strip(), count=1).strip()
            out(type='result', id=rid, text=text, decode_s=round(time.monotonic() - t, 3),
                audio_s=round(len(samples) / SR, 3))
        except Exception as e:
            out(type='error', id=rid, message=repr(e))


if __name__ == '__main__':
    main()
