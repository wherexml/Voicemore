"""Compare run logs from replay_qwen.py / replay_sherpa.py against a human-verified reference.

Reference: UTF-8 plain text (untimed) or SRT (timed, on the captured WAV's own timeline).
Writes report.json and report.md. Definitions are printed in the Markdown so numbers are not
read as more than they are.
"""
import argparse
import hashlib
import json
import re
import sys
import unicodedata
from pathlib import Path

NORMALIZATION = ('NFKC; lowercase; every non-ASCII letter/digit (CJK, kana, Hangul, full-width forms after NFKC) '
                 'is one token; each maximal run of ASCII [a-z0-9\'] is one token; punctuation, symbols and '
                 'whitespace are dropped. No numeral conversion (二十 ≠ 20), no traditional/simplified mapping, '
                 'no filler removal.')


# ---------- reference ----------

def parse_srt(text):
    cues = []
    ts = r'(\d+):(\d{2}):(\d{2})[,.](\d{3})'
    for block in re.split(r'\n\s*\n', text.replace('\r\n', '\n').strip()):
        lines = block.strip().split('\n')
        for i, line in enumerate(lines):
            m = re.match(ts + r'\s*-->\s*' + ts, line.strip())
            if m:
                g = [int(x) for x in m.groups()]
                start = g[0] * 3600 + g[1] * 60 + g[2] + g[3] / 1000
                end = g[4] * 3600 + g[5] * 60 + g[6] + g[7] / 1000
                cues.append({'start': start, 'end': end, 'text': ' '.join(lines[i + 1:]).strip()})
                break
    return cues


def load_reference(path, offset):
    raw = Path(path).read_text(encoding='utf-8-sig')
    if Path(path).suffix.lower() == '.srt':
        cues = parse_srt(raw)
        if not cues:
            sys.exit(f'{path}: no SRT cues found')
        for c in cues:
            c['start'] += offset
            c['end'] += offset
        return cues, True
    return [{'start': None, 'end': None, 'text': raw.strip()}], False


# ---------- normalization and alignment ----------

def tokens(text):
    out, word = [], ''
    for ch in unicodedata.normalize('NFKC', text).lower():
        if ch.isascii() and (ch.isalnum() or ch == "'"):
            word += ch
            continue
        if word:
            out.append(word)
            word = ''
        if not ch.isascii() and ch.isalnum():
            out.append(ch)
    if word:
        out.append(word)
    return out


def align(ref, hyp):
    """Levenshtein with backtrace. Returns ops [(op, ref_i, hyp_j)] with op in = S D I."""
    n, m = len(ref), len(hyp)
    d = [[0] * (m + 1) for _ in range(n + 1)]
    for i in range(n + 1):
        d[i][0] = i
    for j in range(m + 1):
        d[0][j] = j
    for i in range(1, n + 1):
        ri, row, prev = ref[i - 1], d[i], d[i - 1]
        for j in range(1, m + 1):
            row[j] = min(prev[j - 1] + (ri != hyp[j - 1]), prev[j] + 1, row[j - 1] + 1)
    ops, i, j = [], n, m
    while i or j:
        if i and j and d[i][j] == d[i - 1][j - 1] + (ref[i - 1] != hyp[j - 1]):
            ops.append(('=' if ref[i - 1] == hyp[j - 1] else 'S', i - 1, j - 1))
            i, j = i - 1, j - 1
        elif i and d[i][j] == d[i - 1][j] + 1:
            ops.append(('D', i - 1, None))
            i -= 1
        else:
            ops.append(('I', None, j - 1))
            j -= 1
    return ops[::-1]


# ---------- run logs ----------

def load_run(path):
    events = [json.loads(line) for line in Path(path).read_text(encoding='utf-8').splitlines() if line.strip()]
    if not events or events[0].get('event') != 'run':
        sys.exit(f'{path}: not a run log')
    return events


def submit_time(sent, pos):
    """Wall time (t_s) at which audio up to `pos` seconds had been submitted; None if never."""
    for e in sent:
        if e['audio_s'] >= pos - 1e-6:
            return e['t_s']
    return None


def analyze(path, cues, timed, ref_tokens, ref_cue):
    ev = load_run(path)
    head = ev[0]
    sent = [e for e in ev if e['event'] == 'sent']
    results = [e for e in ev if e['event'] in ('partial', 'final')]
    finals = {}
    for e in results:
        if e['event'] == 'final':
            finals[e['segment']] = e  # last final per segment wins
    seg_order = sorted(finals)

    # Final hypothesis tokens, each tagged with the final event that delivered it.
    hyp, hyp_event = [], []
    for s in seg_order:
        t = tokens(finals[s]['text'])
        hyp += t
        hyp_event += [finals[s]] * len(t)
    ops = align(ref_tokens, hyp)
    S = sum(o[0] == 'S' for o in ops)
    D = sum(o[0] == 'D' for o in ops)
    I = sum(o[0] == 'I' for o in ops)
    N = len(ref_tokens)

    # Cumulative token count visible at each result event (earlier finals + this event's text).
    base = {}
    acc = 0
    for s in seg_order:
        base[s] = acc
        acc += len(tokens(finals[s]['text']))
    visible = []
    for e in results:
        b = base.get(e['segment'])
        if b is None:  # partial of a segment that never finalized
            b = sum(len(tokens(finals[s]['text'])) for s in seg_order if s < e['segment'])
        visible.append((e, b + len(tokens(e['text']))))

    def first_event_covering(j, kinds):
        for e, count in visible:
            if e['event'] in kinds and count > j:
                return e
        return None

    stop = next((e for e in ev if e['event'] == 'stop'), None)
    done = next((e for e in ev if e['event'] == 'done'), None)
    errors = [e for e in ev if e['event'] == 'error']
    first_out = next((e for e in results if e['text'].strip()), None)
    audio_s = head['audio_s']

    if head['engine'].startswith('qwen'):
        inference_s = sum(e.get('decode_s') or 0 for e in results)
        inference_note = 'sum of worker decode_s over all partial and final decodes (includes repeated full-utterance redecoding)'
    else:
        inference_s = (done or {}).get('decode_total_s')
        inference_note = 'sum of decode_stream() wall time in the runner process'

    cue_rows = []
    if timed:
        for ci, c in enumerate(cues):
            hj = [o[2] for o in ops if o[1] is not None and ref_cue[o[1]] == ci and o[0] in ('=', 'S')]
            row = {'cue': ci + 1, 'start': c['start'], 'end': c['end'], 'ref_tokens': sum(1 for x in ref_cue if x == ci),
                   'aligned_hyp_tokens': len(hj)}
            if hj:
                fe = first_event_covering(min(hj), ('partial', 'final'))
                fin = hyp_event[max(hj)]
                ts, te = submit_time(sent, c['start']), submit_time(sent, c['end'])
                row['first_result_lag_from_cue_start_s'] = None if fe is None or ts is None else round(fe['t_s'] - ts, 3)
                row['final_lag_from_cue_end_s'] = None if te is None else round(fin['t_s'] - te, 3)
            else:
                row['first_result_lag_from_cue_start_s'] = row['final_lag_from_cue_end_s'] = None
                row['note'] = 'no aligned output'
            cue_rows.append(row)

    segs = []
    for s in sorted({e['segment'] for e in results}):
        rs = [e for e in results if e['segment'] == s]
        fp = next((e for e in rs if e['text'].strip()), None)
        fn = finals.get(s)
        segs.append({'segment': s,
                     'first_text_t_s': fp and fp['t_s'], 'first_text_audio_sent_s': fp and fp['audio_sent_s'],
                     'final_t_s': fn and fn['t_s'], 'final_audio_sent_s': fn and fn['audio_sent_s'],
                     'partials': sum(e['event'] == 'partial' for e in rs)})

    lags = [e['schedule_lag_s'] for e in sent if 'schedule_lag_s' in e]
    return {
        'run_log': str(path), 'run_log_sha256': hashlib.sha256(Path(path).read_bytes()).hexdigest(),
        'engine': head['engine'], 'wav_sha256': head['wav_sha256'], 'audio_s': audio_s,
        'identity': {k: head.get(k) for k in ('argv', 'model_files', 'worker_source', 'installed_revision', 'sherpa_onnx',
                                              'numpy', 'websockets', 'host', 'threads', 'decoding', 'endpoint_rules',
                                              'language', 'chunk_s', 'paced') if head.get(k) is not None},
        'hypothesis': ''.join(finals[s]['text'] for s in seg_order),
        'cer': {'ref_tokens': N, 'hyp_tokens': len(hyp), 'S': S, 'D': D, 'I': I,
                'hits': sum(o[0] == '=' for o in ops), 'cer': round((S + D + I) / N, 4) if N else None},
        'session': {
            'first_output_t_s': first_out and first_out['t_s'],
            'audio_sent_at_first_output_s': first_out and first_out['audio_sent_s'],
            'stop_t_s': stop and stop['t_s'], 'done_t_s': done and done['t_s'],
            'stop_to_done_flush_s': round(done['t_s'] - stop['t_s'], 3) if stop and done else None,
            'finals': len(finals), 'partials': sum(e['event'] == 'partial' for e in results),
            'errors': [e.get('message') for e in errors],
        },
        'throughput': {
            'inference_s': None if inference_s is None else round(inference_s, 3), 'inference_note': inference_note,
            'inference_rtf': None if inference_s is None else round(inference_s / audio_s, 4),
            'wall_to_done_over_audio': round(done['t_s'] / audio_s, 4) if done else None,
            'done_after_audio_end_s': round(done['t_s'] - audio_s, 3) if done else None,
            'max_send_schedule_lag_s': max(lags) if lags else None,
        },
        'cues': cue_rows, 'segments': segs,
    }


# ---------- output ----------

def fmt(v, unit=''):
    return 'unavailable' if v is None else (f'{v:.3f}{unit}' if isinstance(v, float) else f'{v}{unit}')


def markdown(rep):
    L = ['# System-audio replay comparison', '',
         f"Reference: `{rep['reference']['path']}` (sha256 `{rep['reference']['sha256'][:16]}…`, "
         f"{'timed SRT' if rep['reference']['timed'] else 'untimed text'}, {rep['reference']['tokens']} tokens). "
         f"Human verification of the reference is the operator's responsibility.", '']
    if len({r['wav_sha256'] for r in rep['runs']}) != 1:
        L += ['**WARNING: runs used different WAV files; results are not paired.**', '']
    else:
        L += [f"WAV sha256 `{rep['runs'][0]['wav_sha256']}` ({rep['runs'][0]['audio_s']:.2f} s), same file for all runs.", '']
    L += ['| engine | CER | S | D | I | ref tok | hyp tok | first output t | audio sent then | stop→done | inference RTF | done after audio end | max send lag |',
          '|---|---|---|---|---|---|---|---|---|---|---|---|---|']
    for r in rep['runs']:
        c, s, t = r['cer'], r['session'], r['throughput']
        L.append(f"| {r['engine']} | {fmt(c['cer'])} | {c['S']} | {c['D']} | {c['I']} | {c['ref_tokens']} | {c['hyp_tokens']} | "
                 f"{fmt(s['first_output_t_s'], ' s')} | {fmt(s['audio_sent_at_first_output_s'], ' s')} | {fmt(s['stop_to_done_flush_s'], ' s')} | "
                 f"{fmt(t['inference_rtf'])} | {fmt(t['done_after_audio_end_s'], ' s')} | {fmt(t['max_send_schedule_lag_s'], ' s')} |")
    L.append('')
    for r in rep['runs']:
        L += [f"## {r['engine']}", '', f"Errors: {r['session']['errors'] or 'none'}. "
              f"Finals: {r['session']['finals']}, partials: {r['session']['partials']}. Inference time: {r['throughput']['inference_note']}.", '',
              'Final hypothesis:', '', '> ' + (r['hypothesis'] or '(empty)'), '']
        if rep['reference']['timed']:
            L += ['| cue | start | end | ref tok | aligned | first-result lag from cue start | final lag from cue end |', '|---|---|---|---|---|---|---|']
            for c in r['cues']:
                L.append(f"| {c['cue']} | {c['start']:.2f} | {c['end']:.2f} | {c['ref_tokens']} | {c['aligned_hyp_tokens']} | "
                         f"{fmt(c['first_result_lag_from_cue_start_s'], ' s')} | {fmt(c['final_lag_from_cue_end_s'], ' s')} |")
        else:
            L.append('Sentence-level first-result and finalization latency: **unavailable** (reference has no timed segments).')
        L += ['', '| segment | first text t | audio sent | final t | audio sent | partials |', '|---|---|---|---|---|---|']
        for s in r['segments']:
            L.append(f"| {s['segment']} | {fmt(s['first_text_t_s'])} | {fmt(s['first_text_audio_sent_s'])} | {fmt(s['final_t_s'])} | "
                     f"{fmt(s['final_audio_sent_s'])} | {s['partials']} |")
        L += ['', '<details><summary>identity</summary>', '', '```json', json.dumps(r['identity'], ensure_ascii=False, indent=1), '```', '</details>', '']
    L += ['## Definitions and limits', '',
          f"- Normalization: {NORMALIZATION}",
          '- CER = (S + D + I) / reference tokens over the concatenated final texts, for this reference and this normalization only. '
          'It is not a general accuracy figure; 1 − CER is not reported as accuracy. Partials are not scored.',
          '- Clock: t = monotonic seconds since the first audio chunk was submitted. Audio is submitted on a real-time schedule '
          f"in {rep['runs'][0]['identity'].get('chunk_s', 0.1)} s chunks, so audio position p is available to the engine at about t ≈ p "
          '(exact submit times come from the logged `sent` events). "audio sent" columns are submission positions, not word timestamps.',
          '- First-result lag from cue start = t of the first partial/final whose visible text reaches the first hypothesis token aligned '
          'to the cue, minus the submit time of the cue start. It includes the time spent speaking that first word and up to one chunk; '
          'it assumes partial prefixes are stable (a later revision may change credited text).',
          '- Final lag from cue end = t of the final that delivered the last aligned token of the cue, minus the submit time of the cue end. '
          'This is audio-relative end-to-end lag (buffering + segmentation policy + queueing + inference), not inference time.',
          '- Inference RTF = engine inference time / audio duration (definition per engine above); it is separate from end-to-end lag. '
          '"done after audio end" is remaining backlog plus flush after the last audio; "max send lag" is how far the sender fell behind '
          'its real-time schedule (sherpa decodes in the sender loop, so its decode backlog shows here; Qwen decodes asynchronously).',
          '- Cue timing must be on the captured WAV timeline (use --ref-offset to shift). Untimed references give no sentence latency.',
          '- One capture, one reference, sequential runs on one machine; no statistical significance is implied.', '']
    return '\n'.join(L)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--reference', required=True, help='human-verified UTF-8 .txt or .srt')
    ap.add_argument('--ref-offset', type=float, default=0.0, help='seconds added to SRT times to match the WAV timeline')
    ap.add_argument('--out-dir', required=True)
    ap.add_argument('runs', nargs='+', help='run JSONL files')
    args = ap.parse_args()

    cues, timed = load_reference(args.reference, args.ref_offset)
    ref_tokens, ref_cue = [], []
    for i, c in enumerate(cues):
        t = tokens(c['text'])
        ref_tokens += t
        ref_cue += [i] * len(t)
    if not ref_tokens:
        sys.exit('reference has no tokens after normalization')
    rep = {'reference': {'path': str(Path(args.reference).resolve()), 'sha256': hashlib.sha256(Path(args.reference).read_bytes()).hexdigest(),
                         'timed': timed, 'cues': len(cues), 'tokens': len(ref_tokens), 'offset_s': args.ref_offset},
           'normalization': NORMALIZATION, 'argv': sys.argv,
           'runs': [analyze(p, cues, timed, ref_tokens, ref_cue) for p in args.runs]}
    out = Path(args.out_dir)
    out.mkdir(parents=True, exist_ok=True)
    (out / 'report.json').write_text(json.dumps(rep, ensure_ascii=False, indent=2), encoding='utf-8')
    (out / 'report.md').write_text(markdown(rep), encoding='utf-8')
    print(out / 'report.md')


if __name__ == '__main__':
    main()
