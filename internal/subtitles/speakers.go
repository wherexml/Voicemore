package subtitles

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Same-session anonymous speaker labels (docs/public/LIVE-SUBTITLES.md):
// a helper process runs sherpa-onnx OfflineSpeakerDiarization over rolling
// windows of the audio sherpa recognizes, on one CPU thread at low
// priority, one window at a time. Each window commits the turns of a range
// it saw with context on both sides; its local clusters are matched to the
// subtitle session's speakers by embedding similarity in a SpeakerTracker,
// which lives in memory with the subtitle session (across pauses and
// engine restarts) and is never saved. It is optional like correction: it
// starts after the first final, never gates capture, partials or
// translation, yields to dictation (Config.Gate), and a missing model or a
// failing helper only ends speaker labels for the engine session.

//go:embed speaker_diarize.py
var speakerScript []byte

const (
	spkWindow     = 12 * sampleRate      // samples a window holds
	spkHop        = 5 * sampleRate       // samples each window commits
	spkTail       = sampleRate * 3 / 2   // samples of right context a commit keeps back
	spkMaxBehind  = 3 * spkHop           // backlog beyond this is skipped as unknown
	spkInferT     = 8 * time.Second      // per window
	spkMaxRestart = 1
	spkReadyT     = 60 * time.Second

	// Matching of a window's clusters to the session's speakers. Provisional:
	// chosen from unannotated model output on one development sample
	// (artifacts/native-subtitle-speakers-20261025), where turns the
	// diarizer put in one cluster scored about 0.85 and most other pairs
	// 0.1–0.55. No human speaker reference verified them.
	spkMatchSim   = 0.6
	spkUpdateSim  = 0.65
	spkNewSpeechS = 1.5  // a cluster with less unmatched speech stays unknown
	spkMaxWeightS = 30.0 // a centroid stops moving much after this much speech
	SpeakerMax    = 16   // speakers a session tracks; more are unknown
)

// SpeakerTurn is a committed turn in session samples. Speaker is the
// session speaker (1…), or 0 when the turn's cluster matched none.
type SpeakerTurn struct {
	Start, End int64
	Speaker    int
}

// SpeakerInfo is what a Speakers event commits: every turn of [From, To).
// Gap says the range was skipped (backlog or evicted audio): unknown.
type SpeakerInfo struct {
	Window   uint64
	From, To int64
	Turns    []SpeakerTurn
	Gap      bool
	InferMs  int64
}

// SpeakerTracker holds a subtitle session's speakers as embedding
// centroids, in memory only. Its ids are stable for the tracker's life;
// Reset starts a new session. Safe for concurrent use.
type SpeakerTracker struct {
	mu    sync.Mutex
	gen   uint64
	next  int
	cents []centroid
}

type centroid struct {
	id     int
	v      []float64
	weight float64
}

func NewSpeakerTracker() *SpeakerTracker { return &SpeakerTracker{next: 1} }

// Reset forgets every voice; new speakers are numbered from next on, so
// they never take the id of a restored, name-only speaker.
func (t *SpeakerTracker) Reset(next int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gen++
	t.cents, t.next = nil, max(1, next)
}

// Gen changes with every Reset: results of an older one are stale.
func (t *SpeakerTracker) Gen() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gen
}

type spkCluster struct {
	Speaker   int       `json:"speaker"`
	SpeechS   float64   `json:"speech_s"`
	Embedding []float64 `json:"embedding"`
}

// match maps a window's local clusters to session speakers one to one, by
// the most similar pair first. An unmatched cluster with enough speech
// becomes a new speaker while there is room; else it is unknown (0).
func (t *SpeakerTracker) match(gen uint64, cl []spkCluster) (map[int]int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if gen != t.gen {
		return nil, false
	}
	type pair struct {
		l, c int
		sim  float64
	}
	var pairs []pair
	for i, k := range cl {
		if len(k.Embedding) == 0 {
			continue
		}
		for j, c := range t.cents {
			if len(c.v) == len(k.Embedding) {
				pairs = append(pairs, pair{i, j, cosine(c.v, k.Embedding)})
			}
		}
	}
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].sim > pairs[b].sim })
	out := map[int]int{}
	usedL, usedC := map[int]bool{}, map[int]bool{}
	for _, p := range pairs {
		if p.sim < spkMatchSim || usedL[p.l] || usedC[p.c] {
			continue
		}
		usedL[p.l], usedC[p.c] = true, true
		c := &t.cents[p.c]
		out[cl[p.l].Speaker] = c.id
		if p.sim >= spkUpdateSim && cl[p.l].SpeechS >= 1 {
			c.add(cl[p.l].Embedding, cl[p.l].SpeechS)
		}
	}
	for i, k := range cl {
		if usedL[i] {
			continue
		}
		out[k.Speaker] = 0
		if len(k.Embedding) > 0 && k.SpeechS >= spkNewSpeechS && len(t.cents) < SpeakerMax {
			c := centroid{id: t.next}
			c.add(k.Embedding, k.SpeechS)
			t.next++
			t.cents = append(t.cents, c)
			out[k.Speaker] = c.id
		}
	}
	return out, true
}

func (c *centroid) add(v []float64, w float64) {
	if c.v == nil {
		c.v = make([]float64, len(v))
	}
	w = min(w, spkMaxWeightS)
	total := c.weight + w
	for i := range v {
		c.v[i] = (c.v[i]*c.weight + v[i]*w) / total
	}
	c.weight = min(total, spkMaxWeightS)
	normalize(c.v)
}

func normalize(v []float64) {
	var n float64
	for _, x := range v {
		n += x * x
	}
	if n = math.Sqrt(n); n > 0 {
		for i := range v {
			v[i] /= n
		}
	}
}

func cosine(a, b []float64) float64 {
	var d, na, nb float64
	for i := range a {
		d += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return d / math.Sqrt(na*nb)
}

// SpeakerModels are the files a speaker root must hold.
func SpeakerModels(root string) (segmentation, embedding string) {
	return filepath.Join(root, "segmentation.onnx"), filepath.Join(root, "embedding.onnx")
}

type speakerWorker struct {
	cfg     Config
	session uint64
	gen     uint64
	emit    func(Event)
	ring    *pcmRing
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	wake    chan struct{}

	mu       sync.Mutex
	warmed   bool
	disabled bool
	fed      int64 // samples in the ring

	// The loop goroutine's own.
	h          *spkHelper
	restarts   int
	commit     atomic.Int64 // samples committed so far; fedTo reads it
	seq        uint64
	deferredAt time.Time
}

func newSpeakerWorker(ctx context.Context, cfg Config, session uint64, emit func(Event), ring *pcmRing) *speakerWorker {
	w := &speakerWorker{cfg: cfg, session: session, gen: cfg.Speakers.Gen(), emit: emit, ring: ring,
		done: make(chan struct{}), wake: make(chan struct{}, 1)}
	w.ctx, w.cancel = context.WithCancel(ctx)
	cfg.log("speaker_init", map[string]any{"session": session, "window_s": spkWindow / sampleRate, "hop_s": spkHop / sampleRate,
		"tail_s": float64(spkTail) / sampleRate, "match_sim": spkMatchSim, "root": cfg.SpeakerRoot})
	go w.loop()
	return w
}

func (w *speakerWorker) log(event string, fields map[string]any) {
	fields["session"] = w.session
	w.cfg.log(event, fields)
}

func (w *speakerWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// warm lets the helper load: called for every final, so the first
// subtitles and their translations are out before it competes.
func (w *speakerWorker) warm() {
	w.mu.Lock()
	first := !w.warmed
	w.warmed = true
	w.mu.Unlock()
	if first {
		w.signal()
	}
}

// fedTo notes the samples now in the ring; it never blocks.
func (w *speakerWorker) fedTo(samples int64) {
	w.mu.Lock()
	w.fed = samples
	due := w.warmed && !w.disabled && samples >= w.commitTarget()
	w.mu.Unlock()
	if due {
		w.signal()
	}
}

// commitTarget is the ring end at which the next window is due.
func (w *speakerWorker) commitTarget() int64 { return w.commit.Load() + spkHop + spkTail }

func (w *speakerWorker) state() (warmed, disabled bool, fed int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.warmed, w.disabled, w.fed
}

func (w *speakerWorker) disable(why string) {
	w.mu.Lock()
	w.disabled = true
	w.mu.Unlock()
	w.log("speaker_disabled", map[string]any{"why": why})
	w.emit(Event{Session: w.session, Kind: SpeakersOff, Code: why, At: time.Now()})
}

func (w *speakerWorker) loop() {
	defer close(w.done)
	defer func() {
		if w.h != nil {
			w.h.close()
		}
	}()
	for {
		busy, flip := w.cfg.Gate.State()
		if busy {
			w.deferFor("idle", 0)
		} else {
			w.resume()
			w.work(flip)
		}
		select {
		case <-w.ctx.Done():
			return
		case <-w.wake:
		case <-flip:
		}
	}
}

func (w *speakerWorker) deferFor(during string, window uint64) {
	first := w.deferredAt.IsZero()
	if first {
		w.deferredAt = time.Now()
	}
	if first || during != "idle" {
		w.log("speaker_deferred", map[string]any{"why": "dictation", "during": during, "window": window, "loaded": w.h != nil})
	}
}

func (w *speakerWorker) resume() {
	if w.deferredAt.IsZero() {
		return
	}
	d := time.Since(w.deferredAt)
	w.deferredAt = time.Time{}
	_, _, fed := w.state()
	w.log("speaker_resumed", map[string]any{"deferred_ms": d.Milliseconds(), "behind_s": float64(fed-w.commit.Load()) / sampleRate})
}

// work commits every window that is due while dictation stays idle.
func (w *speakerWorker) work(flip <-chan struct{}) {
	for w.ctx.Err() == nil && !isClosed(flip) {
		warmed, disabled, fed := w.state()
		if !warmed || disabled {
			return
		}
		end := w.commitTarget()
		if fed < end {
			return
		}
		// Too far behind: skip to the newest window, the skipped audio unknown.
		if fed-end > spkMaxBehind {
			to := fed - spkTail - spkHop
			w.gap(to, "behind")
			continue
		}
		if w.h == nil {
			h, err := startSpkHelper(w.ctx, w.cfg, flip)
			if err == errDeferred {
				w.deferFor("loading", 0)
				return
			}
			if err != nil {
				if w.ctx.Err() == nil {
					code := ErrEngineStart
					if e, ok := err.(*Error); ok {
						code = e.Code
					}
					w.log("speaker_unavailable", map[string]any{"code": code, "detail": err.Error()})
					w.disable(code)
				}
				return
			}
			w.h = h
			w.log("speaker_ready", map[string]any{"pid": h.pid, "load_ms": h.loadMs})
		}
		start := max(0, end-spkWindow)
		pcm, ok := w.ring.slice(start, end)
		if !ok {
			w.gap(end-spkTail, "evicted")
			continue
		}
		w.seq++
		id := w.seq
		started := time.Now()
		r, broken, err := w.h.diarize(w.ctx, id, pcm, flip)
		inferMs := time.Since(started).Milliseconds()
		if w.ctx.Err() != nil {
			return
		}
		if broken == "deferred" {
			// The window stays due: it runs again once dictation is idle.
			w.h.close()
			w.h = nil
			w.deferFor("inference", id)
			return
		}
		if err != nil {
			w.log("speaker_error", map[string]any{"window": id, "why": broken, "detail": err.Error(), "infer_ms": inferMs})
			if broken != "request" {
				w.h.close()
				w.h = nil
				if w.restarts++; w.restarts > spkMaxRestart {
					w.disable(broken)
					return
				}
				continue // the same window, once more on a new helper
			}
			w.gap(end-spkTail, "error")
			continue
		}
		w.commitWindow(id, start, end, r, inferMs)
	}
}

// gap commits [commit, to) as unknown.
func (w *speakerWorker) gap(to int64, why string) {
	from := w.commit.Load()
	if to <= from {
		return
	}
	info := &SpeakerInfo{From: from, To: to, Gap: true}
	w.log("speaker_gap", map[string]any{"from_s": float64(info.From) / sampleRate, "to_s": float64(to) / sampleRate, "why": why})
	w.commit.Store(to)
	w.emit(Event{Session: w.session, Kind: Speakers, Spk: info, At: time.Now()})
}

func (w *speakerWorker) commitWindow(id uint64, start, end int64, r spkResult, inferMs int64) {
	ids, ok := w.cfg.Speakers.match(w.gen, r.Clusters)
	if !ok {
		return // the subtitle session was reset meanwhile
	}
	from, to := w.commit.Load(), end-spkTail
	info := &SpeakerInfo{Window: id, From: from, To: to, InferMs: inferMs}
	unknown := 0
	for _, t := range r.Turns {
		a, b := start+int64(t.Start*sampleRate), start+int64(t.End*sampleRate)
		a, b = max(a, from), min(b, to)
		if b <= a {
			continue
		}
		spk := ids[t.Speaker]
		if spk == 0 {
			unknown++
		}
		info.Turns = append(info.Turns, SpeakerTurn{Start: a, End: b, Speaker: spk})
	}
	w.commit.Store(to)
	speakers := []int{}
	for _, v := range ids {
		speakers = append(speakers, v)
	}
	w.log("speaker_window", map[string]any{"window": id, "start_s": float64(start) / sampleRate, "end_s": float64(end) / sampleRate,
		"from_s": float64(from) / sampleRate, "to_s": float64(to) / sampleRate, "turns": len(info.Turns), "unknown_turns": unknown,
		"local_clusters": len(r.Clusters), "speakers": speakers, "infer_ms": inferMs, "helper_infer_s": r.InferS})
	w.emit(Event{Session: w.session, Kind: Speakers, Spk: info, At: time.Now()})
}

// close cancels the window running and returns once the helper is gone.
func (w *speakerWorker) close() {
	w.cancel()
	<-w.done
}

// spkHelper is a running speaker_diarize.py on the sherpa venv.
type spkHelper struct {
	p       *proc
	pid     int
	loadMs  int64
	stdin   io.WriteCloser
	results chan spkResult
}

type spkResult struct {
	id     uint64
	err    string
	InferS float64 `json:"infer_s"`
	Turns  []struct {
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Speaker int     `json:"speaker"`
	} `json:"turns"`
	Clusters []spkCluster `json:"clusters"`
}

func startSpkHelper(ctx context.Context, cfg Config, flip <-chan struct{}) (*spkHelper, error) {
	py := SherpaPython(cfg.SherpaRoot)
	seg, emb := SpeakerModels(cfg.SpeakerRoot)
	for _, p := range []string{py, seg, emb} {
		if _, err := os.Stat(p); err != nil {
			return nil, fail(ErrSpeakerMissing, "%s", p)
		}
	}
	script := filepath.Join(cfg.WorkDir, "speaker_diarize.py")
	if err := os.WriteFile(script, speakerScript, 0o600); err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	logOut, _ := os.Create(filepath.Join(cfg.WorkDir, "speakers.log"))
	cmd := exec.Command(py, "-I", script, "--segmentation", seg, "--embedding", emb, "--threads", "1")
	cmd.Dir = cfg.WorkDir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "PYTHONUNBUFFERED=1",
		"OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1", "VECLIB_MAXIMUM_THREADS=1"}
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
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fail(ErrEngineStart, "%v", err)
	}
	// Optional work: below capture, sherpa and the app.
	syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	h := &spkHelper{p: p, pid: cmd.Process.Pid, stdin: stdin, results: make(chan spkResult, 4)}
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			var m struct {
				Type    string  `json:"type"`
				ID      *uint64 `json:"id"`
				Message string  `json:"message"`
				spkResult
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			deliver := func(r spkResult) {
				select {
				case h.results <- r:
				default:
				}
			}
			switch {
			case m.Type == "ready":
				trySend(ready, nil)
			case m.Type == "result" && m.ID != nil:
				r := m.spkResult
				r.id = *m.ID
				deliver(r)
			case m.Type == "error" && m.ID != nil:
				deliver(spkResult{id: *m.ID, err: m.Message})
			case m.Type == "error":
				trySend(ready, fail(ErrSpeakerMissing, "speakers: %s", m.Message))
			}
		}
		werr := p.wait()
		if logOut != nil {
			logOut.Close()
		}
		trySend(ready, fail(ErrEngineStart, "speaker helper exited: %v", werr))
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
		h.close()
		return nil, errDeferred
	case <-time.After(spkReadyT):
		h.close()
		return nil, fail(ErrEngineStart, "speaker helper not ready within %s", spkReadyT)
	}
	h.loadMs = time.Since(started).Milliseconds()
	return h, nil
}

// diarize sends one window and waits for its turns. broken is as in
// corrHelper.recognize.
func (h *spkHelper) diarize(ctx context.Context, id uint64, pcm []byte, flip <-chan struct{}) (spkResult, string, error) {
	hdr, _ := json.Marshal(map[string]any{"id": id, "bytes": len(pcm)})
	sent := make(chan error, 1)
	go func() {
		_, err := h.stdin.Write(append(hdr, '\n'))
		if err == nil {
			_, err = h.stdin.Write(pcm)
		}
		sent <- err
	}()
	timer := time.NewTimer(spkInferT)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return spkResult{}, "", ctx.Err()
		case <-flip:
			return spkResult{}, "deferred", errDeferred
		case err := <-sent:
			if err != nil {
				return spkResult{}, "send", err
			}
			sent = nil
		case r := <-h.results:
			if r.id != id {
				continue
			}
			if r.err != "" {
				return r, "request", fail(ErrEngineFailed, "%s", r.err)
			}
			return r, "", nil
		case <-h.p.waited:
			return spkResult{}, "exited", fail(ErrEngineFailed, "speaker helper exited: %v", h.p.err)
		case <-timer.C:
			return spkResult{}, "timeout", fail(ErrEngineFailed, "no speaker result within %s", spkInferT)
		}
	}
}

func (h *spkHelper) close() {
	h.stdin.Close()
	h.p.stop(2 * time.Second)
}
