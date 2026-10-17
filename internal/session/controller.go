// Package session runs a dictation from the trigger key to the text in the
// target input: recording, the local ASR, the optional cleanup, the
// insertion and the history record. One dictation runs at a time, and a
// cancelled one can no longer write anything.
package session

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/store"
	"voiceinput/internal/textstyle"
)

type Phase int

const (
	Idle Phase = iota
	Preparing
	Recording
	Finalizing
	Cleaning
	Inserting
	Done
	Failed
)

// Levels is how many recent microphone levels the overlay shows.
const Levels = 9

// voicedLevel is the level above which the overlay shows sound. It only
// drives the picture: the ASR service decides what is speech.
const voicedLevel = 0.12

// View is what the overlay shows of the current dictation.
type View struct {
	TranslationLanguage string
	Phase               Phase
	Levels              [Levels]float32 // 0 to 1, the newest last
	Cleanup             bool
	// Final is transcript that no longer changes, Pending what may.
	Final, Pending string
	// Message says what failed, or what a finished dictation did.
	Message string
	// Text is what a failed dictation still holds, to copy.
	Text string
	// CleanupFailed offers trying the cleanup again, or copying.
	CleanupFailed bool
	// Retained says the dictation ended without inserting but holds text
	// to copy: the insertion failed, or the recording or the recognition
	// broke off with words already heard. The phase is Failed; the overlay
	// shows it as a neutral notice, not as an error. A failed cleanup is
	// not this: it offers a retry.
	Retained bool
	// Notice, with Retained, is the reason to show above the text: that
	// the microphone is gone, or why the recognition ended. Empty for a
	// failed insertion, where the overlay only says the text is kept.
	Notice string
}

type Controller struct {
	Settings *config.Store
	History  *store.Store
	Dict     *dict.Dict

	// OnChange is called, from any goroutine, when the view changed, and
	// OnHistory when a record did. CopyText puts text on the clipboard.
	OnChange    func()
	OnHistory   func()
	CopyText    func(string)
	SendPreview func(context.Context, string, *macos.Target, time.Duration, func(string)) bool

	// Tr gives the messages in the user's language and Explain the errors
	// of lower layers; without them both are English. CleanupKey returns
	// the cleanup API key from the app's secure storage.
	Tr         func(key string, args ...any) string
	Explain    func(error) string
	CleanupKey func() string

	// ResolveASR and ResolveCleanup turn the default models of the
	// settings into where to connect. Without them the local services of
	// the settings are used, as before there were providers. An error
	// means the default cannot serve (not set, not configured, or a
	// provider this app has no adapter for): nothing else is tried.
	PrepareASR     func(context.Context, config.Settings) (func(), error)
	ResolveASR     func(config.Settings) (asr.Dialer, error)
	ResolveCleanup func(config.Settings) (cleanup.Options, error)

	mu             sync.Mutex
	cur            *run
	view           View
	cleanupNext    *bool // the user's choice for the next dictation, if made
	retranscribing bool

	notifyOnce sync.Once
	notify     chan struct{}
}

// run is one dictation. Its fields are guarded by Controller.mu, except
// those a single goroutine owns.
type run struct {
	id              string
	started         time.Time
	target          *macos.Target
	capture         *macos.Capture
	ctx             context.Context
	stop            context.CancelFunc // cancels everything in flight
	confirm         chan struct{}      // closed when the user ends the recording
	confirmed       bool
	ready           chan struct{} // closed only after the first PCM buffer is retained
	lost            chan struct{} // closed when the microphone disappeared while recording
	readyOnce       sync.Once
	captureStopOnce sync.Once // schedules hardware shutdown once, independently of text
	cueEnded        bool      // the recording-end cue has already been scheduled
	cancelled       bool
	inserted        bool // an insertion was attempted: never a second one
	translation     string
	viaHold         bool
	cleanup         bool
	// holdCopied keeps "copied to the clipboard" up until it is dismissed,
	// as the V2 capsule has it; the classic one hides it after a while.
	// Read from the settings when the dictation starts.
	holdCopied bool

	pcm     []byte
	more    chan struct{} // signals new samples to the sender
	stopAt  time.Time
	finalAt time.Time
	firstMs int64
	text    string // the transcript after the dictionary's rules

	// rec is the run's record. finish prepares it in memory; persist writes
	// it, once the apps the run touched are known (privacy.go). Until then
	// note changes it here, afterwards in the history.
	rec      store.Record
	prepared bool // finish built rec
	recDone  bool // persist decided: written, or dropped as private
	recSaved bool // rec is in the history
	// private is sticky: the app at the start, or the one the text went
	// to, is a private app. Such a run is never written to the history, as
	// text or audio, and never observed for dictionary learning.
	private bool
	saving  sync.Mutex // held by persist, so that note waits for the write

	// sink, when set, makes this a practice run of the first-run guide:
	// its transcript is given to the sink instead of being inserted. Such
	// a run never calls macos.Insert, never touches the clipboard and is
	// never observed afterwards. It is set when the run is created and
	// never changed.
	sink func(text string)

	observing bool
	rel       sync.Once
}

// release frees the target, once.
func (r *run) release() { r.rel.Do(func() { r.target.Release() }) }

// changed tells the UI the view changed. It never blocks: the microphone's
// thread calls it, and so does code the main thread may be waiting for.
// Changes that come faster than the UI reads them are merged.
func (c *Controller) changed() {
	c.notifyOnce.Do(func() {
		c.notify = make(chan struct{}, 1)
		go func() {
			for range c.notify {
				if c.OnChange != nil {
					c.OnChange()
				}
			}
		}()
	})
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *Controller) historyChanged() {
	if c.OnHistory != nil {
		c.OnHistory()
	}
}

// View returns the state for the overlay.
func (c *Controller) View() View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view
}

// CleanupNext reports whether the next dictation is tidied: the user's
// choice for it, or the default of the settings.
func (c *Controller) CleanupNext() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cleanupNext != nil {
		return *c.cleanupNext
	}
	return c.Settings.Get().CleanupDefault
}

// SetCleanupNext chooses whether dictations are tidied, until the app
// quits; the default in the settings stays as it is.
func (c *Controller) SetCleanupNext(on bool) {
	c.mu.Lock()
	c.cleanupNext = &on
	c.mu.Unlock()
	c.changed()
}

// active reports whether r is still the current dictation and may act.
// The caller holds c.mu.
func (c *Controller) active(r *run) bool { return c.cur == r && !r.cancelled }

// KeyDown and KeyUp are the trigger key. In toggle mode a press and
// release of the key alone starts a dictation, and the next one ends it; in
// hold mode it lasts while the key is down. A key that repeats, or a second
// press while one is being finalized, starts nothing.
func (c *Controller) KeyDown() {
	if c.Settings.Get().TriggerMode != config.TriggerHold {
		return
	}
	c.mu.Lock()
	idle := c.cur == nil
	over := c.view.Phase == Done || c.view.Phase == Failed
	c.mu.Unlock()
	if over {
		c.Dismiss()
		idle = true
	}
	if idle {
		c.start(true)
	}
}

func (c *Controller) KeyUp(combo bool) {
	if c.Settings.Get().TriggerMode != config.TriggerHold {
		if !combo {
			c.Toggle()
		}
		return
	}
	c.mu.Lock()
	r := c.cur
	held := r != nil && r.viaHold && !r.confirmed
	short := r != nil && time.Since(r.started) < 300*time.Millisecond
	c.mu.Unlock()
	switch {
	case !held:
	case combo && short:
		c.Cancel() // Fn+key, not speech
	default:
		c.Confirm()
	}
}

// Toggle starts a dictation, or ends the one that is recording.
func (c *Controller) Toggle() {
	c.mu.Lock()
	r := c.cur
	phase := c.view.Phase
	c.mu.Unlock()
	switch {
	case r == nil:
		c.start(false)
	case phase == Preparing || phase == Recording:
		c.Confirm()
	case phase == Done || phase == Failed:
		c.Dismiss()
		c.start(false)
	}
}

func (c *Controller) start(viaHold bool) {
	c.startWithSink(viaHold, nil)
}

// ToggleTranslation ends a current recording or starts a translated dictation.
// The target is frozen at start, including across model retries.
func (c *Controller) ToggleTranslation() {
	c.mu.Lock()
	phase, active := c.view.Phase, c.cur != nil
	c.mu.Unlock()
	if active && (phase == Preparing || phase == Recording) {
		c.Confirm()
		return
	}
	if active && phase != Done && phase != Failed {
		return
	}
	c.Dismiss()
	language := c.Settings.Get().TranslationLanguage
	if language == "" {
		language = "en"
	}
	c.startConfigured(false, nil, language)
}
func (c *Controller) startWithSink(viaHold bool, sink func(string)) {
	c.startConfigured(viaHold, sink, "")
}
func (c *Controller) startConfigured(viaHold bool, sink func(string), language string) {
	s := c.Settings.Get()
	c.mu.Lock()
	if c.cur != nil || c.retranscribing {
		c.mu.Unlock()
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	r := &run{
		id: store.NewID(), started: time.Now(), ctx: ctx, stop: stop,
		confirm: make(chan struct{}), ready: make(chan struct{}), lost: make(chan struct{}), more: make(chan struct{}, 1), viaHold: viaHold,
		holdCopied: s.CapsuleStyle != config.CapsuleClassic,
	}
	if c.cleanupNext != nil {
		r.cleanup = *c.cleanupNext
	} else {
		r.cleanup = s.CleanupDefault
	}
	r.translation = language
	if language != "" {
		r.cleanup = true
	}
	r.sink = sink // assigned atomically with creating this run
	c.cur = r
	c.view = View{Phase: Preparing, Cleanup: r.cleanup, TranslationLanguage: language}
	c.mu.Unlock()
	c.changed()
	go c.record(r, s)
}

// StartPractice starts one dictation whose transcript goes to sink and
// nowhere else: no app is written to, the clipboard is left alone, and
// nothing is observed. The sink belongs to the run this call starts; if no
// run can start now (one is in progress), nothing is armed and a later
// dictation is an ordinary one. sink is called at most once, from a
// goroutine of the controller, with a transcript that is not empty, and
// only if the run was not cancelled.
func (c *Controller) StartPractice(sink func(text string)) {
	if sink == nil {
		return
	}
	c.Dismiss() // a finished or failed attempt can be retried
	c.startWithSink(false, sink)
}

// Practicing says the dictation in progress is a practice run.
func (c *Controller) Practicing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur != nil && c.cur.sink != nil
}

// CancelPractice abandons the practice run in progress, if there is one:
// its result is discarded and its sink is never called. An ordinary
// dictation is left alone.
func (c *Controller) CancelPractice() {
	c.cancel(true)
}

// Confirm ends the recording and lets the dictation finish and insert.
// Only the first call for a dictation does anything.
func (c *Controller) Confirm() {
	c.mu.Lock()
	r := c.cur
	if r == nil || r.confirmed || r.cancelled || (c.view.Phase != Preparing && c.view.Phase != Recording) {
		c.mu.Unlock()
		return
	}
	r.confirmed = true
	r.stopAt = time.Now()
	c.view.Phase = Finalizing
	capture := r.capture
	c.mu.Unlock()
	c.stopCapture(r, capture)
	close(r.confirm)
	c.changed()
}

// Cancel abandons the dictation: nothing is inserted, and results that
// arrive later are dropped. On a finished or failed one it closes the
// overlay.
func (c *Controller) Cancel() {
	c.cancel(false)
}

func (c *Controller) cancel(practiceOnly bool) {
	c.mu.Lock()
	r := c.cur
	if r == nil || (practiceOnly && r.sink == nil) {
		c.mu.Unlock()
		return
	}
	if c.view.Phase == Done || c.view.Phase == Failed {
		c.mu.Unlock()
		c.Dismiss()
		return
	}
	r.cancelled = true
	c.cur = nil
	c.view = View{}
	capture, pcm, prepared := r.capture, r.pcm, r.prepared
	// An insertion in flight settles the record itself, with the app it
	// actually reached.
	inserting := r.inserted && !r.recDone
	c.mu.Unlock()
	r.stop()
	c.stopCapture(r, capture)
	s := c.Settings.Get()
	switch {
	case prepared:
		c.note(r, func(rec *store.Record) {
			if rec.Status != store.StatusInserted {
				rec.Status, rec.StatusDetail = store.StatusCancelled, c.tr("detailCancelled")
			}
		})
		if !inserting {
			go c.persist(r, s, nil) // off the caller's thread: it may be the main one
		}
	case s.KeepCancelled && len(pcm) > 0:
		go c.keepCancelled(r, s, pcm)
	}
	r.release()
	c.changed()
}

// Dismiss closes the overlay of a finished or failed dictation.
func (c *Controller) Dismiss() {
	c.mu.Lock()
	r := c.cur
	if r == nil || (c.view.Phase != Done && c.view.Phase != Failed) {
		c.mu.Unlock()
		return
	}
	c.cur = nil
	c.view = View{}
	observing := r.observing
	c.mu.Unlock()
	r.stop()
	if !observing {
		r.release()
	}
	// A dictation that ends here without inserting (a failed cleanup left
	// as it is) is settled now; one already written is left alone.
	go c.persist(r, c.Settings.Get(), nil)
	c.changed()
}

// CopyFailed copies the text a failed dictation holds, says for a moment
// that it is on the clipboard, and closes. A practice run never touches
// the clipboard: it only closes.
func (c *Controller) CopyFailed() {
	c.mu.Lock()
	r, text := c.cur, c.view.Text
	held := r != nil && c.view.Phase == Failed && text != ""
	practice := r != nil && r.sink != nil
	c.mu.Unlock()
	if !held || practice || c.CopyText == nil {
		c.Dismiss()
		return
	}
	c.CopyText(text)
	c.done(r, c.tr("copiedFallback"))
	go c.persist(r, c.Settings.Get(), nil)
}

// cleanupOptions is the one cleanup configuration: the dictation's cleanup,
// its retry and the history's re-cleanup all take it from here, and
// speech recognition never does.
func (c *Controller) cleanupOptions(s config.Settings) (cleanup.Options, error) {
	if c.ResolveCleanup != nil {
		return c.ResolveCleanup(s)
	}
	key := s.CleanupAPIKey // a legacy plaintext key, until it is migrated
	if c.CleanupKey != nil {
		if k := c.CleanupKey(); k != "" {
			key = k
		}
	}
	return cleanup.Options{BaseURL: s.CleanupURL, Model: s.CleanupModel, APIKey: key}, nil
}

// dialer is the speech service the default model is on.
func (c *Controller) dialer(s config.Settings) (asr.Dialer, error) {
	if c.ResolveASR != nil {
		return c.ResolveASR(s)
	}
	return asr.LocalLanguage(s.ASRURL, config.LocalASRLanguage(s.ASRLanguage)), nil
}

func pcmMs(pcm []byte) int64 { return int64(len(pcm)) * 1000 / (2 * asr.SampleRate) }

// level maps the loudness of samples to 0..1, from -55 dBFS to -10 dBFS.
func level(pcm []byte) float32 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := range n {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
		sum += v * v
	}
	rms := math.Sqrt(sum/float64(n)) / 32768
	if rms <= 0 {
		return 0
	}
	return float32(min(1, max(0, (20*math.Log10(rms)+55)/45)))
}

// Voiced says the level shows as sound in the overlay.
func Voiced(l float32) bool { return l >= voicedLevel }

func (c *Controller) samples(r *run, pcm []byte) {
	l := level(pcm)
	c.mu.Lock()
	if !c.active(r) || r.confirmed {
		c.mu.Unlock()
		return
	}
	if c.view.Phase == Preparing {
		c.view.Phase = Recording // actual PCM, independent of ASR connection speed
	}
	r.pcm = append(r.pcm, pcm...)
	r.readyOnce.Do(func() { close(r.ready) })
	copy(c.view.Levels[:], c.view.Levels[1:])
	c.view.Levels[Levels-1] = l
	c.mu.Unlock()
	select {
	case r.more <- struct{}{}:
	default:
	}
	c.changed()
}

// fail ends a dictation with an error. Text it already has stays in the
// overlay, to copy, and in the history.
func (c *Controller) fail(r *run, err error, text string) {
	c.mu.Lock()
	if !c.active(r) {
		c.mu.Unlock()
		return
	}
	r.confirmed = true // freeze PCM before asynchronous hardware shutdown
	capture := r.capture
	c.view = View{Phase: Failed, Message: err.Error(), Text: text, Cleanup: r.cleanup}
	if text != "" {
		c.view.Retained, c.view.Notice = true, err.Error()
	}
	c.mu.Unlock()
	c.stopCapture(r, capture)
	c.changed()
}

// stopCapture keeps hardware shutdown and Bluetooth playback recovery off the
// text path. The caller freezes samples under c.mu before scheduling this.
// Capture.Stop retains its lifecycle lock throughout recovery, so a subsequent
// recording cannot race the old output guard or mute restoration.
func (c *Controller) stopCapture(r *run, capture *macos.Capture) {
	if capture == nil {
		return // record owns shutdown if capture has not been published yet
	}
	r.captureStopOnce.Do(func() {
		go func() {
			capture.Stop()
			c.endCue(r)
		}()
	})
}

// endCue is independent of coalesced UI updates and sounds only once per run.
// Call only after microphone capture has stopped.
func (c *Controller) endCue(r *run) {
	macos.OnMain(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		play := c.cur == r && r.capture != nil && !r.cueEnded && c.Settings.Get().InteractionSounds
		r.cueEnded = true
		if play {
			macos.PlayRecordingCue(false)
		}
	})
}

// record is the goroutine of a dictation, up to the final transcript.
func (c *Controller) record(r *run, s config.Settings) {
	r.target = macos.CaptureTarget()
	c.markPrivate(r, s, r.target.BundleID)
	r.rec = store.Record{ID: r.id, StartedAt: r.started, TargetApp: r.target.AppName, TargetBundleID: r.target.BundleID, ASRModel: providers.ModelName(providers.KindASR, s), TranslationLanguage: r.translation, CleanupRequested: r.cleanup, Status: store.StatusFailed}

	switch macos.MicrophoneStatus() {
	case "denied", "restricted":
		c.fail(r, errors.New(c.tr("micDenied")), "")
		return
	case "undetermined":
		granted := make(chan bool, 1)
		macos.RequestMicrophone(func(ok bool) { granted <- ok })
		if !<-granted {
			c.fail(r, errors.New(c.tr("micNotGranted")), "")
			return
		}
	}

	select {
	case <-r.ctx.Done():
		return
	case <-r.confirm:
		c.fail(r, errors.New(c.tr("endedEarly")), "")
		return
	default:
	}

	// The microphone starts before the ASR is connected, and the samples
	// wait: what is said while it connects is not lost.
	capture, err := macos.StartCaptureFromWithOutputMute(s.InputDevice, func(pcm []byte) { c.samples(r, pcm) }, s.MuteOutput)
	if err != nil {
		if errors.Is(err, macos.ErrNoMicrophone) {
			err = errors.New(c.tr("micMissing"))
		}
		c.fail(r, err, "")
		return
	}
	c.mu.Lock()
	if !c.active(r) || r.confirmed {
		// Cancelled, or ended, before the microphone was open.
		c.mu.Unlock()
		capture.Stop()
		if r.confirmed {
			c.fail(r, errors.New(c.tr("endedEarly")), "")
		}
		return
	}
	r.capture = capture
	c.mu.Unlock()
	// AudioQueueStart only schedules the device. Wait for captured PCM,
	// not the API's return value, before announcing that speech is safe.
	readyTimeout := time.NewTimer(5 * time.Second)
	defer readyTimeout.Stop()
	select {
	case <-r.ready:
	case <-r.ctx.Done():
		return
	case <-r.confirm:
		c.fail(r, errors.New(c.tr("endedEarly")), "")
		return
	case <-readyTimeout.C:
		// No samples came: the device went away, or it is there and silent.
		if macos.InputDeviceMissing() {
			c.fail(r, errors.New(c.tr("micMissing")), "")
		} else {
			c.fail(r, errors.New(c.tr("micNoFrames")), "")
		}
		return
	}

	macos.OnMain(func() {
		c.mu.Lock()
		play := c.active(r) && !r.confirmed && s.InteractionSounds
		c.mu.Unlock()
		if play {
			macos.PlayRecordingCue(true)
		}
	})
	// Let the complete start cue sound before muting. Stop restores output
	// before endCue in the background, including an early stop during this wait.
	if err := macos.WaitRecordingStartCue(); err != nil {
		c.fail(r, err, "")
		return
	}
	if s.MuteOutput {
		if err := capture.SilenceOutput(); err != nil {
			c.fail(r, err, "")
			return
		}
	}

	var hotwords *string
	{
		p, n, err := c.Dict.HotwordParam()
		switch {
		case err != nil:
			r.rec.HotwordNote = c.tr("hotwordNotSent", err.Error())
		case n == 0:
			r.rec.HotwordNote = c.tr("hotwordEmpty")
		default:
			hotwords = &p
			r.rec.HotwordNote = c.tr("hotwordSent", n)
		}
	}
	if c.PrepareASR != nil {
		release, err := c.PrepareASR(r.ctx, s)
		if err != nil {
			c.finish(r, s, "", errors.New(c.explain(err)))
			return
		}
		defer release()
	}
	dialCtx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	service, err := c.dialer(s)
	if err != nil {
		cancel()
		c.finish(r, s, "", errors.New(c.explain(err)))
		return
	}
	if !service.Hotwords() && hotwords != nil {
		// Only adapters without term-hint support omit the dictionary.
		hotwords = nil
		r.rec.HotwordNote = c.tr("hotwordCloud")
	}
	if hotwords != nil && s.ASRAssign.Provider == "volc" {
		r.rec.HotwordNote += " " + c.tr("hotwordVolc")
	}
	switch err = service.Check(dialCtx); {
	case errors.Is(err, asr.ErrBusy):
		err = errors.New(c.tr("asrBusy"))
	case errors.Is(err, asr.ErrLanguage):
		err = errors.New(c.tr("asrLanguageOld"))
	case err != nil:
		err = errors.New(c.tr("asrUnreachable", service.Label(), cause(err)))
	}
	var sess asr.Stream
	if err == nil {
		if sess, err = service.Dial(dialCtx, hotwords); err != nil {
			err = errors.New(c.explain(err)) // in the provider's or the service's own terms
		}
	}
	cancel()
	if err != nil {
		c.finish(r, s, "", err)
		return
	}
	defer sess.Close()
	sess.SetOnUpdate(func() {
		c.mu.Lock()
		if c.active(r) {
			if r.firstMs == 0 {
				r.firstMs = time.Since(r.started).Milliseconds()
			}
			c.view.Final, c.view.Pending = sess.Final(), sess.Pending()
		}
		c.mu.Unlock()
		c.changed()
	})

	// Send what was recorded, as it comes, until the user confirms; then
	// the rest, and wait for the service to finalize.
	go c.watchMic(r, capture)
	lost, lostCh := false, r.lost
	sent := 0
	flush := func() error {
		c.mu.Lock()
		chunk := r.pcm[sent:]
		c.mu.Unlock()
		sent += len(chunk)
		return sess.Send(r.ctx, chunk)
	}
	for confirmed := false; !confirmed; {
		select {
		case <-r.ctx.Done():
			return // cancelled: closing the session drops what is left
		case <-r.confirm:
			confirmed = true
		case <-lostCh:
			// The microphone is gone. End the recording as a confirmation
			// would, so what was already recorded is still sent and
			// finalized; if the user ended it first, that stands.
			lostCh = nil
			if !c.micLost(r) {
				continue
			}
			lost, confirmed = true, true
		case <-r.more:
		}
		if sess.Err() != nil {
			break
		}
		if err = flush(); err != nil {
			break
		}
	}
	if err == nil {
		err = sess.Err()
	}
	if err == nil {
		finCtx, cancel := context.WithTimeout(r.ctx, time.Duration(max(s.FinalizeTimeoutSec, 5))*time.Second)
		err = sess.Finish(finCtx)
		cancel()
	}
	text := sess.Final()
	switch {
	case lost:
		// Nothing is inserted from a recording that broke off. The words
		// heard so far are kept, to copy and in the history; when the
		// service could not finalize, also those that were not final yet.
		if err != nil {
			text += sess.Pending()
		}
		err = errors.New(c.tr("micMissing"))
	case err != nil:
		err = errors.New(c.explain(err))
	}
	c.finish(r, s, text, err)
}

// watchMic looks, a few times a second while the dictation records,
// whether the device it started on is still there, and closes r.lost once
// when it is not. It ends with the recording.
func (c *Controller) watchMic(r *run, capture *macos.Capture) {
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.confirm:
			return
		case <-tick.C:
			if capture.DeviceGone() {
				close(r.lost)
				return
			}
		}
	}
}

// micLost ends the recording because its microphone is gone: the capture
// is stopped, which also restores the muted output, and the dictation goes
// on to finalize what it has. It reports false when the recording was
// already ended or abandoned.
func (c *Controller) micLost(r *run) bool {
	c.mu.Lock()
	if !c.active(r) || r.confirmed {
		c.mu.Unlock()
		return false
	}
	r.confirmed = true
	r.stopAt = time.Now()
	c.view.Phase = Finalizing
	capture := r.capture
	c.mu.Unlock()
	c.stopCapture(r, capture)
	c.changed()
	return true
}

// finish takes the final transcript, or what there is of it with an error,
// saves the record and goes on to the cleanup and the insertion.
func (c *Controller) finish(r *run, s config.Settings, raw string, err error) {
	c.mu.Lock()
	if !c.active(r) {
		c.mu.Unlock()
		r.release()
		return
	}
	r.confirmed = true // also freeze PCM on a service error before confirmation
	capture, pcm := r.capture, r.pcm
	r.finalAt = time.Now()
	c.mu.Unlock()
	c.stopCapture(r, capture)

	raw = strings.TrimSpace(raw)
	text, applied := c.formatTranscript(raw, s)
	c.mu.Lock()
	if !c.active(r) {
		c.mu.Unlock()
		r.release()
		return
	}
	r.text = text
	r.rec.DurationMs = pcmMs(pcm)
	r.rec.RawText, r.rec.AppliedRules = raw, applied
	if text != raw {
		r.rec.Corrected, r.rec.CorrectedText = true, text
	}
	r.rec.Timing.FirstPartialMs = r.firstMs
	if !r.stopAt.IsZero() {
		r.rec.Timing.StopToFinalMs = r.finalAt.Sub(r.stopAt).Milliseconds()
	}
	switch {
	case err != nil:
		r.rec.Status, r.rec.StatusDetail = store.StatusFailed, err.Error()
	case text == "":
		r.rec.Status, r.rec.StatusDetail = store.StatusNotInserted, c.tr("detailNoSpeech")
	default:
		r.rec.Status, r.rec.StatusDetail = store.StatusNotInserted, c.tr("detailProgress")
	}
	// The record and the recording stay in memory until the apps the run
	// touched are known: nothing of a private run reaches the disk.
	r.prepared = true
	c.mu.Unlock()

	switch {
	case err != nil:
		c.fail(r, err, text)
		c.persist(r, s, nil)
	case text == "":
		c.done(r, c.tr("noSpeech"))
		c.persist(r, s, nil)
	case r.cleanup:
		c.clean(r, s)
	default:
		c.insert(r, s, text)
	}
}

func (c *Controller) update(r *run, fn func(rec *store.Record)) {
	c.History.Update(r.id, fn)
	c.historyChanged()
}

// clean tidies the transcript, then inserts the result. A failure keeps the
// transcript and lets the user insert it as it is, or try again.
func (c *Controller) clean(r *run, s config.Settings) {
	c.mu.Lock()
	if !c.active(r) {
		c.mu.Unlock()
		return
	}
	c.view.Phase, c.view.Message, c.view.CleanupFailed = Cleaning, "", false
	c.mu.Unlock()
	c.changed()

	began := time.Now()
	ctx, cancel := context.WithTimeout(r.ctx, time.Duration(max(s.CleanupTimeoutSec, 5))*time.Second)
	opts, err := c.cleanupOptions(s)
	cleaned := ""
	if err == nil {
		if r.translation != "" {
			cleaned, err = cleanup.Translate(ctx, opts, r.text, config.TranslationTarget(r.translation))
		} else {
			cleaned, err = cleanup.Run(ctx, opts, r.text)
		}
	}
	cancel()
	took := time.Since(began).Milliseconds()
	c.mu.Lock()
	ok := c.active(r)
	c.mu.Unlock()
	if !ok {
		return // cancelled meanwhile: the late result is dropped
	}
	if err != nil {
		c.note(r, func(rec *store.Record) {
			rec.CleanupError, rec.Timing.CleanupMs = c.cleanupFailure(err), took
			rec.CleanupModel = opts.Model
			rec.StatusDetail = c.cleanupFailure(err)
		})
		c.mu.Lock()
		if c.active(r) {
			c.view = View{Phase: Failed, Cleanup: true, CleanupFailed: true, TranslationLanguage: r.translation, Text: r.text, Message: c.cleanupFailure(err)}
		}
		c.mu.Unlock()
		c.changed()
		return
	}
	if r.translation == "" {
		cleaned, _ = textstyle.Apply(cleaned, s.TextStyle, c.Dict.Hotwords())
	}
	c.note(r, func(rec *store.Record) {
		rec.CleanedText, rec.CleanupError, rec.Timing.CleanupMs = cleaned, "", took
		rec.CleanupModel = opts.Model
	})
	c.insert(r, s, cleaned)
}

// RetryCleanup runs the cleanup again after it failed, and InsertOriginal
// inserts the transcript without it.
func (c *Controller) RetryCleanup() {
	if r := c.failedCleanup(); r != nil {
		go c.clean(r, c.Settings.Get())
	}
}

func (c *Controller) InsertOriginal() {
	if r := c.failedCleanup(); r != nil {
		go c.insert(r, c.Settings.Get(), r.text)
	}
}

func (c *Controller) failedCleanup() *run {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil || c.view.Phase != Failed || !c.view.CleanupFailed {
		return nil
	}
	// Leave the failed state at once, so that a second click does nothing.
	c.view.Phase, c.view.CleanupFailed = Inserting, false
	return c.cur
}

// insert writes text to the target, once per dictation whatever happens.
func (c *Controller) insert(r *run, s config.Settings, text string) {
	if strings.TrimSpace(text) == "" {
		c.note(r, func(rec *store.Record) {
			rec.Status, rec.StatusDetail = store.StatusNotInserted, c.tr("detailNoSpeech")
		})
		c.done(r, c.tr("noSpeech"))
		c.persist(r, s, nil)
		return
	}
	c.mu.Lock()
	if !c.active(r) || r.inserted {
		c.mu.Unlock()
		return
	}
	r.inserted = true
	c.view.Phase, c.view.Message = Inserting, ""
	c.mu.Unlock()
	c.changed()

	if r.sink != nil {
		// A practice run: the transcript goes to the guide's own field and
		// nowhere else. Nothing is inserted into any app, the clipboard is
		// not read or written, and no edit is observed afterwards.
		c.note(r, func(rec *store.Record) {
			rec.Status, rec.StatusDetail = store.StatusNotInserted, c.tr("detailPractice")
			rec.Timing.FinalToInsertMs = time.Since(r.finalAt).Milliseconds()
		})
		c.mu.Lock()
		live := c.active(r)
		c.mu.Unlock()
		if live && strings.TrimSpace(text) != "" {
			r.sink(text)
		}
		c.done(r, "")
		c.persist(r, s, nil)
		return
	}
	var observing bool
	onPasted := func(target *macos.Target) {
		// The app pasted into decides as much as the one at the start: a
		// private run is never observed, so nothing of it is learned.
		if c.markPrivate(r, s, target.BundleID) || !s.ObserveEdits || r.translation != "" {
			return
		}
		observing = true
		c.mu.Lock()
		r.observing = true
		c.mu.Unlock()
		go c.observe(r, s, text, target)
	}
	var editMu sync.Mutex
	editedText := text
	res, err := macos.Insert(text, macos.InsertOptions{Context: r.ctx, RestoreDelayMs: s.ClipboardRestoreDelayMs, RemoteInputs: s.RemoteInputs, AutoSend: s.AutoSend, OnTarget: func(target *macos.Target) {
		c.markPrivate(r, s, target.BundleID)
	}, OnPasted: onPasted, BeforeSend: func(target *macos.Target) bool {
		c.markPrivate(r, s, target.BundleID)
		if c.SendPreview == nil || s.AutoSend.Delay() == 0 {
			return true
		}
		return c.SendPreview(r.ctx, text, target, time.Duration(s.AutoSend.Delay())*time.Millisecond, func(edited string) {
			editMu.Lock()
			editedText = edited
			c.note(r, func(rec *store.Record) { rec.InsertedText = edited })
			editMu.Unlock()
		})
	}})
	if errors.Is(err, context.Canceled) {
		// Cancelled while inserting: cancel left the record to be settled
		// here. Where the text went is not known; the focused app decides.
		c.persist(r, s, nil)
		r.release()
		return
	}
	if err != nil {
		msg := c.tr("notInserted", err.Error())
		c.note(r, func(rec *store.Record) { rec.Status, rec.StatusDetail = store.StatusNotInserted, err.Error() })
		c.persist(r, s, nil)
		c.mu.Lock()
		if c.active(r) {
			c.view = View{Phase: Failed, Message: msg, Text: text, Cleanup: r.cleanup, Retained: true}
		}
		c.mu.Unlock()
		c.changed()
		r.release()
		return
	}
	if res.ClipboardOnly {
		// The text waits on the clipboard for the app that is focused.
		c.note(r, func(rec *store.Record) {
			rec.Status, rec.StatusDetail = store.StatusNotInserted, c.tr("copiedFallback")
		})
		c.persist(r, s, res.Target)
		res.Target.Release()
		c.done(r, c.tr("copiedFallback"))
		return
	}
	detail := c.tr("detailPasted", res.Target.AppName)
	if res.SendShortcut != "" {
		detail += "; " + c.tr("detailSendTriggered", res.SendShortcut)
	}
	if res.SendSkipped != "" {
		detail += "; " + c.tr("detailSendSkipped")
	}
	if res.Remote {
		detail = c.tr("detailRemote", res.Target.AppName)
	}
	if !res.FocusVerified && !res.Remote {
		detail += "; " + c.tr("detailNoElement")
	}
	if !res.ClipboardRestored && !res.Remote {
		detail += "; " + c.tr("detailClipboard")
	}
	editMu.Lock()
	c.note(r, func(rec *store.Record) {
		rec.SendShortcut, rec.SendSkipped = res.SendShortcut, res.SendSkipped
		rec.TargetApp = res.Target.AppName
		rec.TargetBundleID = res.Target.BundleID
		rec.Status, rec.StatusDetail, rec.InsertedText = store.StatusInserted, detail, editedText
		rec.Timing.FinalToInsertMs = time.Since(r.finalAt).Milliseconds()
	})
	editMu.Unlock()
	c.persist(r, s, res.Target)
	if !observing {
		res.Target.Release()
	}
	c.done(r, "")
}

// done shows the result briefly, then closes the overlay.
func (c *Controller) done(r *run, message string) {
	c.mu.Lock()
	if !c.active(r) {
		c.mu.Unlock()
		return
	}
	c.view = View{Phase: Done, Message: message}
	c.mu.Unlock()
	c.changed()
	if r.holdCopied && message == c.tr("copiedFallback") {
		return // until the cross, Escape or the next dictation
	}
	wait := 900 * time.Millisecond
	if message != "" {
		wait = 2200 * time.Millisecond
	}
	time.AfterFunc(wait, func() {
		c.mu.Lock()
		same := c.cur == r
		c.mu.Unlock()
		if same {
			c.Dismiss()
		}
	})
}

// observe retains the last correction across send/clear and continued typing.
func (c *Controller) observe(r *run, s config.Settings, inserted string, target *macos.Target) {
	defer r.release()
	defer target.Release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(max(s.ObserveSeconds, 5))*time.Second)
	defer cancel()
	revised, status := dict.Observe(ctx, inserted, target.ReadValue)
	if c.markPrivate(r, s, target.BundleID) {
		return
	}
	msg := c.tr(status)
	if revised != "" && strings.TrimSpace(revised) != strings.TrimSpace(inserted) {
		_, learned := c.Dict.Propose(inserted, revised, "observed-edit", r.id)
		msg = c.tr("observeLearned", learned)
	}
	c.note(r, func(rec *store.Record) { rec.ObserveNote = msg })
	c.historyChanged()
}

// Revise keeps the user's correction of a record's text as a new version
// and proposes dictionary candidates from it. It returns how many
// candidates were added.
func (c *Controller) Revise(id, base, revised string) (int, error) {
	revised = strings.TrimSpace(revised)
	if revised == "" || revised == base {
		return 0, errors.New(c.tr("revisionSame"))
	}
	n, note := c.Dict.Propose(base, revised, "history-revision", id)
	ok, err := c.History.Update(id, func(rec *store.Record) {
		rec.Versions = append(rec.Versions, store.Version{ID: store.NewID(), Kind: store.KindRevision, Text: revised, BasedOn: base, Note: note, At: time.Now()})
	})
	if err == nil && !ok {
		err = errors.New(c.tr("recordGone"))
	}
	c.historyChanged()
	return n, err
}

// SaveRevision is Revise, returning the dictionary's note about what it
// proposed instead of the count.
func (c *Controller) SaveRevision(id, base, revised string) (string, error) {
	n, err := c.Revise(id, base, revised)
	return strconv.Itoa(n) + " candidate(s) proposed", err
}

// Retranscribe runs a record's saved audio through the ASR again and adds
// the result as a new version; the earlier transcript stays.
func (c *Controller) Retranscribe(id string) error {
	rec, ok := c.History.Get(id)
	if !ok || !rec.HasAudio() {
		return errors.New(c.tr("noAudio"))
	}
	c.mu.Lock()
	if c.cur != nil || c.retranscribing {
		c.mu.Unlock()
		return errors.New(c.tr("busy"))
	}
	c.retranscribing = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.retranscribing = false
		c.mu.Unlock()
	}()
	pcm, err := c.History.ReadPCM(rec.AudioFile)
	if err != nil {
		return err
	}
	s := c.Settings.Get()
	var hotwords *string
	note := c.tr("hotwordDefault")
	if p, n, err := c.Dict.HotwordParam(); err == nil && n > 0 {
		hotwords, note = &p, c.tr("hotwordLocal", n)
	}
	if c.PrepareASR != nil {
		release, err := c.PrepareASR(context.Background(), s)
		if err != nil {
			return errors.New(c.explain(err))
		}
		defer release()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(max(s.FinalizeTimeoutSec, 5))*time.Second+time.Duration(pcmMs(pcm))*time.Millisecond)
	defer cancel()
	service, err := c.dialer(s)
	if err != nil {
		return errors.New(c.explain(err))
	}
	if !service.Hotwords() {
		hotwords, note = nil, c.tr("hotwordCloud")
	}
	if hotwords != nil && s.ASRAssign.Provider == "volc" {
		note += "; " + c.tr("hotwordVolc")
	}
	if providers.Assigned(providers.KindASR, s).Provider == providers.Local {
		note += "; " + c.tr("noteLanguage", c.tr("asrLang_"+config.LocalASRLanguage(s.ASRLanguage)))
	}
	raw, err := asr.Transcribe(ctx, service, hotwords, pcm)
	if errors.Is(err, asr.ErrLanguage) {
		return errors.New(c.tr("asrLanguageOld"))
	}
	if err != nil && raw == "" {
		return errors.New(c.tr("asrUnreachable", service.Label(), c.explain(err)))
	}
	text, applied := c.formatTranscript(strings.TrimSpace(raw), s)
	if len(applied) > 0 {
		note += "; " + c.tr("noteRules", strings.Join(applied, ", "))
	}
	if err != nil {
		note += "; " + c.tr("noteIncomplete", err.Error())
	}
	c.History.Update(id, func(rec *store.Record) {
		rec.Versions = append(rec.Versions, store.Version{ID: store.NewID(), Kind: store.KindRetranscribe, Text: text, BasedOn: raw, Note: note, At: time.Now()})
	})
	c.historyChanged()
	return err
}

// Recleanup tidies a text of a record again and adds the result as a new
// version.
func (c *Controller) Recleanup(id, base string) error {
	s := c.Settings.Get()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(max(s.CleanupTimeoutSec, 5))*time.Second)
	defer cancel()
	opts, err := c.cleanupOptions(s)
	cleaned := ""
	if err == nil {
		cleaned, err = cleanup.Run(ctx, opts, base)
	}
	if err != nil {
		return errors.New(c.cleanupFailure(err))
	}
	cleaned, _ = textstyle.Apply(cleaned, s.TextStyle, c.Dict.Hotwords())
	c.History.Update(id, func(rec *store.Record) {
		rec.Versions = append(rec.Versions, store.Version{ID: store.NewID(), Kind: store.KindRecleanup, Text: cleaned, BasedOn: base, Model: opts.Model, At: time.Now()})
	})
	c.historyChanged()
	return nil
}

// formatTranscript is shared by live dictation and history retranscription.
func (c *Controller) formatTranscript(raw string, s config.Settings) (string, []string) {
	text, rules := c.Dict.Apply(raw)
	text, styleRules := textstyle.Apply(text, s.TextStyle, c.Dict.Hotwords())
	return text, append(rules, styleRules...)
}
