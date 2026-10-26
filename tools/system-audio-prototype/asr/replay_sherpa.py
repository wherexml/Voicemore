"""Engine B: replay a captured WAV in real time through a sherpa-onnx streaming transducer.

Streaming here is measured, not assumed: audio is fed in 100 ms chunks on a wall-clock schedule,
the stream is decoded only as far as the recognizer reports is_ready(), and every text change is
logged with the submitted audio position. If decoding cannot keep up, the schedule lag (backlog)
grows and is recorded on each "sent" event. CPU only; one engine at a time.
"""
import argparse
import sys
import time
from pathlib import Path

import numpy as np
import sherpa_onnx

sys.path.insert(0, str(Path(__file__).resolve().parent))
from common import CHUNK_S, SR, RunLog, chunks, file_identity, host_info, read_wav, sha256_file  # noqa: E402

TAIL_PADDING_S = 0.66  # silence appended after the last chunk, as in the upstream streaming examples


def pick(model_dir, pattern, exclude=None):
    found = sorted(p for p in model_dir.glob(pattern) if not (exclude and exclude in p.name))
    if len(found) != 1:
        sys.exit(f'{model_dir}: expected exactly one {pattern}, found {[p.name for p in found]}')
    return found[0]


def result_text(recognizer, stream):
    r = recognizer.get_result(stream)
    return (r if isinstance(r, str) else getattr(r, 'text', str(r))).strip()


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--wav', required=True)
    ap.add_argument('--out', required=True, help='run JSONL path')
    ap.add_argument('--model-dir', required=True)
    ap.add_argument('--encoder', default='encoder-*.int8.onnx')
    ap.add_argument('--decoder', default='decoder-*.onnx', help='glob; .int8. files are excluded unless --decoder-int8')
    ap.add_argument('--decoder-int8', action='store_true')
    ap.add_argument('--joiner', default='joiner-*.int8.onnx')
    ap.add_argument('--threads', type=int, default=2)
    ap.add_argument('--decoding', default='greedy_search', choices=['greedy_search', 'modified_beam_search'])
    ap.add_argument('--rule1', type=float, default=2.4, help='endpoint: trailing silence without speech (s)')
    ap.add_argument('--rule2', type=float, default=1.2, help='endpoint: trailing silence after speech (s)')
    ap.add_argument('--rule3', type=float, default=20.0, help='endpoint: max utterance length (s)')
    args = ap.parse_args()

    model_dir = Path(args.model_dir).resolve()
    enc, dec, joi = pick(model_dir, args.encoder), pick(model_dir, args.decoder, None if args.decoder_int8 else '.int8.'), pick(model_dir, args.joiner)
    tokens = model_dir / 'tokens.txt'
    pcm = read_wav(args.wav)
    audio_total = len(pcm) / 2 / SR

    log = RunLog(Path(args.out).resolve())
    log.emit('run', engine='sherpa-onnx-streaming-zipformer', argv=sys.argv, wav=str(Path(args.wav).resolve()),
             wav_sha256=sha256_file(args.wav), audio_s=audio_total, chunk_s=CHUNK_S, paced='realtime',
             model_dir=str(model_dir), model_files=file_identity([enc, dec, joi, tokens]),
             sherpa_onnx=getattr(sherpa_onnx, '__version__', 'unknown'), numpy=np.__version__,
             threads=args.threads, decoding=args.decoding, endpoint_rules=[args.rule1, args.rule2, args.rule3],
             tail_padding_s=TAIL_PADDING_S, host=host_info())

    t_load = time.monotonic()
    recognizer = sherpa_onnx.OnlineRecognizer.from_transducer(
        tokens=str(tokens), encoder=str(enc), decoder=str(dec), joiner=str(joi),
        num_threads=args.threads, sample_rate=SR, feature_dim=80, decoding_method=args.decoding,
        enable_endpoint_detection=True, rule1_min_trailing_silence=args.rule1,
        rule2_min_trailing_silence=args.rule2, rule3_min_utterance_length=args.rule3, provider='cpu')
    stream = recognizer.create_stream()
    log.emit('engine_ready', load_s=round(time.monotonic() - t_load, 3))

    segment, last, decode_total, sent = 0, '', 0.0, 0

    def drain(final_flush=False):
        nonlocal segment, last, decode_total
        while recognizer.is_ready(stream):
            t = time.perf_counter()
            recognizer.decode_stream(stream)
            dt = time.perf_counter() - t
            decode_total += dt
            text = result_text(recognizer, stream)
            if text != last:
                log.emit('partial', segment=segment, text=text, decode_s=round(dt, 5),
                         audio_sent_s=round(sent / 2 / SR, 3))
                last = text
        if not final_flush and recognizer.is_endpoint(stream):
            if last:
                log.emit('final', segment=segment, text=last, endpoint=True, audio_sent_s=round(sent / 2 / SR, 3))
                segment += 1
            recognizer.reset(stream)
            last = ''

    log.start_clock()
    t0 = time.monotonic()
    try:
        for start, chunk in chunks(pcm):
            delay = t0 + start / 2 / SR - time.monotonic()
            if delay > 0:
                time.sleep(delay)
            samples = np.frombuffer(chunk, dtype='<i2').astype(np.float32) / 32768
            stream.accept_waveform(SR, samples)
            sent = start + len(chunk)
            log.emit('sent', audio_s=round(sent / 2 / SR, 3),
                     schedule_lag_s=round(time.monotonic() - t0 - start / 2 / SR, 4))
            drain()
        tail = t0 + audio_total - time.monotonic()
        if tail > 0:
            time.sleep(tail)
        stream.accept_waveform(SR, np.zeros(int(TAIL_PADDING_S * SR), dtype=np.float32))
        stream.input_finished()
        log.emit('stop', audio_sent_s=round(sent / 2 / SR, 3), tail_padding_s=TAIL_PADDING_S)
        drain(final_flush=True)
        if last:
            log.emit('final', segment=segment, text=last, endpoint=False, audio_sent_s=round(sent / 2 / SR, 3))
        log.emit('done', decode_total_s=round(decode_total, 4), audio_sent_s=round(sent / 2 / SR, 3))
    except Exception as e:
        log.emit('error', stage='runner', message=repr(e))
        raise
    finally:
        log.close()


if __name__ == '__main__':
    main()
