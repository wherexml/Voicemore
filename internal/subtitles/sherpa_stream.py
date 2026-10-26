"""Live sherpa-onnx streaming recognition for Voicemore subtitles.

Reads raw 16 kHz mono PCM16 little-endian from stdin as it is captured and writes one JSON object
per line to stdout: ready, partial/final (segment, text) and error. Each text carries audio_s, the
audio fed to the stream so far (s since the process started reading), and token_end_s, the
recognizer's start_time of the segment plus its last token timestamp on that same clock, else -1.
A final also carries its range for background correction on the recognizer's decoded clock:
start_sample is its start_time before the endpoint reset, end_sample the new start_time after it
(every reset, silent ones included, moves the start), rounded to samples and clamped to the fed audio;
first_token_s/last_token_s relative to the start, prev_token_sample (the previous segment's last
token, -1 when none), tokens with token_samples (each token and its session sample, when the
recognizer gives as many tokens as timestamps; for speaker alignment), and clock ('decoded', or 'fed' when the recognizer gives no start_time).
Token timestamps are the recognizer's estimated token instants, not annotated speech ends. Endpointing follows
tools/system-audio-prototype/asr/replay_sherpa.py. stdin EOF flushes the last segment and exits.
"""
import argparse
import json
import sys
import time
from pathlib import Path

SR = 16000
CHUNK_BYTES = SR // 10 * 2


def out(**rec):
    sys.stdout.write(json.dumps(rec, ensure_ascii=False) + '\n')
    sys.stdout.flush()


def pick(model_dir, pattern, exclude=None):
    found = sorted(p for p in model_dir.glob(pattern) if not (exclude and exclude in p.name))
    if len(found) != 1:
        raise SystemExit(f'{model_dir}: expected exactly one {pattern}, found {[p.name for p in found]}')
    return found[0]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--model-dir', required=True)
    ap.add_argument('--threads', type=int, default=2)
    args = ap.parse_args()
    try:
        import numpy as np
        import sherpa_onnx
        model = Path(args.model_dir)
        t = time.monotonic()
        recognizer = sherpa_onnx.OnlineRecognizer.from_transducer(
            tokens=str(model / 'tokens.txt'), encoder=str(pick(model, 'encoder-*.int8.onnx')),
            decoder=str(pick(model, 'decoder-*.onnx', '.int8.')), joiner=str(pick(model, 'joiner-*.int8.onnx')),
            num_threads=args.threads, sample_rate=SR, feature_dim=80, decoding_method='greedy_search',
            enable_endpoint_detection=True, rule1_min_trailing_silence=2.4,
            rule2_min_trailing_silence=1.2, rule3_min_utterance_length=20.0, provider='cpu')
        stream = recognizer.create_stream()
    except BaseException as e:  # reported, then a non-zero exit
        out(type='error', message=repr(e))
        raise SystemExit(1)
    out(type='ready', load_s=round(time.monotonic() - t, 3))

    segment, last = 0, ''
    fed = 0  # samples accepted by the stream
    seg_start = 0  # sample where the current segment's decoding starts
    prev_token = -1  # sample of the last token of the last segment that had one

    def text_of():
        r = recognizer.get_result(stream)
        return (r if isinstance(r, str) else getattr(r, 'text', str(r))).strip()

    def decoded():
        # The segment's start_time (frames since the stream started × frame shift: the decoded clock,
        # behind fed by the model's lookahead) and its token times relative to it; None without them.
        try:
            r = recognizer.get_result_all(stream)
            return float(r.start_time), [float(x) for x in r.timestamps], [str(x) for x in getattr(r, 'tokens', [])]
        except Exception:
            return None

    def sample(s):
        return min(fed, max(0, int(round(s * SR))))

    def emit(kind, s, d=None, bounds=None):
        if kind == 'partial':
            d = decoded()
        ts = d[1] if d else []
        rec = dict(type=kind, segment=segment, text=s, audio_s=round(fed / SR, 3),
                   token_end_s=round(d[0] + ts[-1], 3) if ts else -1)
        if bounds:
            rec.update(bounds)
        out(**rec)

    def final_bounds(d, end, clock):
        # The range of a final, and its tokens relative to its start, for background correction.
        nonlocal prev_token
        ts = d[1] if d else []
        start = sample(d[0]) if d else seg_start
        b = dict(start_sample=start, end_sample=end, clock=clock, prev_token_sample=prev_token,
                 first_token_s=round(ts[0], 3) if ts else -1, last_token_s=round(ts[-1], 3) if ts else -1)
        if ts:
            prev_token = sample(d[0] + ts[-1])
            toks = d[2] if len(d) > 2 else []
            if len(toks) == len(ts):
                # Every token and its session sample, for speaker alignment of the final.
                b.update(tokens=toks, token_samples=[sample(d[0] + x) for x in ts])
        return b

    def drain():
        nonlocal segment, last, seg_start
        while recognizer.is_ready(stream):
            recognizer.decode_stream(stream)
            s = text_of()
            if s != last:
                emit('partial', s)
                last = s
        if recognizer.is_endpoint(stream):
            before = decoded()  # the segment's metadata, before reset clears it
            recognizer.reset(stream)
            after = decoded()  # Reset advances the processed frames: the next start_time
            if before and after:
                end, clock = sample(after[0]), 'decoded'
            else:
                end, clock = fed, 'fed'
            if last:
                emit('final', last, before, final_bounds(before if clock == 'decoded' else None, end, clock))
                segment += 1
            last = ''
            seg_start = end

    src = sys.stdin.buffer
    pending = b''
    while True:
        b = src.read1(CHUNK_BYTES) if hasattr(src, 'read1') else src.read(CHUNK_BYTES)
        if not b:
            break
        pending += b
        n = len(pending) // 2 * 2
        if n == 0:
            continue
        stream.accept_waveform(SR, np.frombuffer(pending[:n], dtype='<i2').astype(np.float32) / 32768)
        fed += n // 2
        pending = pending[n:]
        drain()
    stream.accept_waveform(SR, np.zeros(int(0.66 * SR), dtype=np.float32))
    stream.input_finished()
    while recognizer.is_ready(stream):
        recognizer.decode_stream(stream)
    s = text_of()
    if s:
        d = decoded()
        emit('final', s, d, final_bounds(d, fed, 'decoded' if d else 'fed'))


if __name__ == '__main__':
    main()
