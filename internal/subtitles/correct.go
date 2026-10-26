package subtitles

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Background correction of sherpa's finals: the corrector keeps the last
// corrRingSeconds of the audio it feeds sherpa, cuts each final's range
// (sherpa's endpoint resets, in its own sample clock) and has the installed
// Qwen model recognize that audio again in an isolated helper process,
// one request at a time. It is optional in every way: it loads only after
// the first partial was emitted, its queue is bounded and drops old work,
// and a missing model, a failed load or a hung helper only ends correction,
// never the session. While the app's dictation is busy (Config.Gate) it
// starts no load or inference and aborts one in flight, keeping its job;
// it resumes the bounded queue once dictation is idle.
// See docs/public/LIVE-SUBTITLES.md.

//go:embed qwen_correct.py
var correctScript []byte

const (
	corrRingSeconds = 60
	corrMinRange    = sampleRate / 2   // samples: shorter audio is too little to recognize again
	corrMaxRange    = 24 * sampleRate  // samples: longer ranges are skipped, not cut
	corrQueueMax    = 3                // waiting jobs; the oldest is dropped beyond
	corrStaleAfter  = 25 * time.Second // from the final, not counting time deferred for dictation
	corrMaxAge      = 90 * time.Second // from the final, deferred time included
	corrInferT      = 12 * time.Second
	corrMaxRestarts = 1
	// The sherpa model is bilingual, so Qwen detects the language itself.
	corrLanguage = "auto"

	// Boundary risk: a token this close to a range's start or end may
	// belong to a word the endpoint cut. A risky start gets up to
	// corrPreroll of earlier audio, never reaching within corrPrevGuard of
	// the previous segment's last token.
	corrEdgeS     = 0.24
	corrPreroll   = sampleRate * 2 / 5 // samples (0.4 s)
	corrPrevGuard = sampleRate / 2     // samples (0.5 s)
)

// segRange is where a sherpa final's audio is, on the recognizer's decoded
// clock in session samples (sherpa_stream.py), with its first and last
// token relative to its start (-1 when none), and the previous segment's
// last token (-1 when none). clock is "fed" when sherpa gave no start_time.
type segRange struct {
	start, end          int64
	prevToken           int64
	firstTokS, lastTokS float64
	clock               string
}

// pcmRing holds the last corrRingSeconds of fed audio, addressed by sample
// offset from the session's first fed chunk. sherpa's decoded clock counts
// from the same first sample, behind fed by its lookahead, and every chunk
// is added here before sherpa gets it.
type pcmRing struct {
	mu  sync.Mutex
	buf []byte
	end int64 // bytes ever added
}

func (r *pcmRing) add(pcm []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := int64(len(r.buf))
	for len(pcm) > 0 {
		k := copy(r.buf[r.end%n:], pcm)
		pcm = pcm[k:]
		r.end += int64(k)
	}
}

// slice copies samples [from, to), or is false when the ring no longer (or
// does not yet) hold all of them.
func (r *pcmRing) slice(from, to int64) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, b, n := from*2, to*2, int64(len(r.buf))
	if a < 0 || b <= a || b > r.end || a < r.end-n {
		return nil, false
	}
	out := make([]byte, b-a)
	for k := int64(0); k < b-a; {
		off := (a + k) % n
		k += int64(copy(out[k:], r.buf[off:min(n, off+b-a-k)]))
	}
	return out, true
}

type corrJob struct {
	id         uint64
	final      Event
	start, end int64 // samples, pre-roll included
	preroll    int64
	lead, tail bool // boundary risks, see CorrInfo
	pcm        []byte
	ref        time.Time // the final's time, moved on by time deferred for dictation
}

type corrector struct {
	cfg     Config
	session uint64
	emit    func(Event)
	ring    *pcmRing
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	wake    chan struct{}

	mu       sync.Mutex
	queue    []*corrJob
	seq      uint64
	warmed   bool
	closed   bool
	disabled bool

	// The loop goroutine's own.
	h          *corrHelper
	restarts   int
	deferredAt time.Time // since when dictation holds correction back; zero when not
}

// What ensure found.
const (
	corrReady = iota
	corrDeferred
	corrUnavailable
)

var errDeferred = fail(ErrEngineStart, "deferred for dictation")

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func newCorrector(ctx context.Context, cfg Config, session uint64, emit func(Event), ring *pcmRing) *corrector {
	c := &corrector{cfg: cfg, session: session, emit: emit, ring: ring,
		done: make(chan struct{}), wake: make(chan struct{}, 1)}
	c.ctx, c.cancel = context.WithCancel(ctx)
	cfg.log("correction_init", map[string]any{"session": session, "ring_s": corrRingSeconds, "queue_max": corrQueueMax,
		"timeout_ms": corrInferT.Milliseconds(), "language": corrLanguage, "qwen_root": cfg.QwenRoot})
	go c.loop()
	return c
}

func (c *corrector) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *corrector) log(event string, fields map[string]any) {
	fields["session"] = c.session
	c.cfg.log(event, fields)
}

// warm lets the helper load: called for every partial, so the first
// subtitles are out before Qwen competes for the machine.
func (c *corrector) warm() {
	c.mu.Lock()
	first := !c.warmed
	c.warmed = true
	c.mu.Unlock()
	if first {
		c.signal()
	}
}

// submit queues the audio of a final for recognition; it is called from
// the engine's goroutine and never blocks on Qwen.
func (c *corrector) submit(final Event, rng *segRange) {
	if rng == nil {
		rng = &segRange{start: -1, end: -1, prevToken: -1, firstTokS: -1, lastTokS: -1}
	}
	start, end := rng.start, rng.end
	// Boundary risk is judged on the decoded clock only: a fed range
	// already runs past the recognizer's boundary.
	edge := int64(corrEdgeS * sampleRate)
	decodedClock := rng.clock == "decoded"
	lead := decodedClock && rng.firstTokS >= 0 && rng.firstTokS < corrEdgeS
	tail := decodedClock && rng.lastTokS >= 0 && end-(start+int64(rng.lastTokS*sampleRate)) < edge
	var preroll int64
	if lead {
		floor := int64(0)
		if rng.prevToken >= 0 {
			floor = rng.prevToken + corrPrevGuard
		}
		preroll = min(max(start-floor, 0), corrPreroll)
		start -= preroll
	}
	fields := map[string]any{"segment": final.Segment, "rev": final.Rev, "start_s": float64(start) / sampleRate,
		"end_s": float64(end) / sampleRate, "dur_s": float64(end-start) / sampleRate, "clock": rng.clock,
		"decoded_start_s": float64(rng.start) / sampleRate, "preroll_s": float64(preroll) / sampleRate,
		"first_token_s": rng.firstTokS, "last_token_s": rng.lastTokS, "prev_token_s": float64(rng.prevToken) / sampleRate,
		"lead_risk": lead, "tail_risk": tail}
	drop := func(why string) {
		fields["why"] = why
		c.log("correction_drop", fields)
	}
	switch {
	case start < 0 || end <= start:
		drop("no_range")
		return
	case end-start < corrMinRange:
		drop("too_short")
		return
	case end-start > corrMaxRange:
		drop("too_long")
		return
	}
	cut := time.Now()
	pcm, ok := c.ring.slice(start, end)
	if !ok {
		drop("evicted")
		return
	}
	c.mu.Lock()
	if c.closed || c.disabled {
		c.mu.Unlock()
		drop("unavailable")
		return
	}
	c.seq++
	job := &corrJob{id: c.seq, final: final, start: start, end: end, preroll: preroll, lead: lead, tail: tail, pcm: pcm, ref: final.At}
	c.queue = append(c.queue, job)
	var old *corrJob
	if len(c.queue) > corrQueueMax {
		old, c.queue = c.queue[0], c.queue[1:]
	}
	depth := len(c.queue)
	c.mu.Unlock()
	fields["job"], fields["depth"], fields["copy_ms"] = job.id, depth, time.Since(cut).Milliseconds()
	c.log("correction_range", fields)
	if old != nil {
		c.dropJob(old, "saturated")
	}
	c.signal()
}

func (c *corrector) dropJob(j *corrJob, why string) {
	c.log("correction_drop", map[string]any{"job": j.id, "segment": j.final.Segment, "rev": j.final.Rev, "why": why,
		"age_ms": time.Since(j.final.At).Milliseconds()})
}

func (c *corrector) pop() *corrJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) == 0 {
		return nil
	}
	j := c.queue[0]
	c.queue = c.queue[1:]
	return j
}

// disable ends correction for the session and drops what waits.
func (c *corrector) disable(why string) {
	c.mu.Lock()
	c.disabled = true
	q := c.queue
	c.queue = nil
	c.mu.Unlock()
	c.log("correction_disabled", map[string]any{"why": why})
	for _, j := range q {
		c.dropJob(j, "unavailable")
	}
}

func (c *corrector) loop() {
	defer close(c.done)
	defer func() {
		if c.h != nil {
			c.h.close()
		}
	}()
	for {
		busy, flip := c.cfg.Gate.State()
		if busy {
			c.deferFor("idle", 0)
		} else {
			c.resume()
			c.work(flip)
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		case <-flip:
		}
	}
}

// work runs what waits while dictation stays idle; flip closes when it
// becomes busy, and then work stops, keeping the job it was on.
func (c *corrector) work(flip <-chan struct{}) {
	c.mu.Lock()
	wanted := !c.disabled && (c.warmed || len(c.queue) > 0)
	c.mu.Unlock()
	if wanted && c.ensure(flip) == corrDeferred {
		c.deferFor("loading", 0)
		return
	}
	for c.ctx.Err() == nil && !isClosed(flip) {
		j := c.pop()
		if j == nil {
			return
		}
		if time.Since(j.ref) > corrStaleAfter || time.Since(j.final.At) > corrMaxAge {
			c.dropJob(j, "stale_age")
			continue
		}
		switch c.ensure(flip) {
		case corrDeferred:
			c.requeue(j)
			c.deferFor("loading", j.id)
			return
		case corrUnavailable:
			c.dropJob(j, "unavailable")
			continue
		}
		if !c.run(j, flip) {
			c.requeue(j)
			c.deferFor("inference", j.id)
			return
		}
	}
}

// requeue puts back a job dictation interrupted, first in line.
func (c *corrector) requeue(j *corrJob) {
	c.mu.Lock()
	if c.closed || c.disabled {
		c.mu.Unlock()
		return
	}
	c.queue = append([]*corrJob{j}, c.queue...)
	var old *corrJob
	if len(c.queue) > corrQueueMax {
		old, c.queue = c.queue[0], c.queue[1:]
	}
	c.mu.Unlock()
	if old != nil {
		c.dropJob(old, "saturated")
	}
}

// deferFor notes that dictation holds correction back: once when it
// starts, and for every load or inference it interrupts.
func (c *corrector) deferFor(during string, job uint64) {
	first := c.deferredAt.IsZero()
	if first {
		c.deferredAt = time.Now()
	}
	if first || during != "idle" {
		c.mu.Lock()
		depth := len(c.queue)
		c.mu.Unlock()
		c.log("correction_deferred", map[string]any{"why": "dictation", "during": during, "job": job, "depth": depth, "loaded": c.h != nil})
	}
}

// resume ends a deferral: the waiting jobs' staleness does not count it.
func (c *corrector) resume() {
	if c.deferredAt.IsZero() {
		return
	}
	d := time.Since(c.deferredAt)
	c.deferredAt = time.Time{}
	c.mu.Lock()
	for _, j := range c.queue {
		j.ref = j.ref.Add(min(d, time.Since(j.ref)))
	}
	depth := len(c.queue)
	c.mu.Unlock()
	c.log("correction_resumed", map[string]any{"deferred_ms": d.Milliseconds(), "depth": depth})
}

// ensure has a ready helper, loading one when there is none and
// correction is still possible. Dictation becoming busy aborts a load.
func (c *corrector) ensure(flip <-chan struct{}) int {
	if c.h != nil {
		return corrReady
	}
	c.mu.Lock()
	disabled := c.disabled
	c.mu.Unlock()
	if disabled || c.ctx.Err() != nil {
		return corrUnavailable
	}
	started := time.Now()
	c.log("correction_load_start", map[string]any{"restarts": c.restarts})
	h, err := startCorrHelper(c.ctx, c.cfg, flip)
	if err == errDeferred {
		return corrDeferred
	}
	if err != nil {
		if c.ctx.Err() == nil {
			code := ErrEngineStart
			if e, ok := err.(*Error); ok {
				code = e.Code
			}
			c.log("correction_unavailable", map[string]any{"code": code, "detail": err.Error(), "load_ms": time.Since(started).Milliseconds()})
			c.disable(code)
		}
		return corrUnavailable
	}
	c.h = h
	c.log("correction_ready", map[string]any{"pid": h.pid, "load_ms": time.Since(started).Milliseconds()})
	return corrReady
}

// run recognizes one job; false when dictation interrupted it, which ends
// the helper (MLX cannot stop a generation) without counting a failure.
func (c *corrector) run(j *corrJob, flip <-chan struct{}) bool {
	started := time.Now()
	queueMs := started.Sub(j.final.At).Milliseconds()
	c.log("correction_start", map[string]any{"job": j.id, "segment": j.final.Segment, "rev": j.final.Rev, "queue_ms": queueMs,
		"audio_s": float64(j.end-j.start) / sampleRate})
	r, broken, err := c.h.recognize(c.ctx, j.id, j.pcm, flip)
	inferMs := time.Since(started).Milliseconds()
	if c.ctx.Err() != nil {
		return true // cancelled: nothing late reaches the session
	}
	if broken == "deferred" {
		c.h.close()
		c.h = nil
		return false
	}
	j.pcm = nil
	if err != nil {
		c.log("correction_error", map[string]any{"job": j.id, "segment": j.final.Segment, "why": broken, "detail": err.Error(), "infer_ms": inferMs})
		if broken != "" && broken != "request" {
			c.h.close()
			c.h = nil
			if c.restarts++; c.restarts > corrMaxRestarts {
				c.disable(broken)
			}
		}
		return true
	}
	c.log("correction_inference", map[string]any{"job": j.id, "segment": j.final.Segment, "rev": j.final.Rev, "infer_ms": inferMs,
		"decode_s": r.decodeS, "queue_ms": queueMs, "text": r.text, "sherpa_text": j.final.Text})
	c.emit(Event{Session: c.session, Kind: Correction, Segment: j.final.Segment, Rev: j.final.Rev, Text: r.text, At: time.Now(),
		FedS: j.final.FedS, AudioS: j.final.AudioS, TokenEndS: j.final.TokenEndS,
		Corr: &CorrInfo{Job: j.id, StartS: float64(j.start) / sampleRate, EndS: float64(j.end) / sampleRate, PrerollS: float64(j.preroll) / sampleRate,
			FinalAt: j.final.At, QueueMs: queueMs, InferMs: inferMs, LeadRisk: j.lead, TailRisk: j.tail}})
	return true
}

// close cancels what is queued or running and returns once the helper is
// gone. Late submits are dropped. It may be called more than once.
func (c *corrector) close() {
	c.mu.Lock()
	c.closed = true
	c.queue = nil
	c.mu.Unlock()
	c.cancel()
	<-c.done
}

// corrHelper is a running qwen_correct.py over the installed runtime.
type corrHelper struct {
	p       *proc
	pid     int
	stdin   io.WriteCloser
	results chan corrResult
}

type corrResult struct {
	id      uint64
	text    string
	decodeS float64
	err     string
}

func startCorrHelper(ctx context.Context, cfg Config, flip <-chan struct{}) (*corrHelper, error) {
	root := cfg.QwenRoot
	py, model := filepath.Join(root, "runtime/bin/python3.11"), filepath.Join(root, "model")
	for _, p := range []string{py, model} {
		if _, err := os.Stat(p); err != nil {
			return nil, fail(ErrQwenMissing, "%s", p)
		}
	}
	script := filepath.Join(cfg.WorkDir, "qwen_correct.py")
	if err := os.WriteFile(script, correctScript, 0o600); err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	logOut, _ := os.Create(filepath.Join(cfg.WorkDir, "qwen-correct.log"))
	cmd := exec.Command(py, "-I", script, "--model-dir", model)
	cmd.Dir = cfg.WorkDir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1",
		"TOKENIZERS_PARALLELISM=false", "PYTHONUNBUFFERED=1"}
	cmd.Stderr = logOut
	p := newProc(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	h := &corrHelper{p: p, pid: cmd.Process.Pid, stdin: stdin, results: make(chan corrResult, 4)}
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var m struct {
				Type    string  `json:"type"`
				ID      *uint64 `json:"id"`
				Text    string  `json:"text"`
				Message string  `json:"message"`
				DecodeS float64 `json:"decode_s"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			// One request runs at a time; an answer nobody waits for any
			// more is dropped rather than block the reader (and the reap).
			deliver := func(r corrResult) {
				select {
				case h.results <- r:
				default:
				}
			}
			switch {
			case m.Type == "ready":
				trySend(ready, nil)
			case m.Type == "result" && m.ID != nil:
				deliver(corrResult{id: *m.ID, text: strings.TrimSpace(m.Text), decodeS: m.DecodeS})
			case m.Type == "error" && m.ID != nil:
				deliver(corrResult{id: *m.ID, err: m.Message})
			case m.Type == "error":
				trySend(ready, fail(ErrEngineStart, "qwen correction: %s", m.Message))
			}
		}
		werr := p.wait()
		if logOut != nil {
			logOut.Close()
		}
		trySend(ready, fail(ErrEngineStart, "qwen correction exited: %v", werr))
	}()
	select {
	case err := <-ready:
		if err != nil {
			h.close()
			return nil, err
		}
	case <-ctx.Done():
		h.close()
		return nil, ctx.Err()
	case <-flip:
		// Loading reads the whole model: dictation's own model goes first.
		h.close()
		return nil, errDeferred
	case <-time.After(qwenReadyTimeout):
		h.close()
		return nil, fail(ErrEngineStart, "qwen correction not ready within %s", qwenReadyTimeout)
	}
	return h, nil
}

// recognize sends one range and waits for its text. broken says why the
// helper cannot be used further ("timeout", "exited", "send"), "request"
// for a failure of this request alone, or "deferred" when flip closed.
func (h *corrHelper) recognize(ctx context.Context, id uint64, pcm []byte, flip <-chan struct{}) (corrResult, string, error) {
	hdr, _ := json.Marshal(map[string]any{"id": id, "bytes": len(pcm), "language": corrLanguage})
	sent := make(chan error, 1)
	// A wedged helper stops reading: the write must not hold the loop, and
	// close unblocks it.
	go func() {
		_, err := h.stdin.Write(append(hdr, '\n'))
		if err == nil {
			_, err = h.stdin.Write(pcm)
		}
		sent <- err
	}()
	timer := time.NewTimer(corrInferT)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return corrResult{}, "", ctx.Err()
		case <-flip:
			return corrResult{}, "deferred", errDeferred
		case err := <-sent:
			if err != nil {
				return corrResult{}, "send", err
			}
			sent = nil
		case r := <-h.results:
			if r.id != id {
				continue // an answer to a request given up on before
			}
			if r.err != "" {
				return r, "request", fail(ErrEngineFailed, "%s", r.err)
			}
			return r, "", nil
		case <-h.p.waited:
			return corrResult{}, "exited", fail(ErrEngineFailed, "helper exited: %v", h.p.err)
		case <-timer.C:
			return corrResult{}, "timeout", fail(ErrEngineFailed, "no result within %s", corrInferT)
		}
	}
}

// close ends input and the process group, and returns once it is gone or
// the bound passed. It may be called more than once.
func (h *corrHelper) close() {
	h.stdin.Close()
	h.p.stop(3 * time.Second)
}
