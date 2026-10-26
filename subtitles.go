package main

import (
	"context"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/subtitles"
)

// The live subtitle window of docs/public/LIVE-SUBTITLES.md, after
// mock/live-subtitles: a dark rounded panel above other windows, dragged by
// its header only and resized from its bottom-right corner, with every
// bilingual entry in a scroll region that follows the newest one until the
// reader scrolls up. Live entries come from internal/subtitles; the
// synthetic fixture is a separate, labelled feed for window acceptance.

// subtitlesFixtureEnv opens the window with a synthetic English feed, in Dev
// builds only. It is for native acceptance of the window, never a source of
// live subtitles.
const subtitlesFixtureEnv = "VOICE_INPUT_SUBTITLES_FIXTURE"

const (
	subMinW, subMinH = 320, 96
	subDefaultW      = 720
	subHeadH         = 40
	subLineHeight    = 1.45
	subPadTop        = 2
	subCardW         = 272
	subCardH         = 4*44 + 36 + 4 + 12
)

var (
	subInk      = ui.RGB(250, 250, 250)
	subInk2     = ui.RGB(212, 212, 212)
	subInk3     = ui.RGB(163, 163, 163)
	subLive     = ui.RGB(105, 231, 195)
	subDanger   = ui.Hex("#f87171")
	subAccentFg = ui.Hex("#a5a8ff")
	subHair     = ui.RGBA(255, 255, 255, 0.08)
	subHover    = ui.RGBA(255, 255, 255, 0.08)
	subTag      = ui.RGBA(255, 255, 255, 0.07)
	subCardBg   = ui.RGBA(30, 30, 34, 0.97)
	subRowLine  = ui.RGBA(255, 255, 255, 0.06)
	subLatestBg = ui.RGBA(40, 40, 46, 0.94)
	subDot      = ui.RGBA(255, 255, 255, 0.28)
)

var (
	icSubtitles = icon(`<rect x="1.5" y="3" width="13" height="10" rx="2"/><path d="M4.5 9.5h3M9.5 9.5h2M4.5 7h7"/>`)
	icSubGrip   = icon(`<circle cx="6" cy="4" r=".9" fill="currentColor" stroke="none"/><circle cx="10" cy="4" r=".9" fill="currentColor" stroke="none"/><circle cx="6" cy="8" r=".9" fill="currentColor" stroke="none"/><circle cx="10" cy="8" r=".9" fill="currentColor" stroke="none"/><circle cx="6" cy="12" r=".9" fill="currentColor" stroke="none"/><circle cx="10" cy="12" r=".9" fill="currentColor" stroke="none"/>`)
	icSubPause  = icon(`<path d="M5.5 3.5v9M10.5 3.5v9"/>`)
	icSubPlay   = icon(`<path d="M5 3.2v9.6L12.5 8z" fill="currentColor"/>`)
	icSubNew    = icon(`<path d="M13 8a5 5 0 1 1-1.6-3.7M13 2.5v3h-3"/>`)
	icSubClose  = icon(`<path d="M4 4l8 8M12 4l-8 8"/>`)
	icSubDown   = icon(`<path d="M8 3v10M4 9l4 4 4-4"/>`)
	icSubResize = icon(`<path d="M13 6 6 13M13 10l-3 3"/>`)
	icSubMinus  = icon(`<path d="M3.5 8h9"/>`)
	icSubPlus   = icon(`<path d="M3.5 8h9M8 3.5v9"/>`)
	icSubNext   = icon(`<path d="M6 3.5 10.5 8 6 12.5"/>`)
)

// subtitleTargets are the translation targets, in their own names.
var subtitleTargets = []struct{ id, tag, label string }{
	{"zh-Hans", "ZH", "简体中文"},
	{"zh-Hant", "ZH-HANT", "繁體中文"},
	{"en", "EN", "English"},
	{"ja", "JA", "日本語"},
	{"ko", "KO", "한국어"},
	{"fr", "FR", "Français"},
	{"de", "DE", "Deutsch"},
	{"es", "ES", "Español"},
}

func subtitleTargetTag(id string) string {
	for _, t := range subtitleTargets {
		if t.id == id {
			return t.tag
		}
	}
	return strings.ToUpper(id)
}

type trState int

// trState is the state of an entry's final translation; drafts before it
// show in translation without changing it.
const (
	trNone    trState = iota
	trPending         // the final text is being translated; a draft or the original shows meanwhile
	trDone
	trFailed // the original (and any draft) stays, with a retry
)

// subtitleEntry is one segment of a session: its original, revised as the
// engine streams, and one translation that drafts update and the final
// corrects in place. session and segment key it; rev orders its revisions.
type subtitleEntry struct {
	session     uint64
	segment     int
	rev         int
	original    string
	translation string // the latest applied draft or final translation
	final       bool
	same        bool   // already in the target language: shown once
	target      string // the target that translation and same are for
	tr          trState

	shownSrc   string // the source text of translation
	shownRev   int    // the revision translation is for
	shownFinal bool   // translation is of the final text
	want       *subtitleJob
	busy       *subtitleJob
	busyCancel func()

	// The draft policy (subtitles_translate.go).
	prev     string          // the previous revision's text
	stable   string          // the prefix the last two revisions agree on
	firstAt  time.Time       // the first revision
	draftSrc string          // the source of the last draft job
	draftAt  time.Time       // when it was made
	ev       subtitles.Event   // the current revision, for diagnostics
	hyps     []subtitles.Event // the recent revisions, to time a draft by the one whose text it is
	finalAt  time.Time         // when the final came

	// Background correction (subtitles_correct.go): Qwen's text of the
	// final's audio replaces original; its translation runs in its own lane.
	corrected   bool
	asrText     string // sherpa's final text, kept once corrected
	cwant       *subtitleJob
	cbusy       *subtitleJob
	cbusyCancel func()

	// Speakers (subtitles_speakers.go): the final's audio on the decoded
	// clock (-1 unknown) and tokens, and its utterances once labelled.
	start, end int64
	tokens     []subtitles.Token
	utts       []subUtt
	spkDone    bool
}

type subtitleWindow struct {
	a       *app
	win     *mygo.Window
	card    *mygo.Window // the display flyout, outside the caption window
	entries []subtitleEntry
	log     *subtitleLog

	scroll   ui.ScrollState
	follow   bool
	paused   bool
	cardOpen bool

	// The live session: events of any other session id are stale.
	sessID     uint64
	sessCancel context.CancelFunc
	running    bool
	status     string // subtitles.Event.Code of the running session
	errCode    string // why the last session ended, if it failed
	textErr    bool   // the text service for translation is not set up
	liveKey    string // engine and language the session runs with

	// The translator: results of another generation are stale.
	trGen    uint64
	trCtx    context.Context // nil while no translator runs
	trCancel context.CancelFunc
	inflight int // requests running, cancelled ones included until they return
	active   int // requests running that are not cancelled: what the slots count
	jobSeq   uint64
	// The correction lane, apart from the live slots.
	corrInflight int
	corrActive   int
	dictating    bool // the app's dictation is busy: optional work yields

	// The reading anchor: the first visible entry and how far into it the
	// view starts, kept across reflows, as the mock's settle().
	frame      uint64
	rows       []ui.Rect
	content    ui.Rect
	anchorRow  int
	anchorFrac float32
	anchored   bool
	layoutSig     string
	reanchorStart uint64  // the frame the reflow began
	reanchorAt    uint64  // the last frame that restores the anchor; 0 when none
	contentH      float32 // the content height last frame, to see a reflow settle

	displayRect ui.Rect
	resizing    bool
	rsStart     mygo.Point
	rsBounds    mygo.Rectangle
	expectW     int
	expectH     int
	userSized   bool
	saveTimer   *time.Timer

	fixture    bool
	fixtureGen uint64
	fxLine     int
	fxWord     int

	// Speakers and their menu (subtitles_speakers.go).
	spk        speakerState
	who        *mygo.Window
	whoOpen    bool
	whoSession uint64
	whoSegment int
	whoKey     int
	whoSpeaker string
	whoScope   string // "one" or "all"
	whoName    string
	whoGen     uint64
	chipRects  map[string]ui.Rect

	// The explicit save (subtitles_store.go): changes counts content
	// changes, savedChanges the count the last save wrote.
	confirm      *mygo.Window
	confirmOpen  bool
	newRect      ui.Rect
	changes      uint64
	savedChanges uint64
	saving       bool
	saveFailed   bool
	storeID      string
	storeGen     uint64 // New session moves it on: older saves' completions are stale
	restored     bool   // opened from the saved slot: the first show stays paused
}

// subtitlesAcceptance is a development run of the subtitle window, which
// binds no global dictation trigger (bindTrigger).
func subtitlesAcceptance() bool {
	return devBuild() && (os.Getenv(subtitlesFixtureEnv) == "1" || subtitlesLiveOnLaunch())
}

func subtitlesFixtureEnabled() bool {
	return devBuild() && os.Getenv(subtitlesFixtureEnv) == "1" && !subtitlesLiveOnLaunch()
}

// openSubtitleFixture opens the window on launch for development: on the
// synthetic feed, or capturing real system audio.
func (a *app) openSubtitleFixture() {
	if subtitlesFixtureEnabled() || subtitlesLiveOnLaunch() {
		a.openSubtitles()
	}
}

// openSubtitles shows the subtitle window, making it the first time.
func (a *app) openSubtitles() {
	if a.subtitles == nil {
		a.subtitles = newSubtitleWindow(a)
	}
	a.subtitles.show()
}

// changeSubtitles changes the subtitles' own settings, and nothing else.
func (a *app) changeSubtitles(fn func(s *config.Subtitles)) {
	s := a.settings.Get()
	old := s.Subtitles.Normalized()
	sub := old
	fn(&sub)
	s.Subtitles = sub
	if err := a.settings.Set(s); err != nil {
		log.Printf("subtitles: settings not saved: %v", err)
	}
	if a.subtitles != nil {
		a.subtitles.settingsChanged(old, sub)
		a.subtitles.refresh()
	}
	a.prefs.refresh()
}

// subtitleDefaultHeight fits two entries and one wrapped translation line
// at the current font sizes, as the mock's defaultSize.
func subtitleDefaultHeight(s config.Subtitles, showOriginal bool) int {
	t := float32(s.TransSize) * subLineHeight
	o := float32(0)
	if showOriginal {
		o = float32(s.OrigSize)*subLineHeight + 3
	}
	return int(subHeadH + 20 + 2*(o+t) + 12 + t)
}

// subtitleBounds is where the window opens: the saved place while it is on a
// display, else the bottom centre of the display in use. Either fits the
// display's work area.
func subtitleBounds(s config.Subtitles, width, height int) mygo.Rectangle {
	b := mygo.Rectangle{X: s.X, Y: s.Y, Width: width, Height: height}
	wa := mygo.Screen.DisplayMatching(b).WorkArea
	if !s.Placed || !intersects(b, wa) {
		wa = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
		b.X = wa.X + (wa.Width-width)/2
		b.Y = wa.Y + wa.Height - height - max(64, wa.Height/10)
	}
	b.Width = min(b.Width, max(subMinW, wa.Width-24))
	b.Height = min(b.Height, max(subMinH, wa.Height-24))
	b.X = min(max(b.X, wa.X), wa.X+wa.Width-b.Width)
	b.Y = min(max(b.Y, wa.Y), wa.Y+wa.Height-b.Height)
	return b
}

func intersects(a, b mygo.Rectangle) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func newSubtitleWindow(a *app) *subtitleWindow {
	w := &subtitleWindow{a: a, follow: true, fixture: subtitlesFixtureEnabled(), log: openSubtitleLog(),
		spk: newSpeakerState(), chipRects: map[string]ui.Rect{}}
	w.restore()
	if w.storeID == "" {
		w.storeID = newSessionID()
	}
	s := a.settings.Get().Subtitles.Normalized()
	width, height := s.Width, s.Height
	w.userSized = width > 0 && height > 0
	if !w.userSized {
		width, height = subDefaultW, subtitleDefaultHeight(s, !s.HideOriginal)
	}
	b := subtitleBounds(s, width, height)
	w.expectW, w.expectH = b.Width, b.Height
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:             T("catSubtitles"),
		Width:             b.Width,
		Height:            b.Height,
		UseContentSize:    true,
		X:                 b.X,
		Y:                 b.Y,
		MinWidth:          subMinW,
		MinHeight:         subMinH,
		Hidden:            true,
		Frameless:         true,
		Transparent:       true,
		BackgroundColor:   "#00000000",
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		DisableShadow:     true,
		AlwaysOnTop:       true,
		SkipTaskbar:       true,
		Content:           ui.View(w.view),
	})
	w.win.SetContentBounds(b) // X and Y of zero would centre it otherwise
	w.win.SetVisibleOnAllWorkspaces(true)
	w.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			w.close()
		}
	})
	w.win.OnResize(w.resized)
	w.win.OnMove(w.scheduleSave)
	go w.watchDictation()
	w.card = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent:    w.win,
		Placement: mygo.PlacementTopEnd,
		Gap:       6,
		Width:     subCardW,
		Height:    subCardH,
		Focusable: true,
		Hidden:    true,
		Shadow:    true,
		Content:   ui.View(w.cardView),
	})
	// Outside clicks and Escape close the card as the user closing it: it hides, to open again.
	w.card.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			w.hideCard()
		}
	})
	w.confirm = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent:    w.win,
		Placement: mygo.PlacementBottomEnd,
		Gap:       6,
		Width:     subConfirmW,
		Height:    subConfirmH,
		Focusable: true,
		Hidden:    true,
		Shadow:    true,
		Content:   ui.View(w.confirmView),
	})
	// Escape or an outside click is Cancel: the session stays.
	w.confirm.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			w.hideConfirm()
		}
	})
	return w
}

func (w *subtitleWindow) refresh() {
	if w != nil && w.win != nil {
		w.win.Invalidate()
		if w.cardOpen {
			w.card.Invalidate()
		}
	}
}

func (w *subtitleWindow) show() {
	w.win.Show()
	if w.restored {
		// A restored session opens paused; Resume starts capture.
		w.restored = false
		w.refresh()
		return
	}
	w.paused = false
	if w.fixture {
		w.startFeed()
	} else if !w.running {
		w.startLive()
	}
	w.refresh()
}

// close hides the window and stops capture, recognition and translation;
// the entries stay for the next open.
func (w *subtitleWindow) close() {
	w.hideCard()
	w.hideWho()
	w.hideConfirm()
	w.stopFeed()
	w.stopLive()
	w.saveBounds()
	w.win.Hide()
}

func (w *subtitleWindow) togglePause() {
	w.paused = !w.paused
	switch {
	case w.fixture && w.paused:
		w.stopFeed()
	case w.fixture:
		w.startFeed()
	case w.paused:
		w.stopLive()
	default:
		w.startLive()
	}
}

// requestNewSession starts a new session, asking first when the current
// one has unsaved content.
func (w *subtitleWindow) requestNewSession() {
	if !w.dirty() {
		w.newSession()
		return
	}
	w.hideCard()
	w.hideWho()
	r := w.newRect
	w.confirm.SetAnchor(mygo.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W), Height: int(r.H)})
	w.confirmOpen = true
	w.confirm.Show()
	w.confirm.Invalidate()
	w.win.Invalidate()
}

func (w *subtitleWindow) hideConfirm() {
	if !w.confirmOpen {
		return
	}
	w.confirmOpen = false
	w.confirm.Hide()
	w.win.Invalidate()
}

// confirmView is the new-session prompt: Cancel (focused), Don't save, Save.
func (w *subtitleWindow) confirmView(c *ui.Context) {
	subTheme(c)
	ui.Column(c.Key("confirm")).Fill().Padding(12).Gap(12).Radius(14).Background(subCardBg).Role(ui.RoleAlertDialog).Label(T("subNewSessionQ")).Children(func() {
		ui.Text(c, T("subNewSessionQ")).FontSize(13).FontWeight(500).TextColor(subInk).Padding(2, 2, 0)
		ui.Row(c.Key("actions")).FillWidth().Gap(6).Justify(ui.End).Children(func() {
			btn := func(key, label string, primary, focus bool) bool {
				b := ui.ButtonBase(c.Key(key)).Height(28).Padding(0, 11).Radius(7).AlignItems(ui.Center).Label(label).Background(ui.RGBA(255, 255, 255, 0.08))
				if focus {
					b.AutoFocus()
				}
				if primary {
					b.Background(ui.Hex("#6366c4"))
					if b.Hovered() {
						b.Background(ui.Hex("#8487de"))
					}
				} else if b.Hovered() {
					b.Background(ui.RGBA(255, 255, 255, 0.13))
				}
				b.Children(func() { ui.Text(c, label).FontSize(12.5).TextColor(subInk).SingleLine() })
				return b.Clicked()
			}
			if btn("cancel", T("subCancel"), false, true) {
				w.hideConfirm()
			}
			if btn("discard", T("subDontSave"), false, false) {
				w.hideConfirm()
				w.newSession()
			}
			if btn("save", T("subSave"), true, false) && !w.saving {
				w.hideConfirm()
				w.saveAndNew()
			}
		})
	})
}

// newSession clears the history and starts again, with no speakers:
// voices of the old session are forgotten, not carried over.
func (w *subtitleWindow) newSession() {
	w.stopLive()
	w.stopFeed()
	w.resetSpeakers(1)
	w.storeGen++ // a save of the old session finishing later changes nothing here
	w.storeID, w.restored, w.saveFailed = newSessionID(), false, false
	w.savedChanges = w.changes
	w.entries, w.rows, w.anchored, w.follow = nil, nil, false, true
	w.scroll.Y = 0
	w.fxLine, w.paused = 0, false
	if w.fixture {
		w.startFeed()
	} else {
		w.startLive()
	}
}

// settingsChanged restarts what a changed choice owns: the session for the
// engine or recognition language; for the target, the translator and the
// history's translations (retarget).
func (w *subtitleWindow) settingsChanged(old, s config.Subtitles) {
	if w.fixture {
		return
	}
	if (old.Engine != s.Engine || old.Language != s.Language) && w.running {
		w.startLive()
		return
	}
	if old.Target != s.Target {
		if w.trCtx != nil {
			w.startTranslator()
		}
		w.retarget(s.Target)
	}
}

/* ---------- live session ---------- */

func (w *subtitleWindow) startLive() {
	w.stopLive()
	s := w.a.settings.Get().Subtitles.Normalized()
	w.sessID++
	id := w.sessID
	ctx, cancel := context.WithCancel(context.Background())
	w.sessCancel, w.running, w.status, w.errCode = cancel, true, "loading", ""
	w.liveKey = s.Engine + "|" + s.Language
	cfg := w.a.subtitleConfig(s, w.log)
	speakers, why := false, "engine"
	if s.Engine == config.SubtitleSherpa {
		speakers, why = subtitleSpeakersEnabled(cfg.SpeakerRoot)
	}
	if speakers {
		cfg.Speakers = w.spk.tracker
		w.spk.on[id] = true
	} else if s.Engine == config.SubtitleSherpa {
		w.log.emit("speaker_unavailable", map[string]any{"session": id, "code": why, "root": cfg.SpeakerRoot})
	}
	w.log.emit("session_start", map[string]any{"session": id, "engine": s.Engine, "language": s.Language, "target": s.Target,
		"tap": cfg.TapPath, "qwen_root": cfg.QwenRoot, "sherpa_root": cfg.SherpaRoot, "correction": cfg.Correction && s.Engine == config.SubtitleSherpa,
		"speakers": speakers, "speaker_root": cfg.SpeakerRoot})
	go func() {
		err := subtitles.Run(ctx, id, cfg, func(ev subtitles.Event) {
			w.win.Update(func() { w.onEvent(ev) })
		})
		w.win.Update(func() { w.onSessionEnd(id, err) })
	}()
	w.startTranslator()
}

// stopLive cancels the session and its translations; an unfinished segment
// keeps its text, final, with a retry for its translation.
func (w *subtitleWindow) stopLive() {
	if w.sessCancel != nil {
		w.sessCancel()
		w.sessCancel = nil
		w.log.emit("session_stop", map[string]any{"session": w.sessID})
	}
	w.running = false
	w.stopTranslator()
	w.finishDangling(w.sessID)
	w.finishSpeakers(w.sessID)
}

func (w *subtitleWindow) onSessionEnd(id uint64, err error) {
	if id != w.sessID || !w.running {
		return
	}
	w.running = false
	if err != nil {
		w.errCode = subtitleErrCode(err)
		w.log.emit("session_error", map[string]any{"session": id, "code": w.errCode, "detail": err.Error()})
	}
	w.finishDangling(id)
	w.finishSpeakers(id)
	w.refresh()
}

func (w *subtitleWindow) finishDangling(id uint64) {
	for i := range w.entries {
		if e := &w.entries[i]; e.session == id && !e.final && e.original != "" {
			e.final = true
			w.requestFinal(i, "session_end")
		}
	}
}

func (w *subtitleWindow) findEntry(session uint64, segment int) int {
	for i := len(w.entries) - 1; i >= 0; i-- {
		if e := w.entries[i]; e.session == session && e.segment == segment {
			return i
		}
		if w.entries[i].session < session {
			break
		}
	}
	return -1
}

func (w *subtitleWindow) onEvent(ev subtitles.Event) {
	if ev.Session != w.sessID || !w.running {
		w.log.emit("stale_event", map[string]any{"session": ev.Session, "current": w.sessID, "segment": ev.Segment})
		return
	}
	defer w.refresh()
	if ev.Kind == subtitles.Status {
		w.status = ev.Code
		w.log.emit("status", map[string]any{"session": ev.Session, "code": ev.Code})
		return
	}
	if ev.Kind == subtitles.Correction {
		w.onCorrection(ev)
		return
	}
	if ev.Kind == subtitles.Speakers {
		w.onSpeakers(ev)
		return
	}
	if ev.Kind == subtitles.SpeakersOff {
		w.onSpeakersOff(ev)
		return
	}
	i := w.findEntry(ev.Session, ev.Segment)
	if i < 0 {
		if ev.Text == "" {
			return
		}
		w.entries = append(w.entries, subtitleEntry{session: ev.Session, segment: ev.Segment, firstAt: ev.At, start: -1, end: -1})
		i = len(w.entries) - 1
	}
	e := &w.entries[i]
	if e.final || ev.Rev <= e.rev {
		return // a revision after the final, or out of order: never a duplicate
	}
	e.rev, e.original = ev.Rev, ev.Text
	w.changes++
	kind := "asr_partial"
	if ev.Kind == subtitles.Final {
		kind = "asr_final"
	}
	e.ev = ev
	if e.hyps = append(e.hyps, ev); len(e.hyps) > 32 {
		e.hyps = e.hyps[1:]
	}
	w.log.emit(kind, map[string]any{"session": ev.Session, "segment": ev.Segment, "rev": ev.Rev, "chars": len([]rune(ev.Text)), "text": ev.Text,
		"fed_s": ev.FedS, "engine_audio_s": ev.AudioS, "token_end_s": ev.TokenEndS})
	if ev.Kind != subtitles.Final {
		w.onRevision(i)
		return
	}
	e.final, e.finalAt, e.hyps = true, ev.At, nil
	if e.original == "" {
		w.dropJobs(i)
		w.entries = append(w.entries[:i], w.entries[i+1:]...)
		return
	}
	w.requestFinal(i, "final")
	w.speakerFinal(i, ev)
}

/* ---------- bounds ---------- */

// resized notes a size the user chose, at the window's edge or its handle.
func (w *subtitleWindow) resized() {
	b := w.win.ContentBounds()
	if b.Width != w.expectW || b.Height != w.expectH {
		w.userSized = true
	}
	w.scheduleSave()
}

// scheduleSave saves the bounds once moving or resizing settles.
func (w *subtitleWindow) scheduleSave() {
	if w.saveTimer != nil {
		w.saveTimer.Stop()
	}
	w.saveTimer = time.AfterFunc(400*time.Millisecond, func() { w.win.Update(w.saveBounds) })
}

func (w *subtitleWindow) saveBounds() {
	b := w.win.ContentBounds()
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	user := w.userSized
	s := w.a.settings.Get().Subtitles.Normalized()
	if s.Placed && s.X == b.X && s.Y == b.Y && (!user || (s.Width == b.Width && s.Height == b.Height)) {
		return
	}
	w.a.changeSubtitles(func(s *config.Subtitles) {
		s.Placed, s.X, s.Y = true, b.X, b.Y
		if user {
			s.Width, s.Height = b.Width, b.Height
		}
	})
}

func (w *subtitleWindow) setBounds(b mygo.Rectangle) {
	wa := mygo.Screen.DisplayMatching(b).WorkArea
	b.Width = max(subMinW, min(b.Width, wa.X+wa.Width-b.X))
	b.Height = max(subMinH, min(b.Height, wa.Y+wa.Height-b.Y))
	w.expectW, w.expectH = b.Width, b.Height
	w.win.SetContentBounds(b)
}

// fitDefault re-derives the default height after a font or original
// display change, while the user has not chosen a size.
func (w *subtitleWindow) fitDefault() {
	if w.userSized {
		return
	}
	s := w.a.settings.Get().Subtitles.Normalized()
	b := w.win.ContentBounds()
	b.Height = subtitleDefaultHeight(s, !s.HideOriginal)
	w.setBounds(b)
}

/* ---------- views ---------- */

// subTheme is the dark theme with the app's purple accent; the window
// itself stays transparent behind the panel.
func subTheme(c *ui.Context) {
	t := *c.Theme()
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = ui.Hex("#161616"), ui.Hex("#1b1b1b"), ui.Hex("#242424"), ui.Hex("#2e2e2e")
	t.Text, t.TextMuted, t.Border = ui.Hex("#fafafa"), ui.Hex("#a3a3a3"), ui.Transparent
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#6366c4"), ui.Hex("#7477d4"), ui.Hex("#5053ad"), ui.Hex("#ffffff")
	t.Selection = ui.RGBA(116, 119, 212, 0.45)
	c.SetTheme(&t)
	c.Root().Background(ui.Transparent)
}

func (w *subtitleWindow) view(c *ui.Context) {
	subTheme(c)
	w.frame++
	s := w.a.settings.Get().Subtitles.Normalized()
	src := subtitleSource(s)
	if w.fixture {
		src = "en"
	}
	showOriginal := !s.HideOriginal

	// Follow the newest entry until the reader scrolls up from it; back at
	// the end, or with Latest, it follows again.
	// An open speaker menu holds the view still, so its chip stays put.
	if !w.scroll.Scrolling() && !w.whoOpen {
		atEnd := w.scroll.Y >= w.scroll.MaxY-2
		if w.follow && !atEnd && w.reanchorAt == 0 {
			w.follow = false
		} else if !w.follow && atEnd && w.reanchorAt == 0 {
			w.follow = true
		}
	}
	w.keepAnchor(c, s)
	if w.follow && !w.whoOpen {
		w.scroll.Y = math.MaxFloat32
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		w.save(nil)
	}
	if c.Shortcut(ui.Cmd, ui.KeyZ) {
		w.undoSpeakers()
	}
	clear(w.chipRects)

	bg := ui.RGBA(16, 16, 20, float32(s.Opacity)/100)
	panel := ui.Column(c.Key("panel")).Fill().Radius(16).Background(bg).Border(1, subHair).Clip()
	hover := panel.Hovered()
	panel.Children(func() {
		w.header(c, s, src)
		if w.errCode != "" || w.textErr {
			w.errorRow(c)
		}
		ui.Box(c.Key("body")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Scroll(c.Key("captions")).TrackScroll(&w.scroll).Fill().Padding(subPadTop, 22, 18).Children(func() {
				content := ui.Column(c.Key("content")).FillWidth().Gap(12)
				w.content = content.Bounds()
				content.Children(func() {
					w.rows = w.rows[:0]
					for i, e := range w.entries {
						w.entry(c, i, e, i < len(w.entries)-1, s, showOriginal)
					}
				})
			})
			if !w.follow {
				ui.Row(c.Key("latest-wrap")).Absolute().Left(0).Right(0).Bottom(12).Justify(ui.Center).Children(func() {
					b := ui.ButtonBase(c.Key("latest")).Height(26).Padding(0, 10, 0, 8).Gap(5).Radius(13).AlignItems(ui.Center).
						Background(subLatestBg).Border(1, ui.RGBA(255, 255, 255, 0.12)).Shadow(0, 6, 16, 0, ui.RGBA(0, 0, 0, 0.35)).Label(T("subLatestAX"))
					b.Children(func() {
						ui.Icon(c, icSubDown).Size(12, 12).TextColor(subInk)
						ui.Text(c, T("subLatest")).FontSize(12).TextColor(subInk).SingleLine()
					})
					if b.Clicked() {
						w.follow, w.anchored = true, false
						w.scroll.Y = math.MaxFloat32
					}
				})
			}
			w.resizeHandle(c, hover)
		})
	})
}

// keepAnchor records the reading position as an entry and a fraction into
// it, and restores it after a change of width, font sizes or original
// display reflows the entries. The rows' bounds are those of the layout
// before this frame's, so one frame after a change may still show the old
// geometry: the restore runs on every frame from the one after the change
// until the content height has held for two frames (at most ten), and a
// change during it (a resize drag) keeps the anchor it started from.
func (w *subtitleWindow) keepAnchor(c *ui.Context, s config.Subtitles) {
	sig := strconv.Itoa(w.win.ContentBounds().Width) + "|" + strconv.Itoa(s.OrigSize) + "|" + strconv.Itoa(s.TransSize) + "|" + strconv.FormatBool(s.HideOriginal)
	changed := sig != w.layoutSig && w.layoutSig != ""
	w.layoutSig = sig
	if w.follow || len(w.rows) == 0 {
		w.reanchorAt, w.contentH = 0, w.content.H
		return
	}
	if changed && w.anchored {
		if w.reanchorAt == 0 {
			w.reanchorStart = w.frame
		}
		w.reanchorAt = max(w.reanchorAt, w.frame+2)
	}
	offset := func(i int) float32 { return w.rows[i].Y - w.content.Y + subPadTop }
	if w.reanchorAt != 0 {
		if w.frame > w.reanchorStart && w.anchorRow < len(w.rows) {
			w.scroll.Y = max(0, offset(w.anchorRow)+w.anchorFrac*w.rows[w.anchorRow].H)
		}
		if w.content.H != w.contentH && w.frame < w.reanchorStart+10 {
			w.reanchorAt = max(w.reanchorAt, w.frame+2)
		}
		w.contentH = w.content.H
		if w.frame >= w.reanchorAt {
			w.reanchorAt = 0
		} else {
			c.After(16 * time.Millisecond)
		}
		return
	}
	w.contentH = w.content.H
	top := w.scroll.Y
	for i := range w.rows {
		if offset(i)+w.rows[i].H > top {
			w.anchorRow, w.anchored = i, true
			w.anchorFrac = 0
			if h := w.rows[i].H; h > 0 {
				w.anchorFrac = (top - offset(i)) / h
			}
			return
		}
	}
}

func (w *subtitleWindow) header(c *ui.Context, s config.Subtitles, src string) {
	ui.Row(c.Key("head")).Height(subHeadH).FillWidth().Shrink(0).Padding(0, 8, 0, 6).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		w.grip(c)
		// The source gives way first when the header is narrow (320 DIP).
		ui.Row(c.Key("source")).Gap(7).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
			level := subLive
			if !w.running && !w.fixture || w.paused || w.status == "silent" {
				level = subInk3
			}
			ui.Row(c).Gap(2).Height(12).AlignItems(ui.Center).Children(func() {
				for i, h := range []float32{6, 12, 8} {
					ui.Box(c.Key("lv"+strconv.Itoa(i))).Width(2).Height(h).Radius(1).Background(level)
				}
			})
			ui.Text(c, T("subSystemAudio")).FontSize(12).TextColor(subInk2).SingleLine().MinWidth(0).Shrink(1)
		})
		tag := subtitleSourceTag(src)
		if !sameLanguage(src, s.Target, "") || src == "auto" || src == "bilingual" {
			tag += " → " + subtitleTargetTag(s.Target)
		}
		subTagText(c, "lang", tag)
		if w.fixture {
			subTagText(c, "fixture", T("subFixture"))
		}
		if state := w.stateText(); state != "" {
			ui.Text(c, state).FontSize(12).TextColor(subInk3).SingleLine().MinWidth(0).Shrink(1)
		}
		w.savedState(c)
		ui.Spacer(c).Grow(1)
		ui.Row(c.Key("controls")).Gap(2).Shrink(0).Children(func() {
			if subIconButtonIf(c, "undo", icSubUndo, T("subUndo"), len(w.spk.undo) > 0).Clicked() && len(w.spk.undo) > 0 {
				w.undoSpeakers()
			}
			if subIconButton(c, "save", icSubSave, T("subSaveSession"), false).Clicked() {
				w.save(nil)
			}
			pauseIcon, pauseLabel := icSubPause, T("subPause")
			if w.paused || (!w.running && !w.fixture) {
				pauseIcon, pauseLabel = icSubPlay, T("subResume")
			}
			if subIconButton(c, "pause", pauseIcon, pauseLabel, false).Clicked() {
				if !w.fixture && !w.running && !w.paused {
					w.startLive() // after a failure: try again
				} else {
					w.togglePause()
				}
			}
			newB := subIconButton(c, "new", icSubNew, T("subNewSession"), w.confirmOpen)
			w.newRect = newB.Bounds()
			if newB.Clicked() {
				if w.confirmOpen {
					w.hideConfirm()
				} else {
					w.requestNewSession()
				}
			}
			display := subIconButton(c, "display", icSliders, T("subDisplayOptions"), w.cardOpen)
			w.displayRect = display.Bounds()
			if display.Clicked() {
				if w.cardOpen {
					w.hideCard()
				} else {
					w.showCard()
				}
			}
			if subIconButton(c, "close", icSubClose, T("subClose"), false).Clicked() {
				w.close()
			}
		})
	})
}

// savedState is the save status: unsaved (amber dot), saved, or failed.
func (w *subtitleWindow) savedState(c *ui.Context) {
	text, col := "", subInk3
	switch {
	case w.saveFailed:
		text, col = T("subSaveFailed"), subDanger
	case w.dirty():
		text = T("subUnsaved")
	case len(w.entries) > 0:
		text = T("subSaved")
	default:
		return
	}
	ui.Row(c.Key("saved")).Gap(5).AlignItems(ui.Center).Shrink(1).MinWidth(0).Role(ui.RoleStatus).Label(text).Children(func() {
		if text == T("subUnsaved") {
			ui.Box(c.Key("dirty")).Width(5).Height(5).Radius(2.5).Background(ui.Hex("#fbbf24")).Shrink(0)
		}
		ui.Text(c, text).FontSize(11.5).TextColor(col).SingleLine().MinWidth(0).Shrink(1)
	})
}

func (w *subtitleWindow) stateText() string {
	switch {
	case w.paused:
		return T("subPaused")
	case w.fixture:
		return ""
	case w.running && w.status == "loading":
		return T("subLoading")
	case w.running && w.status == "silent":
		return T("subSilent")
	}
	return ""
}

// errorRow says why the session ended, with the way to set it up.
func (w *subtitleWindow) errorRow(c *ui.Context) {
	ui.Row(c.Key("error")).FillWidth().Shrink(0).Padding(0, 22, 6).Gap(10).AlignItems(ui.Center).Children(func() {
		code := w.errCode
		if code == "" {
			code = "text"
		}
		ui.Text(c, T("subErr_"+code)).FontSize(12).TextColor(subDanger).Grow(1).MinWidth(0)
		b := ui.ButtonBase(c.Key("err-settings")).Shrink(0).Label(T("subSettings"))
		b.Children(func() { ui.Text(c, T("subSettings")).FontSize(12).TextColor(subAccentFg).SingleLine() })
		if b.Clicked() {
			w.a.prefs.open("subtitles")
		}
	})
}

// grip moves the window with the arrow keys, 10 DIP or 40 with Shift; the
// pointer drags the whole header.
func (w *subtitleWindow) grip(c *ui.Context) {
	g := ui.ButtonBase(c.Key("grip")).Width(24).Height(28).Radius(7).AlignItems(ui.Center).Justify(ui.Center).Label(T("subMove")).Cursor(ui.CursorGrab)
	col := ui.RGBA(255, 255, 255, 0.32)
	if g.Hovered() || g.Focused() {
		col = ui.RGBA(255, 255, 255, 0.6)
	}
	g.Children(func() { ui.Icon(c, icSubGrip).Size(16, 16).TextColor(col) })
	if g.Focused() {
		if dx, dy, ok := arrowStep(c); ok {
			b := w.win.ContentBounds()
			wa := mygo.Screen.DisplayMatching(b).WorkArea
			b.X = min(max(b.X+dx, wa.X), wa.X+wa.Width-b.Width)
			b.Y = min(max(b.Y+dy, wa.Y), wa.Y+wa.Height-b.Height)
			w.win.SetContentBounds(b)
			w.scheduleSave()
		}
	}
}

// arrowStep is the arrow key pressed this frame, as a step of 10 DIP or 40 with Shift.
func arrowStep(c *ui.Context) (dx, dy int, ok bool) {
	for _, m := range []ui.Modifiers{0, ui.Shift} {
		d := 10
		if m == ui.Shift {
			d = 40
		}
		switch {
		case c.Shortcut(m, ui.KeyLeft):
			return -d, 0, true
		case c.Shortcut(m, ui.KeyRight):
			return d, 0, true
		case c.Shortcut(m, ui.KeyUp):
			return 0, -d, true
		case c.Shortcut(m, ui.KeyDown):
			return 0, d, true
		}
	}
	return 0, 0, false
}

func subTagText(c *ui.Context, key, text string) {
	ui.Box(c.Key(key)).Padding(1, 7).Radius(6).Background(subTag).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(11).TextColor(subInk2).SingleLine()
	})
}

func subIconButton(c *ui.Context, key string, svg *ui.SVG, label string, on bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Width(30).Height(30).Radius(8).AlignItems(ui.Center).Justify(ui.Center).Label(label).Tooltip(label)
	col := subInk2
	if on || b.Hovered() {
		b.Background(subHover)
		col = subInk
	}
	b.Children(func() {
		ui.Icon(c, svg).Size(16, 16).TextColor(col)
	})
	return b
}

// subIconButtonIf is subIconButton, disabled and muted unless enabled.
func subIconButtonIf(c *ui.Context, key string, svg *ui.SVG, label string, enabled bool) ui.Element {
	if enabled {
		return subIconButton(c, key, svg, label, false)
	}
	b := ui.ButtonBase(c.Key(key)).Width(30).Height(30).Radius(8).AlignItems(ui.Center).Justify(ui.Center).Label(label).Tooltip(label).Disabled(true)
	b.Children(func() {
		ui.Icon(c, svg).Size(16, 16).TextColor(ui.RGBA(255, 255, 255, 0.24))
	})
	return b
}

// entry draws one segment. History is a quieter colour, not half opacity.
// A segment in the target language shows once, as the caption.
func (w *subtitleWindow) entry(c *ui.Context, i int, e subtitleEntry, past bool, s config.Subtitles, showOriginal bool) {
	origCol, trCol, trWeight := subInk2, subInk, 500
	if past {
		origCol, trCol, trWeight = subInk3, subInk2, 400
	}
	trSize := float32(s.TransSize)
	row := ui.Column(c.Key("e"+strconv.Itoa(int(e.session))+"-"+strconv.Itoa(e.segment))).FillWidth().Gap(3)
	w.rows = append(w.rows, row.Bounds())
	row.Children(func() {
		// Speaker labels; a reliably split entry shows each utterance's text under its label.
		origShown := e.same || showOriginal || e.translation == ""
		size, col, weight := float32(s.OrigSize), origCol, 400
		switch {
		case e.same:
			size, col, weight = trSize, trCol, trWeight
		case !showOriginal:
			size, col = trSize, subInk3
		}
		split := w.speakerLabels(c, i, e, past, origShown, func(t string) {
			ui.Text(c, t).FontSize(size).LineHeight(subLineHeight).FontWeight(weight).TextColor(col).Selectable()
		})
		if e.same {
			if !split {
				ui.Text(c, e.original).FontSize(trSize).LineHeight(subLineHeight).FontWeight(trWeight).TextColor(trCol).Selectable()
			}
			return
		}
		// Without the original row, the original stands in until a translation comes.
		if origShown && !split {
			ui.Text(c, e.original).FontSize(size).LineHeight(subLineHeight).TextColor(col).Selectable()
		}
		// A draft and its final correction are the same caption, updated in place.
		if e.translation != "" {
			ui.Text(c, e.translation).FontSize(trSize).LineHeight(subLineHeight).FontWeight(trWeight).TextColor(trCol).Selectable()
		}
		switch {
		case e.tr == trFailed:
			ui.Row(c.Key("failed")).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T("subTranslateFailed")).FontSize(12).TextColor(subInk3).SingleLine()
				b := ui.ButtonBase(c.Key("retry")).Height(22).Padding(0, 8).Radius(6).Background(subTag).Label(T("subRetry"))
				b.Children(func() { ui.Text(c, T("subRetry")).FontSize(12).TextColor(subAccentFg).SingleLine() })
				if b.Clicked() {
					w.retry(i)
				}
			})
		case e.translation == "" && showOriginal:
			{
				ui.Row(c.Key("pending")).Height(trSize*subLineHeight).Gap(5).AlignItems(ui.Center).Children(func() {
					for d := range 3 {
						ui.Box(c.Key("d"+strconv.Itoa(d))).Width(5).Height(5).Radius(2.5).Background(subDot)
					}
				})
			}
		}
	})
}

// resizeHandle resizes from the bottom-right corner; the top-left stays.
// It follows the pointer on the screen, not deltas in the window: the
// handle moves with the corner it resizes. Arrow keys resize it while it
// has the focus, 10 DIP or 40 with Shift.
func (w *subtitleWindow) resizeHandle(c *ui.Context, hover bool) {
	h := ui.ButtonBase(c.Key("resize")).Absolute().Right(2).Bottom(2).Width(22).Height(22).Radius(0, 0, 14, 0).
		AlignItems(ui.Center).Justify(ui.Center).Label(T("subResize")).Cursor(ui.CursorResizeNWSE)
	focused := h.Focused()
	col := ui.RGBA(255, 255, 255, 0.5)
	if h.Hovered() || w.resizing || focused {
		col = subInk
	}
	h.Children(func() {
		ui.Icon(c, icSubResize).Size(12, 12).TextColor(col)
	})
	if !(hover || w.resizing || focused || h.Hovered()) {
		h.Opacity(0)
	}
	switch {
	case h.Pressed():
		p := mygo.Screen.CursorScreenPoint()
		if !w.resizing {
			w.resizing, w.rsStart, w.rsBounds = true, p, w.win.ContentBounds()
		}
		b := w.rsBounds
		b.Width += p.X - w.rsStart.X
		b.Height += p.Y - w.rsStart.Y
		if cur := w.win.ContentBounds(); cur.Width != max(subMinW, b.Width) || cur.Height != max(subMinH, b.Height) {
			w.userSized = true
			w.setBounds(b)
		}
		c.After(16 * time.Millisecond) // the pointer may move outside the window
	case w.resizing:
		w.resizing = false
		w.saveBounds()
	}
	if focused {
		if dx, dy, ok := arrowStep(c); ok {
			b := w.win.ContentBounds()
			b.Width += dx
			b.Height += dy
			w.userSized = true
			w.setBounds(b)
			w.scheduleSave()
		}
	}
}

/* ---------- display card ---------- */

func (w *subtitleWindow) showCard() {
	r := w.displayRect
	w.card.SetAnchor(mygo.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W), Height: int(r.H)})
	w.cardOpen = true
	w.card.Show()
	w.card.Invalidate()
}

func (w *subtitleWindow) hideCard() {
	if !w.cardOpen {
		return
	}
	w.cardOpen = false
	w.card.Hide()
	w.win.Invalidate()
}

// cardView is the compact display card: font sizes, opacity, original.
func (w *subtitleWindow) cardView(c *ui.Context) {
	subTheme(c)
	s := w.a.settings.Get().Subtitles.Normalized()
	origOff := s.HideOriginal
	ui.Column(c.Key("card")).Fill().Padding(6).Radius(14).Background(subCardBg).Children(func() {
		ui.Column(c.Key("rows")).FillWidth().Dividers(1, subRowLine).Children(func() {
			subCardRow(c, "orig", T("subOrigSize"), origOff, func() {
				subStepper(c, "orig", T("subOrigSize"), s.OrigSize, config.SubtitleOrigMin, config.SubtitleOrigMax, origOff, false, func(v int) {
					w.a.changeSubtitles(func(s *config.Subtitles) { s.OrigSize = v })
					w.fitDefault()
				})
			})
			subCardRow(c, "trans", T("subTransSize"), false, func() {
				subStepper(c, "trans", T("subTransSize"), s.TransSize, config.SubtitleTransMin, config.SubtitleTransMax, false, false, func(v int) {
					w.a.changeSubtitles(func(s *config.Subtitles) { s.TransSize = v })
					w.fitDefault()
				})
			})
			subCardRow(c, "opacity", T("subOpacity"), false, func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					v := float64(s.Opacity)
					if ui.StepSlider(c.Key("opacity"), &v, config.SubtitleOpacityMin, 100, 1).Width(104).Label(T("subOpacity")).Changed() {
						w.a.changeSubtitles(func(s *config.Subtitles) { s.Opacity = int(math.Round(v)) })
					}
					ui.Text(c, strconv.Itoa(s.Opacity)+"%").FontSize(12).TextColor(subInk3).Width(34).SingleLine()
				})
			})
			subCardRow(c, "original", T("subShowOriginal"), false, func() {
				on := !s.HideOriginal
				if ui.Switch(c.Key("sw"), &on).Label(T("subShowOriginal")).Changed() {
					w.a.changeSubtitles(func(s *config.Subtitles) { s.HideOriginal = !on })
					w.fitDefault()
				}
			})
		})
		link := ui.ButtonBase(c.Key("open-settings")).FillWidth().Height(36).Padding(0, 10).Radius(9).AlignItems(ui.Center).Label(T("subSettings"))
		if link.Hovered() {
			link.Background(subHover)
		}
		link.Children(func() {
			ui.Text(c, T("subSettings")).FontSize(13).TextColor(subInk2).Grow(1).SingleLine()
			ui.Icon(c, icSubNext).Size(12, 12).TextColor(subInk2)
		})
		if link.Clicked() {
			w.hideCard()
			w.a.prefs.open("subtitles")
		}
	})
}

func subCardRow(c *ui.Context, key, label string, off bool, control func()) {
	col := subInk2
	if off {
		col = subInk3
	}
	ui.Row(c.Key(key)).FillWidth().Height(44).Padding(0, 8, 0, 10).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(col).Grow(1).SingleLine()
		control()
	})
}

// subStepper is a − value + control; an end of its range disables its button.
// light draws it for the settings window instead of the dark panel.
func subStepper(c *ui.Context, key, label string, v, lo, hi int, disabled, light bool, set func(int)) {
	t := c.Theme()
	bg, line, ink, h := ui.RGBA(255, 255, 255, 0.06), ui.RGBA(255, 255, 255, 0.06), subInk, float32(28)
	if light {
		bg, line, ink, h = t.Background, settingsGroupLine(c), t.Text, 32
	}
	ui.Row(c.Key(key+"-stepper")).Height(h).Radius(9).Background(bg).Border(1, line).AlignItems(ui.Center).Shrink(0).Children(func() {
		step := func(id string, svg *ui.SVG, name string, to int, can bool) {
			b := ui.ButtonBase(c.Key(id)).Width(h).Height(h-2).Radius(8).AlignItems(ui.Center).Justify(ui.Center).Label(label + " " + name).Disabled(disabled || !can)
			col := ink
			if disabled || !can {
				col = ui.RGBA(128, 128, 128, 0.45)
			} else if b.Hovered() {
				b.Background(subHover)
			}
			b.Children(func() { ui.Icon(c, svg).Size(12, 12).TextColor(col) })
			if b.Clicked() && !disabled && can {
				set(to)
			}
		}
		step("minus", icSubMinus, T("subSmaller"), v-1, v > lo)
		col := ink
		if disabled {
			col = ui.RGBA(128, 128, 128, 0.7)
		}
		ui.Text(c, strconv.Itoa(v)).FontSize(13).TextColor(col).Width(34).SingleLine().Justify(ui.Center)
		step("plus", icSubPlus, T("subLarger"), v+1, v < hi)
	})
}

/* ---------- synthetic fixture ---------- */

// The synthetic fixture: seven English lines streamed word by word, each
// followed by a sample translation, repeating so the history keeps growing.
var subtitleFixture = []struct{ en, zh string }{
	{"Welcome back, everyone, and thanks for joining the quarterly review.", "欢迎大家回来，感谢参加本季度回顾。"},
	{"Today we will look at three things: growth, retention, and the roadmap.", "今天我们看三件事：增长、留存和路线图。"},
	{"Active users grew by eighteen percent compared with last quarter.", "活跃用户比上季度增长了百分之十八。"},
	{"Most of that came from teams who invited their colleagues.", "其中大部分来自邀请同事加入的团队。"},
	{"Retention is steady, but new users still drop off in the first week.", "留存保持稳定，但新用户在第一周仍有流失。"},
	{"So the next release focuses on a shorter, clearer first run.", "所以下一个版本专注于更短、更清晰的首次使用流程。"},
	{"Let's open the floor for questions before we move on.", "在继续之前，我们先开放提问。"},
}

func (w *subtitleWindow) startFeed() {
	if !w.fixture || w.paused {
		return
	}
	w.fixtureGen++
	w.spk.on[0] = true
	w.scheduleFixture(w.fixtureGen, 300*time.Millisecond)
}

func (w *subtitleWindow) stopFeed() {
	w.fixtureGen++
}

func (w *subtitleWindow) scheduleFixture(gen uint64, d time.Duration) {
	time.AfterFunc(d, func() {
		w.win.Update(func() {
			if gen != w.fixtureGen || w.paused {
				return
			}
			w.scheduleFixture(gen, w.fixtureStep())
		})
	})
}

// fixtureStep advances the feed by one event and says when the next is due.
// Its entries are session 0, which live sessions never use.
func (w *subtitleWindow) fixtureStep() time.Duration {
	defer w.refresh()
	line := subtitleFixture[w.fxLine]
	words := strings.Fields(line.en)
	n := len(w.entries)
	w.changes++
	if n == 0 || w.entries[n-1].tr == trDone {
		if n > 0 && !w.entries[n-1].spkDone {
			w.fixtureSpeaker(n - 1)
		}
		w.entries = append(w.entries, subtitleEntry{segment: n, tr: trPending, start: -1, end: -1})
		w.fxWord = 0
		return 260 * time.Millisecond
	}
	e := &w.entries[n-1]
	if w.fxWord < len(words) {
		word := words[w.fxWord]
		if e.original != "" {
			e.original += " "
		}
		e.original += word
		w.fxWord++
		if w.fxWord == len(words) {
			e.final = true
			return 480 * time.Millisecond
		}
		return time.Duration(170+22*len(strings.Trim(word, ",.:;?!'"))) * time.Millisecond
	}
	s := w.a.settings.Get().Subtitles.Normalized()
	e.same = sameLanguage("en", s.Target, e.original)
	e.translation, e.shownSrc, e.shownFinal, e.tr = line.zh, e.original, true, trDone
	w.fxLine = (w.fxLine + 1) % len(subtitleFixture)
	return 1300 * time.Millisecond
}

// subtitleFixtureSpeakers are the fixture's scripted labels, one per line;
// line 6 is labelled wrong on purpose, for the assign and merge steps.
var subtitleFixtureSpeakers = []string{"a1", "a2", "a1", "a2", "a2", "a3", "a1"}

// subtitleFixtureTurns script the turns of some lines, by word: speaker
// per word, 0 for none (unknown audio), and an extra speaker over some
// words. They exercise the real alignment: line 3 an unknown interior,
// line 4 an unknown start, line 5 one overlapped word, line 7 an unknown end.
var subtitleFixtureTurns = map[int]struct {
	words []int
	over  map[int]int
}{
	2: {words: []int{1, 1, 1, 0, 0, 0, 0, 1, 1, 1}},
	3: {words: []int{0, 0, 0, 0, 0, 2, 2, 2, 2, 2}},
	4: {words: []int{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}, over: map[int]int{6: 3}},
	6: {words: []int{1, 1, 1, 1, 1, 1, 0, 0, 0, 0}},
}

// fixtureSpeaker labels a fixture entry a line after its translation, as
// late labels do. Only the labelled sample feed is labelled this way.
func (w *subtitleWindow) fixtureSpeaker(i int) {
	e := &w.entries[i]
	line := e.segment % len(subtitleFixture)
	defer w.refreshWho()
	if sc, ok := subtitleFixtureTurns[line]; ok {
		words := strings.Fields(e.original)
		if len(words) == len(sc.words) {
			// Each word a token 0.35 s long, each entry a minute of its own on the fixture's clock.
			const word = subSampleRate * 35 / 100
			base := int64(e.segment) * 60 * subSampleRate
			e.start, e.end, e.tokens = base, base+int64(len(words))*word, nil
			turns := w.spk.turns[0]
			for k, wd := range words {
				at := base + int64(k)*word
				e.tokens = append(e.tokens, subtitles.Token{Text: "▁" + wd, Sample: at + word/4})
				if spk := sc.words[k]; spk > 0 {
					turns = append(turns, subtitles.SpeakerTurn{Start: at, End: at + word, Speaker: spk})
				}
				if spk := sc.over[k]; spk > 0 {
					turns = append(turns, subtitles.SpeakerTurn{Start: at, End: at + word, Speaker: spk})
				}
			}
			w.spk.turns[0] = turns
			w.alignSpeakers(i)
			return
		}
	}
	id := subtitleFixtureSpeakers[line]
	w.ensureProfile(id)
	e.utts, e.spkDone = []subUtt{{start: -1, end: -1, auto: id}}, true
}
