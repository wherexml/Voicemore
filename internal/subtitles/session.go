// Package subtitles runs live subtitles of system audio: the bundled
// VoicemoreAudioTap helper captures real output as 16 kHz mono PCM16, and
// one of two engines recognizes it as it plays: an isolated copy of the
// installed Qwen worker with short segments, or a sherpa-onnx streaming
// recognizer whose finished segments the installed Qwen model recognizes
// again in the background (correct.go). It never touches dictation's
// models, ports or settings.
// See docs/public/LIVE-SUBTITLES.md.
package subtitles

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

const (
	EngineQwen   = "qwen"
	EngineSherpa = "sherpa"

	sampleRate  = 16000
	chunkBytes  = sampleRate / 10 * 2 // 100 ms of PCM16
	queueChunks = 50                  // 5 s of audio between capture and engine
)

// Kind is what an Event reports.
type Kind int

const (
	Status  Kind = iota // Code: "loading", "listening" or "silent"
	Partial             // the segment's text so far
	Final               // the segment's last text
	Correction          // Qwen's text of a Final's audio; Rev is that Final's
	Speakers            // Spk: the committed speaker turns of a range of session audio
	SpeakersOff         // Code: why speaker labels ended for the session
)

// Event is one report of a session. Segment and Rev order the text: a
// segment's revisions only grow, and a Final ends it.
type Event struct {
	Session uint64
	Kind    Kind
	Code    string
	Segment int
	Rev     int
	Text    string
	// Diagnostics of when the text's audio played, for latency analysis.
	// FedS is how much session audio had been fed to the engine when the
	// event arrived: an upper bound of the text's audio end, not its end.
	// AudioS and TokenEndS are what the engine reported (seconds, -1 when it
	// reports none); their reference point is the engine's, see the module spec.
	At        time.Time
	FedS      float64
	AudioS    float64
	TokenEndS float64
	// Corr describes a Correction's audio range and timing.
	Corr *CorrInfo
	// A sherpa Final's audio on the decoded clock, in session samples
	// (-1 when unknown), and its tokens with their times, for speaker
	// alignment. Partials and Qwen events have none.
	Start, End int64
	Tokens     []Token
	// Spk is a Speakers event's range and turns.
	Spk *SpeakerInfo
}

// Token is one recognizer token and the session sample it was emitted at.
type Token struct {
	Text   string
	Sample int64
}

// CorrInfo is where a Correction's audio was and how long it took.
type CorrInfo struct {
	Job          uint64
	StartS, EndS float64   // the range Qwen heard, pre-roll included, in session audio
	PrerollS     float64   // audio before the decoded segment start
	FinalAt      time.Time // when the Final it corrects came from the engine
	QueueMs      int64     // from the Final to the start of inference
	InferMs      int64     // inference, including the transfer to the helper
	// LeadRisk and TailRisk say a token of the Final sits at its range's
	// start or end, where a word may be cut: then Qwen's text must keep
	// sherpa's first or last word to be taken.
	LeadRisk, TailRisk bool
}

// timing is what an engine reports with a text. A sherpa final also has
// its segment's range, else rng is nil.
type timing struct {
	audioS, tokenEndS float64
	rng               *segRange
	tokens            []Token
}

var noTiming = timing{audioS: -1, tokenEndS: -1}

// Error is a failure with a stable code for the interface; Detail is for
// diagnostics only.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

// Codes of Error.
const (
	ErrTapMissing     = "tap_missing"     // the capture helper is not in the app bundle
	ErrCapture        = "capture_failed"  // the helper could not tap system audio
	ErrNoCallbacks    = "no_callbacks"    // the tap delivered no buffers
	ErrQwenMissing    = "qwen_missing"    // no installed local Qwen runtime/model
	ErrQwenWorker     = "qwen_worker"     // the installed worker is not the expected version
	ErrSherpaMissing  = "sherpa_missing"  // no sherpa runtime/model at the configured root
	ErrEngineStart    = "engine_start"    // the engine did not become ready
	ErrEngineFailed   = "engine_failed"   // the engine stopped or reported an error
	ErrAudioBackpress = "audio_backpress" // the engine fell behind beyond the bounded queue
	ErrSpeakerMissing = "speaker_missing" // no speaker models or runtime: labels only, never the session
)

func fail(code, format string, args ...any) error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Config is what a session runs.
type Config struct {
	Engine   string
	Language string // Qwen: "zh", "auto" or "en"
	TapPath  string // the VoicemoreAudioTap executable
	// QwenRoot is the installed local-asr directory, used read-only.
	QwenRoot string
	// SherpaRoot holds venv/ (with sherpa_onnx) and model/.
	SherpaRoot string
	// WorkDir is this app's own directory for the worker copy, the sherpa
	// script and the engine logs.
	WorkDir string
	// Correction re-recognizes each sherpa final's audio with QwenRoot in
	// the background. It never gates capture, partials or translation.
	Correction bool
	// Gate is the app's dictation state: correction and speaker labels
	// yield while it is busy.
	Gate *Gate
	// Speakers labels sherpa sessions with the subtitle session's speakers
	// (speakers.go); nil is off. SpeakerRoot holds segmentation.onnx and
	// embedding.onnx; the helper runs on SherpaRoot's venv.
	Speakers    *SpeakerTracker
	SpeakerRoot string
	// Log records a diagnostic event; it may be nil. It never gets audio.
	Log func(event string, fields map[string]any)
}

func (c Config) log(event string, fields map[string]any) {
	if c.Log != nil {
		c.Log(event, fields)
	}
}

// engine is a running recognizer: it takes PCM chunks and reports text.
type engine interface {
	feed(ctx context.Context, pcm []byte) error
	close()
}

// Run captures and recognizes until ctx ends or something fails. emit gets
// every event of the session, from Run's goroutines. A cancelled ctx
// returns nil. Before it returns, every process it started has exited:
// each close sends SIGTERM, then SIGKILL after a grace period, and waits
// (capture 2 s, sherpa 3 s, Qwen 5 s, plus 2 s after SIGKILL). Each engine
// and the capture also close themselves when ctx ends, so a feed blocked on
// a hung child cannot keep them alive.
func Run(ctx context.Context, id uint64, cfg Config, emit func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rev := map[int]int{}
	var fed atomic.Int64 // PCM bytes fed to the engine
	// The corrector exists before the engine reports anything, so text may
	// use it from the engine's goroutine; it loads Qwen only once the first
	// partial is out.
	// Correction and speaker labels read the same ring of fed audio.
	var ring *pcmRing
	if cfg.Engine == EngineSherpa && (cfg.Correction || cfg.Speakers != nil) {
		ring = &pcmRing{buf: make([]byte, corrRingSeconds*sampleRate*2)}
	}
	var corr *corrector
	if cfg.Engine == EngineSherpa && cfg.Correction {
		corr = newCorrector(ctx, cfg, id, emit, ring)
		defer corr.close()
	}
	var spk *speakerWorker
	if cfg.Engine == EngineSherpa && cfg.Speakers != nil {
		spk = newSpeakerWorker(ctx, cfg, id, emit, ring)
		defer spk.close()
	}
	text := func(kind Kind, segment int, s string, t timing) {
		rev[segment]++
		ev := Event{Session: id, Kind: kind, Segment: segment, Rev: rev[segment], Text: s,
			At: time.Now(), FedS: float64(fed.Load()) / 2 / sampleRate, AudioS: t.audioS, TokenEndS: t.tokenEndS,
			Start: -1, End: -1, Tokens: t.tokens}
		if t.rng != nil {
			ev.Start, ev.End = t.rng.start, t.rng.end
		}
		emit(ev)
		if spk != nil && kind == Final && s != "" {
			spk.warm()
		}
		switch {
		case corr == nil || s == "":
		case kind == Partial:
			corr.warm()
		case kind == Final:
			corr.submit(ev, t.rng)
		}
	}
	status := func(code string) { emit(Event{Session: id, Kind: Status, Code: code}) }

	status("loading")
	started := time.Now()
	var eng engine
	var engErr <-chan error
	var err error
	switch cfg.Engine {
	case EngineSherpa:
		eng, engErr, err = startSherpa(ctx, cfg, text)
	default:
		eng, engErr, err = startQwen(ctx, cfg, text)
	}
	if err != nil {
		return cancelled(ctx, err)
	}
	defer eng.close()
	cfg.log("engine_ready", map[string]any{"session": id, "engine": cfg.Engine, "load_ms": time.Since(started).Milliseconds()})

	capt, err := startCapture(ctx, cfg, id)
	if err != nil {
		return cancelled(ctx, err)
	}
	defer capt.close()
	status("listening")

	silent := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-capt.silent:
			if !silent {
				silent = true
				status("silent")
			}
		case err := <-engErr:
			return cancelled(ctx, err)
		case err := <-capt.done:
			return cancelled(ctx, err)
		case pcm := <-capt.pcm:
			if silent && nonzero(pcm) {
				silent = false
				status("listening")
			}
			if fed.Load() == 0 {
				cfg.log("pcm_first", map[string]any{"session": id, "note": "wall time of the first fed chunk; audio position 0 of FedS"})
			}
			if ring != nil {
				// Before the engine has it, so a range it reports is always here.
				ring.add(pcm)
			}
			if err := eng.feed(ctx, pcm); err != nil {
				return cancelled(ctx, err)
			}
			fed.Add(int64(len(pcm)))
			if spk != nil {
				spk.fedTo(fed.Load() / 2)
			}
		}
	}
}

// cancelled is nil for the failures a cancelled session causes itself.
func cancelled(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	var e *Error
	if err != nil && !errors.As(err, &e) {
		err = &Error{Code: ErrEngineFailed, Detail: err.Error()}
	}
	return err
}

func nonzero(pcm []byte) bool {
	for _, b := range pcm {
		if b != 0 {
			return true
		}
	}
	return false
}
