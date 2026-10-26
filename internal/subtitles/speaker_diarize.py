"""Windowed speaker diarization for Voicemore live subtitles.

Run with the sherpa venv's interpreter: --segmentation (pyannote segmentation-3.0 model.onnx) and
--embedding (3D-Speaker ERes2Net base, zh-cn). Loads once, then serves one request at a time on
stdin: a JSON header line {"id", "bytes"} followed by exactly that many bytes of 16 kHz mono PCM16
little-endian, one rolling window of system audio. Writes one JSON object per line to stdout:
ready, result (id, turns, clusters, infer_s) and error (with id for a failed request, without for a
failed load, which then exits non-zero).

turns are the window's speech turns, start/end in seconds from the window start, speaker the
window-local cluster. clusters carry, per local cluster, its speech seconds without overlap and a
unit-length embedding of up to MAX_EMBED_S of that speech, so the app can match clusters across
windows within one session. Embeddings exist only in this process and the app's memory: nothing is
written to disk here. stdin EOF exits. No port, no files written. CPU only, one thread by default.
"""
import argparse
import json
import sys
import time

SR = 16000
MAX_EMBED_S = 8.0
MIN_EMBED_S = 0.5


def out(**rec):
    sys.stdout.write(json.dumps(rec, ensure_ascii=False) + '\n')
    sys.stdout.flush()


def overlap_free(turns):
    """Per turn, its parts that no turn of another cluster overlaps."""
    parts = []
    for i, t in enumerate(turns):
        segs = [(t.start, t.end)]
        for j, u in enumerate(turns):
            if j == i or u.speaker == t.speaker:
                continue
            nxt = []
            for a, b in segs:
                if u.end <= a or u.start >= b:
                    nxt.append((a, b))
                    continue
                if u.start > a:
                    nxt.append((a, u.start))
                if u.end < b:
                    nxt.append((u.end, b))
            segs = nxt
        parts += [(t.speaker, a, b) for a, b in segs if b - a > 0.05]
    return parts


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--segmentation', required=True)
    ap.add_argument('--embedding', required=True)
    ap.add_argument('--threads', type=int, default=1)
    ap.add_argument('--threshold', type=float, default=0.5)
    args = ap.parse_args()
    try:
        import numpy as np
        import sherpa_onnx as s
        t = time.monotonic()
        config = s.OfflineSpeakerDiarizationConfig(
            segmentation=s.OfflineSpeakerSegmentationModelConfig(
                pyannote=s.OfflineSpeakerSegmentationPyannoteModelConfig(model=args.segmentation),
                num_threads=args.threads, provider='cpu'),
            embedding=s.SpeakerEmbeddingExtractorConfig(model=args.embedding, num_threads=args.threads, provider='cpu'),
            clustering=s.FastClusteringConfig(num_clusters=-1, threshold=args.threshold),
            min_duration_on=0.3, min_duration_off=0.5)
        if not config.validate():
            raise ValueError('invalid diarization config')
        diarizer = s.OfflineSpeakerDiarization(config)
        extractor = s.SpeakerEmbeddingExtractor(
            s.SpeakerEmbeddingExtractorConfig(model=args.embedding, num_threads=args.threads, provider='cpu'))
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
            t = time.monotonic()
            audio = np.frombuffer(data[:n // 2 * 2], dtype='<i2').astype(np.float32) / 32768
            turns = list(diarizer.process(audio).sort_by_start_time())
            speech = {}
            for spk, a, b in overlap_free(turns):
                speech.setdefault(spk, []).append((a, b))
            clusters = []
            for spk, segs in sorted(speech.items()):
                total = sum(b - a for a, b in segs)
                pcs, used = [], 0.0
                # The longest parts first: they hold the cleanest voice of the cluster.
                for a, b in sorted(segs, key=lambda x: x[0] - x[1]):
                    take = min(b - a, MAX_EMBED_S - used)
                    if take <= 0:
                        break
                    pcs.append(audio[int(a * SR):int((a + take) * SR)])
                    used += take
                rec = dict(speaker=int(spk), speech_s=round(total, 3))
                if used >= MIN_EMBED_S:
                    st = extractor.create_stream()
                    st.accept_waveform(SR, np.concatenate(pcs))
                    st.input_finished()
                    v = np.asarray(extractor.compute(st), dtype=np.float32)
                    norm = float(np.linalg.norm(v))
                    if norm > 0:
                        rec['embedding'] = [round(float(x), 5) for x in v / norm]
                clusters.append(rec)
            out(type='result', id=rid, infer_s=round(time.monotonic() - t, 3), audio_s=round(len(audio) / SR, 3),
                turns=[dict(start=round(x.start, 3), end=round(x.end, 3), speaker=int(x.speaker)) for x in turns],
                clusters=clusters)
        except Exception as e:
            out(type='error', id=rid, message=repr(e))


if __name__ == '__main__':
    main()
