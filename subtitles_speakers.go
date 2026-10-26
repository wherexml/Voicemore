package main

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/subtitles"
)

// Same-session speaker labels of live subtitles (docs/public/LIVE-SUBTITLES.md),
// after mock/live-subtitles-speakers. internal/subtitles commits speaker
// turns of session audio as they are inferred; here a final's tokens are
// aligned to them. A segment splits into utterances only where the
// alignment is reliable; anything else stays explicit (unknown, overlap,
// mixed) and is never given a majority speaker. Labels are display state
// only: renaming, assigning, merging and undo never touch text,
// translations or their jobs.

const (
	spkUnknown = "?" // no committed turn covers the speech
	spkOverlap = "~" // two speakers at once
	spkMixed   = "*" // more than one speaker, and no reliable split

	subSampleRate   = 16000
	subSpkNameMax   = 24
	subSpkUndoMax   = 30
	subSpkMinSplit  = subSampleRate * 4 / 5 // samples an utterance of a split must last
	subSpkNear      = subSampleRate / 4     // a token this close to a turn belongs to it
	subSpkTinyGap   = subSampleRate * 3 / 10 // an unknown one-token gap this short may be smoothed
	subSpkMenuW     = 232
	subSpkMenuRowH  = 34
	subSpkMenuRows  = 6 // list rows shown before the list scrolls
	subConfirmW     = 248
	subConfirmH     = 92
	subSpkProfileMx = subtitles.SpeakerMax // manual speakers are offered while fewer profiles exist
)

var subSpkColors = []ui.Color{ui.Hex("#8fb0ff"), ui.Hex("#69e7c3"), ui.Hex("#f5b971"), ui.Hex("#d9a1f2"),
	ui.Hex("#f29b8f"), ui.Hex("#a6d97f"), ui.Hex("#7fd3e8"), ui.Hex("#e8d67f")}

var (
	icSubUndo  = icon(`<path d="M5.6 3.4 2.8 6.2 5.6 9"/><path d="M3 6.2h6.3a3.6 3.6 0 0 1 0 7.2H7.6"/>`)
	icSubSave  = icon(`<path d="M8 2.6v7.2"/><path d="M5 6.9 8 9.9l3-3"/><path d="M2.8 10.4v1.8a1.3 1.3 0 0 0 1.3 1.3h7.8a1.3 1.3 0 0 0 1.3-1.3v-1.8"/>`)
	icSubCheck = icon(`<path d="M3.5 8.4 6.7 11.5l5.8-6.7"/>`)
)

// speakerProfile is a speaker of the subtitle session: automatic ("a<k>",
// k from the session tracker) or made by the user ("m<k>"). Into is the
// speaker it was merged into.
type speakerProfile struct {
	N    int    `json:"n"`
	Name string `json:"name,omitempty"`
	Into string `json:"into,omitempty"`
}

// subUtt is one utterance of an entry: tokens [a, b) of its final (both 0
// without tokens) and its audio. key, its first token, is stable: splits
// are made once, when the entry's audio is fully committed.
type subUtt struct {
	key        int
	a, b       int
	start, end int64
	auto       string // a speaker id, or spkUnknown, spkOverlap, spkMixed
	manual     string // the user's speaker for it, "" for none
}

// speakerState is the speaker side of a subtitle session. Turns and
// coverage are per engine session (sample clocks differ); the tracker's
// voices live in memory only and go with New session or a restore.
type speakerState struct {
	profiles map[string]*speakerProfile
	turns    map[uint64][]subtitles.SpeakerTurn // Speaker -1: a gap, unknown
	covered  map[uint64]int64                   // committed up to this sample
	on       map[uint64]bool                    // labels were started for the engine session
	off      map[uint64]bool                    // and ended early (model missing or failing)
	tracker  *subtitles.SpeakerTracker
	undo     []string
}

func newSpeakerState() speakerState {
	return speakerState{profiles: map[string]*speakerProfile{}, turns: map[uint64][]subtitles.SpeakerTurn{},
		covered: map[uint64]int64{}, on: map[uint64]bool{}, off: map[uint64]bool{}, tracker: subtitles.NewSpeakerTracker()}
}

// resetSpeakers starts a new subtitle session's speakers; next is the
// first automatic id new voices may take.
func (w *subtitleWindow) resetSpeakers(next int) {
	tracker := w.spk.tracker
	w.spk = newSpeakerState()
	w.spk.tracker = tracker
	tracker.Reset(next)
	w.hideWho()
}

// subtitleSpeakersEnabled is whether a sherpa session gets labels: models
// present at root, and not switched off for a paired Dev run.
func subtitleSpeakersEnabled(root string) (bool, string) {
	if devBuild() && os.Getenv(subtitlesSpeakersEnv) == "0" {
		return false, "dev_off"
	}
	seg, emb := subtitles.SpeakerModels(root)
	for _, p := range []string{seg, emb} {
		if _, err := os.Stat(p); err != nil {
			return false, subtitles.ErrSpeakerMissing
		}
	}
	return true, ""
}

/* ---------- inference results ---------- */

func (w *subtitleWindow) onSpeakers(ev subtitles.Event) {
	info := ev.Spk
	if info == nil || !w.spk.on[ev.Session] {
		return
	}
	turns := w.spk.turns[ev.Session]
	if info.Gap {
		turns = append(turns, subtitles.SpeakerTurn{Start: info.From, End: info.To, Speaker: -1})
	} else {
		turns = append(turns, info.Turns...)
	}
	w.spk.turns[ev.Session] = turns
	w.spk.covered[ev.Session] = max(w.spk.covered[ev.Session], info.To)
	labelled := 0
	for i := range w.entries {
		e := &w.entries[i]
		if e.session == ev.Session && e.final && !e.spkDone && e.end >= 0 && e.end <= info.To {
			w.alignSpeakers(i)
			labelled++
		}
	}
	w.log.emit("speaker_apply", map[string]any{"session": ev.Session, "window": info.Window, "from_s": float64(info.From) / subSampleRate,
		"to_s": float64(info.To) / subSampleRate, "gap": info.Gap, "turns": len(info.Turns), "entries": labelled,
		"event_to_apply_ms": time.Since(ev.At).Milliseconds()})
	if labelled > 0 {
		w.changes++
		w.refreshWho()
	}
}

func (w *subtitleWindow) onSpeakersOff(ev subtitles.Event) {
	w.spk.off[ev.Session] = true
	w.log.emit("speaker_off", map[string]any{"session": ev.Session, "code": ev.Code})
}

// speakerFinal takes a final's audio range and tokens, and labels it at
// once when its audio is already committed.
func (w *subtitleWindow) speakerFinal(i int, ev subtitles.Event) {
	e := &w.entries[i]
	e.start, e.end, e.tokens = ev.Start, ev.End, ev.Tokens
	if e.start < 0 || e.end < e.start {
		e.start, e.end, e.tokens = -1, -1, nil // no usable range: labelled from nothing, unknown
	}
	if w.spk.on[e.session] && e.end >= 0 && e.end <= w.spk.covered[e.session] {
		w.alignSpeakers(i)
	}
}

// finishSpeakers ends labelling for an engine session: what its turns
// did not reach is unknown, never left waiting.
func (w *subtitleWindow) finishSpeakers(session uint64) {
	if !w.spk.on[session] || w.spk.off[session] {
		return
	}
	for i := range w.entries {
		if e := &w.entries[i]; e.session == session && !e.spkDone && e.original != "" {
			w.alignSpeakers(i)
		}
	}
}

// labelAt is the speaker of one instant: the committed turn holding it,
// or, with none holding it, one within subSpkNear. Two turns of different
// voices there, an unrecognized one included, are an overlap; a gap
// (backlog, evicted audio) is unknown.
func labelAt(turns []subtitles.SpeakerTurn, at int64) string {
	found := map[int]bool{}
	near, nearD := -1, int64(subSpkNear+1)
	for _, t := range turns {
		switch {
		case at >= t.Start && at < t.End:
			found[t.Speaker] = true
		case at < t.Start && t.Start-at < nearD:
			near, nearD = t.Speaker, t.Start-at
		case at >= t.End && at-t.End < nearD:
			near, nearD = t.Speaker, at-t.End
		}
	}
	gap := found[-1]
	delete(found, -1)
	if len(found) == 0 && !gap && nearD <= subSpkNear && near >= 0 {
		found[near] = true
	}
	switch len(found) {
	case 0:
		return spkUnknown
	case 1:
		for k := range found {
			if k > 0 {
				return "a" + strconv.Itoa(k)
			}
		}
		return spkUnknown
	}
	return spkOverlap
}

// alignSpeakers labels entry i from the committed turns of its session.
// Each token takes the speaker of its instant. Only a tiny unknown gap
// (subSpkTinyGap, one token) between two tokens of the same speaker, or at
// an edge next to a speaker, is smoothed; overlap and longer unknown spans
// stay. Runs become utterances only when every run lasts subSpkMinSplit
// with two tokens or more and the tokens spell the recognized text
// exactly; otherwise the entry is one utterance: overlap when any run is
// one, else mixed — never a majority speaker. Manual choices of earlier
// utterances with the same key are kept.
func (w *subtitleWindow) alignSpeakers(i int) {
	e := &w.entries[i]
	turns := w.spk.turns[e.session]
	manual := map[int]string{}
	for _, u := range e.utts {
		if u.manual != "" {
			manual[u.key] = u.manual
		}
	}
	var utts []subUtt
	if len(e.tokens) == 0 || e.start < 0 {
		lbl := spkUnknown
		if e.start >= 0 && e.end > e.start {
			lbl = rangeLabel(turns, e.start, e.end)
		}
		utts = []subUtt{{start: e.start, end: e.end, auto: lbl}}
	} else {
		utts = tokenRuns(e, turns)
	}
	for k := range utts {
		utts[k].manual = manual[utts[k].key]
		if id := utts[k].auto; strings.HasPrefix(id, "a") {
			w.ensureProfile(id)
		}
	}
	e.utts, e.spkDone = utts, true
	fields := map[string]any{"session": e.session, "segment": e.segment, "utterances": len(utts), "tokens": len(e.tokens)}
	labels := make([]string, len(utts))
	for k, u := range utts {
		labels[k] = u.auto
	}
	fields["labels"] = labels
	w.log.emit("speaker_align", fields)
}

// rangeLabel is the one speaker of [from, to) without tokens, sampled
// every 100 ms: any overlap is overlap, two known voices mixed, and more
// than subSpkTinyGap of unknown audio unknown — never a majority.
func rangeLabel(turns []subtitles.SpeakerTurn, from, to int64) string {
	const step = subSampleRate / 10
	seen := map[string]int{}
	for at := from; at < to; at += step {
		seen[labelAt(turns, at)]++
	}
	unknown, overlap := seen[spkUnknown], seen[spkOverlap]
	delete(seen, spkUnknown)
	delete(seen, spkOverlap)
	switch {
	case overlap > 0:
		return spkOverlap
	case len(seen) > 1:
		return spkMixed
	case len(seen) == 0 || int64(unknown)*step > subSpkTinyGap:
		return spkUnknown
	}
	for k := range seen {
		return k
	}
	return spkUnknown
}

func tokenRuns(e *subtitleEntry, turns []subtitles.SpeakerTurn) []subUtt {
	type run struct {
		a, b int
		lbl  string
	}
	var runs []run
	for k, t := range e.tokens {
		l := labelAt(turns, t.Sample)
		if n := len(runs); n > 0 && runs[n-1].lbl == l {
			runs[n-1].b = k + 1
		} else {
			runs = append(runs, run{k, k + 1, l})
		}
	}
	// A run's audio: from its first token to the next run's first token.
	dur := func(k int) int64 {
		from, to := e.tokens[runs[k].a].Sample, e.end
		if k == 0 {
			from = e.start
		}
		if k < len(runs)-1 {
			to = e.tokens[runs[k+1].a].Sample
		}
		return to - from
	}
	tiny := func(k int) bool {
		return runs[k].lbl == spkUnknown && runs[k].b-runs[k].a == 1 && dur(k) <= subSpkTinyGap
	}
	known := func(l string) bool { return l != spkUnknown && l != spkOverlap }
	for changed := true; changed && len(runs) > 1; {
		changed = false
		for k := range runs {
			switch {
			case !tiny(k):
				continue
			case k > 0 && k < len(runs)-1 && runs[k-1].lbl == runs[k+1].lbl && known(runs[k-1].lbl):
				runs[k-1].b = runs[k+1].b
				runs = append(runs[:k], runs[k+2:]...)
			case k == 0 && known(runs[1].lbl):
				runs[1].a = runs[0].a
				runs = runs[1:]
			case k == len(runs)-1 && known(runs[k-1].lbl):
				runs[k-1].b = runs[k].b
				runs = runs[:k]
			default:
				continue
			}
			changed = true
			break
		}
	}
	at := func(k int) int64 { return e.tokens[k].Sample }
	// Within the entry's range: token times may sit just past its end.
	clamp := func(x int64) int64 { return min(max(x, e.start), e.end) }
	bounds := func(r run, k int) (int64, int64) {
		s, t := clamp(at(r.a)), e.end
		if k == 0 {
			s = e.start
		}
		if k < len(runs)-1 {
			t = clamp(at(runs[k+1].a))
		}
		return s, t
	}
	if len(runs) == 1 {
		return []subUtt{{key: 0, a: 0, b: len(e.tokens), start: e.start, end: e.end, auto: runs[0].lbl}}
	}
	reliable := foldText(tokensText(e.tokens)) == foldText(e.asr())
	overlap := false
	for k, r := range runs {
		s, t := bounds(r, k)
		if r.b-r.a < 2 || t-s < subSpkMinSplit {
			reliable = false
		}
		overlap = overlap || r.lbl == spkOverlap
	}
	if !reliable {
		lbl := spkMixed
		if overlap {
			lbl = spkOverlap
		}
		return []subUtt{{key: 0, a: 0, b: len(e.tokens), start: e.start, end: e.end, auto: lbl}}
	}
	utts := make([]subUtt, len(runs))
	for k, r := range runs {
		s, t := bounds(r, k)
		utts[k] = subUtt{key: r.a, a: r.a, b: r.b, start: s, end: t, auto: r.lbl}
	}
	return utts
}

// asr is the recognizer's own text of the entry, which its tokens spell.
func (e *subtitleEntry) asr() string {
	if e.corrected {
		return e.asrText
	}
	return e.original
}

// tokensText spells tokens as the recognizer does: "▁" starts a word.
func tokensText(toks []subtitles.Token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(strings.ReplaceAll(t.Text, "▁", " "))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// splitShown is whether entry e shows each utterance's own text: only
// while the original is still the recognizer's text its tokens spell.
func splitShown(e *subtitleEntry) bool {
	return len(e.utts) > 1 && len(e.tokens) > 0 && (!e.corrected || e.original == e.asrText) &&
		foldText(tokensText(e.tokens)) == foldText(e.original)
}

/* ---------- profiles ---------- */

func (w *subtitleWindow) nextNumber() int {
	n := 0
	for _, p := range w.spk.profiles {
		n = max(n, p.N)
	}
	return n + 1
}

func (w *subtitleWindow) ensureProfile(id string) {
	if w.spk.profiles[id] == nil {
		w.spk.profiles[id] = &speakerProfile{N: w.nextNumber()}
	}
}

// resolve follows merges to the speaker an id now is.
func (w *subtitleWindow) resolve(id string) string {
	for guard := 0; guard <= len(w.spk.profiles); guard++ {
		p := w.spk.profiles[id]
		if p == nil || p.Into == "" {
			return id
		}
		id = p.Into
	}
	return id
}

func (u *subUtt) effective(w *subtitleWindow) string {
	if u.manual != "" {
		return w.resolve(u.manual)
	}
	return w.resolve(u.auto)
}

func speakerSpecial(id string) bool { return id == spkUnknown || id == spkOverlap || id == spkMixed }

func (w *subtitleWindow) speakerName(id string) string {
	switch id {
	case spkUnknown:
		return T("subSpkUnknown")
	case spkOverlap:
		return T("subSpkOverlap")
	case spkMixed:
		return T("subSpkMixed")
	}
	p := w.spk.profiles[id]
	if p == nil {
		return T("subSpkUnknown")
	}
	if p.Name != "" {
		return p.Name
	}
	return T("subSpkN", p.N)
}

func (w *subtitleWindow) speakerColor(id string) ui.Color {
	if p := w.spk.profiles[id]; p != nil && !speakerSpecial(id) {
		return subSpkColors[(p.N-1)%len(subSpkColors)]
	}
	return subInk3
}

// activeSpeakers are the speakers to offer: not merged away, and holding
// an utterance now.
func (w *subtitleWindow) activeSpeakers() []string {
	used := map[string]bool{}
	for i := range w.entries {
		for k := range w.entries[i].utts {
			used[w.entries[i].utts[k].effective(w)] = true
		}
	}
	var ids []string
	for id, p := range w.spk.profiles {
		if p.Into == "" && used[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(a, b int) bool { return w.spk.profiles[ids[a]].N < w.spk.profiles[ids[b]].N })
	return ids
}

/* ---------- edits and undo ---------- */

type speakerSnap struct {
	Profiles map[string]speakerProfile `json:"p"`
	Manual   map[string]string         `json:"m"`
}

func uttID(e *subtitleEntry, key int) string {
	return strconv.FormatUint(e.session, 10) + "-" + strconv.Itoa(e.segment) + "-" + strconv.Itoa(key)
}

func (w *subtitleWindow) snapSpeakers() string {
	s := speakerSnap{Profiles: map[string]speakerProfile{}, Manual: map[string]string{}}
	for id, p := range w.spk.profiles {
		s.Profiles[id] = *p
	}
	for i := range w.entries {
		e := &w.entries[i]
		for _, u := range e.utts {
			if u.manual != "" {
				s.Manual[uttID(e, u.key)] = u.manual
			}
		}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// edit applies a label edit with an undo step; it never retranslates.
func (w *subtitleWindow) edit(what string, fn func()) {
	w.spk.undo = append(w.spk.undo, w.snapSpeakers())
	if len(w.spk.undo) > subSpkUndoMax {
		w.spk.undo = w.spk.undo[1:]
	}
	fn()
	w.changes++
	w.log.emit("speaker_edit", map[string]any{"what": what, "undo_depth": len(w.spk.undo)})
	w.refreshWho()
	w.refresh()
}

// undo restores the last edit only: automatic speakers that appeared
// since stay, user-made ones go.
func (w *subtitleWindow) undoSpeakers() {
	n := len(w.spk.undo)
	if n == 0 {
		return
	}
	var s speakerSnap
	if json.Unmarshal([]byte(w.spk.undo[n-1]), &s) != nil {
		return
	}
	w.spk.undo = w.spk.undo[:n-1]
	for id, p := range w.spk.profiles {
		switch old, ok := s.Profiles[id]; {
		case ok:
			*p = old
		case strings.HasPrefix(id, "m"):
			delete(w.spk.profiles, id)
		default:
			p.Name, p.Into = "", ""
		}
	}
	for i := range w.entries {
		e := &w.entries[i]
		for k := range e.utts {
			e.utts[k].manual = s.Manual[uttID(e, e.utts[k].key)]
		}
	}
	w.changes++
	w.log.emit("speaker_edit", map[string]any{"what": "undo", "undo_depth": len(w.spk.undo)})
	w.hideWho()
	w.refresh()
}

func (w *subtitleWindow) renameSpeaker(id, raw string) {
	p := w.spk.profiles[id]
	if p == nil {
		return
	}
	name := []rune(strings.TrimSpace(raw))
	if len(name) > subSpkNameMax {
		name = name[:subSpkNameMax]
	}
	next := string(name)
	if next == T("subSpkN", p.N) {
		next = ""
	}
	if next == p.Name {
		return
	}
	w.edit("rename", func() { p.Name = next })
}

func (w *subtitleWindow) assignUtt(i, key int, id string) {
	if u := w.uttOf(i, key); u != nil {
		w.edit("assign", func() { u.manual = id })
	}
}

func (w *subtitleWindow) canCreateSpeaker() bool { return len(w.spk.profiles) < subSpkProfileMx }

func (w *subtitleWindow) createSpeaker(i, key int) string {
	u := w.uttOf(i, key)
	if u == nil || !w.canCreateSpeaker() {
		return ""
	}
	k := 1
	for w.spk.profiles["m"+strconv.Itoa(k)] != nil {
		k++
	}
	id := "m" + strconv.Itoa(k)
	w.edit("create", func() {
		w.spk.profiles[id] = &speakerProfile{N: w.nextNumber()}
		u.manual = id
	})
	return id
}

func (w *subtitleWindow) mergeSpeaker(from, to string) {
	if from == to || w.spk.profiles[from] == nil || w.spk.profiles[to] == nil {
		return
	}
	w.edit("merge", func() {
		for _, p := range w.spk.profiles {
			if p.Into == from {
				p.Into = to
			}
		}
		w.spk.profiles[from].Into = to
	})
}

func (w *subtitleWindow) uttOf(i, key int) *subUtt {
	if i < 0 || i >= len(w.entries) {
		return nil
	}
	for k := range w.entries[i].utts {
		if w.entries[i].utts[k].key == key {
			return &w.entries[i].utts[k]
		}
	}
	return nil
}

/* ---------- labels in the captions ---------- */

// speakerLabels draws an entry's labels: a chip per utterance, each above
// its own text when the entry is split and its text can show (then it
// returns true); else the chips in a row above the whole entry. While its
// audio is not committed yet, waiting dots that cannot be edited.
func (w *subtitleWindow) speakerLabels(c *ui.Context, i int, e subtitleEntry, past, canSplit bool, text func(string)) bool {
	if len(e.utts) == 0 {
		if w.spk.on[e.session] && !w.spk.off[e.session] && !e.spkDone {
			ui.Row(c.Key("who-wait")).Height(22).Gap(4).AlignItems(ui.Center).Children(func() {
				for d := range 3 {
					ui.Box(c.Key("wd"+strconv.Itoa(d))).Width(4).Height(4).Radius(2).Background(ui.RGBA(255, 255, 255, 0.24))
				}
			})
		}
		return false
	}
	split := canSplit && splitShown(&e)
	if split {
		for _, u := range e.utts {
			ui.Column(c.Key("utt-"+strconv.Itoa(u.key))).FillWidth().Gap(1).Children(func() {
				ui.Row(c.Key("who-row")).Margin(0, 0, 0, -7).Children(func() { w.chip(c, i, &e, u, past, false) })
				text(tokensText(e.tokens[u.a:u.b]))
			})
		}
		return true
	}
	ui.Row(c.Key("who-row")).Gap(4).Wrap().Margin(0, 0, 0, -7).Children(func() {
		for _, u := range e.utts {
			// Chips over a whole text that is not split say which part each one is.
			w.chip(c, i, &e, u, past, len(e.utts) > 1)
		}
	})
	return false
}

// uttDetail names which part of an entry an utterance is: its time in the
// session audio and the recognizer's words of it (sherpa's, also when Qwen
// corrected the shown text). Only the tooltip and accessible label carry it.
func uttDetail(e *subtitleEntry, u subUtt) string {
	var parts []string
	if u.start >= 0 && u.end >= u.start {
		parts = append(parts, clockText(u.start)+"–"+clockText(u.end))
	}
	if u.b > u.a && u.b <= len(e.tokens) {
		r := []rune(tokensText(e.tokens[u.a:u.b]))
		if len(r) > 24 {
			r = append(r[:24], '…')
		}
		parts = append(parts, "“"+string(r)+"”")
	}
	return strings.Join(parts, " · ")
}

// clockText is a session sample as m:ss.s.
func clockText(sample int64) string {
	d := float64(sample) / subSampleRate
	m := int(d) / 60
	sec := d - float64(m*60)
	return strconv.Itoa(m) + ":" + strconv.FormatFloat(100+sec, 'f', 1, 64)[1:]
}

func (w *subtitleWindow) chip(c *ui.Context, i int, e *subtitleEntry, u subUtt, past, detail bool) {
	id := u.effective(w)
	name := w.speakerName(id)
	label := name
	if d := uttDetail(e, u); detail && d != "" {
		label = name + " · " + d
	}
	open := w.whoOpen && w.whoSession == e.session && w.whoSegment == e.segment && w.whoKey == u.key
	// The chip's padding hangs left of the text column, as in the mock.
	b := ui.ButtonBase(c.Key("who-"+strconv.Itoa(u.key))).Height(22).Padding(0, 7).Gap(6).Radius(6).
		AlignItems(ui.Center).Shrink(1).MinWidth(0).Label(label).Tooltip(label)
	col := subInk2
	if past {
		col = subInk3
	}
	if open || b.Hovered() {
		b.Background(subHover)
		col = subInk
	}
	b.Children(func() {
		ui.Box(c.Key("dot")).Width(7).Height(7).Radius(3.5).Background(w.speakerColor(id)).Shrink(0)
		ui.Text(c, name).FontSize(12).TextColor(col).SingleLine().MinWidth(0).Shrink(1)
	})
	w.chipRects[uttID(e, u.key)] = b.Bounds()
	if b.Clicked() {
		if open {
			w.hideWho()
		} else {
			w.showWho(i, u.key)
		}
	}
}

/* ---------- speaker menu ---------- */

func (w *subtitleWindow) newWhoCard() *mygo.Window {
	f := mygo.NewFlyout(mygo.FlyoutOptions{
		Parent:    w.win,
		Placement: mygo.PlacementBottomStart,
		Gap:       4,
		Width:     subSpkMenuW,
		Height:    w.whoHeight(),
		Focusable: true,
		Hidden:    true,
		Shadow:    true,
		Content:   ui.View(w.whoView),
	})
	f.OnClose(func(e *mygo.CloseEvent) {
		if !w.a.quitting {
			e.PreventDefault()
			w.hideWho()
		}
	})
	return f
}

// whoHeight fits the menu's rows: name, scope and up to subSpkMenuRows
// choices; more scroll.
func (w *subtitleWindow) whoHeight() int {
	n := 1
	if w.whoOpen {
		n = len(w.whoChoices())
	}
	h := 6 + 6 + min(max(n, 1), subSpkMenuRows)*subSpkMenuRowH
	if !speakerSpecial(w.whoSpeaker) {
		h += 34 + 6
	}
	if w.whoScopeShown() {
		h += 28 + 4
	}
	return h
}

func (w *subtitleWindow) showWho(i, key int) {
	e := &w.entries[i]
	u := w.uttOf(i, key)
	r, ok := w.chipRects[uttID(e, key)]
	if u == nil || !ok {
		return
	}
	w.hideCard()
	w.hideConfirm()
	w.whoOpen, w.whoSession, w.whoSegment, w.whoKey = true, e.session, e.segment, key
	w.whoSpeaker, w.whoScope = u.effective(w), "one"
	w.whoName = ""
	if p := w.spk.profiles[w.whoSpeaker]; p != nil {
		w.whoName = p.Name
	}
	w.whoGen++
	if w.who == nil {
		w.who = w.newWhoCard()
	}
	w.sizeWho()
	w.who.SetAnchor(mygo.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W), Height: int(r.H)})
	w.who.Show()
	w.who.Invalidate()
	w.win.Invalidate()
}

// sizeWho sets the menu's height before it places itself at its chip.
func (w *subtitleWindow) sizeWho() {
	b := w.who.ContentBounds()
	if h := w.whoHeight(); b.Height != h {
		b.Width, b.Height = subSpkMenuW, h
		w.who.SetContentBounds(b)
	}
}

func (w *subtitleWindow) hideWho() {
	if !w.whoOpen {
		return
	}
	w.whoOpen = false
	if w.who != nil {
		w.who.Hide()
	}
	w.win.Invalidate()
}

// refreshWho follows label changes in an open menu: a merge moves it to
// the speaker merged into.
func (w *subtitleWindow) refreshWho() {
	if !w.whoOpen {
		return
	}
	i := w.findEntry(w.whoSession, w.whoSegment)
	u := w.uttOf(i, w.whoKey)
	if u == nil {
		w.hideWho()
		return
	}
	w.whoSpeaker = w.resolve(w.whoSpeaker)
	w.sizeWho()
	w.who.Invalidate()
}

func (w *subtitleWindow) whoScopeShown() bool { return !speakerSpecial(w.whoSpeaker) }

// whoChoices are the menu's list: other active speakers, then a new
// speaker for this utterance only.
func (w *subtitleWindow) whoChoices() []string {
	var out []string
	for _, id := range w.activeSpeakers() {
		if id != w.whoSpeaker {
			out = append(out, id)
		}
	}
	if w.whoScope == "one" {
		out = append(out, "new")
	}
	return out
}

func (w *subtitleWindow) whoView(c *ui.Context) {
	subTheme(c)
	if !w.whoOpen {
		return
	}
	i := w.findEntry(w.whoSession, w.whoSegment)
	cur := w.whoSpeaker
	ui.Column(c.Key("who-menu")).Fill().Padding(6).Radius(12).Background(subCardBg).Border(1, ui.RGBA(255, 255, 255, 0.1)).Children(func() {
		if !speakerSpecial(cur) {
			apply := func() {
				w.renameSpeaker(cur, w.whoName)
				w.hideWho()
			}
			ui.Row(c.Key("who-name")).FillWidth().Height(34).Padding(0, 3, 0, 10).Gap(6).Radius(8).AlignItems(ui.Center).
				Background(ui.RGBA(255, 255, 255, 0.06)).Border(1, ui.RGBA(255, 255, 255, 0.08)).Margin(0, 0, 6, 0).Children(func() {
				ui.Box(c.Key("dot")).Width(7).Height(7).Radius(3.5).Background(w.speakerColor(cur)).Shrink(0)
				// Keyed by the opening, so each opening focuses the name afresh.
				p := w.spk.profiles[cur]
				placeholder := ""
				if p != nil {
					placeholder = T("subSpkN", p.N)
				}
				ui.TextInput(c.Key("who-in-"+strconv.FormatUint(w.whoGen, 10)), &w.whoName).Placeholder(placeholder).Label(T("subSpkName")).
					Grow(1).MinWidth(0).BorderWidth(0).Background(ui.Transparent).FontSize(13).AutoFocus().OnSubmit(apply)
				ok := ui.ButtonBase(c.Key("who-apply")).Width(28).Height(28).Radius(6).AlignItems(ui.Center).Justify(ui.Center).
					Label(T("subSpkApply")).Tooltip(T("subSpkApply"))
				okCol := subInk2
				if ok.Hovered() {
					ok.Background(subHover)
					okCol = subInk
				}
				ok.Children(func() { ui.Icon(c, icSubCheck).Size(12, 12).TextColor(okCol) })
				if ok.Clicked() {
					apply()
				}
			})
		}
		if w.whoScopeShown() {
			ui.Row(c.Key("who-scope")).FillWidth().Height(28).Padding(2).Gap(2).Radius(8).Background(ui.RGBA(255, 255, 255, 0.06)).
				Margin(0, 0, 4, 0).Role(ui.RoleRadioGroup).Label(T("subSpkScope")).Children(func() {
				for _, sc := range []struct{ id, label string }{{"one", T("subSpkOne")}, {"all", T("subSpkAll")}} {
					on := w.whoScope == sc.id
					b := ui.ButtonBase(c.Key("scope-"+sc.id)).Grow(1).FillHeight().Radius(6).AlignItems(ui.Center).Justify(ui.Center).
						Role(ui.RoleRadio).Checked(on).Label(sc.label)
					col := subInk3
					if on {
						b.Background(ui.RGBA(255, 255, 255, 0.12))
						col = subInk
					}
					b.Children(func() { ui.Text(c, sc.label).FontSize(12).TextColor(col).SingleLine() })
					if b.Clicked() && !on {
						w.whoScope = sc.id
						w.sizeWho()
					}
				}
			})
		}
		ui.Scroll(c.Key("who-list")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c.Key("who-opts")).FillWidth().Role(ui.RoleGroup).Label(T("subSpkTo")).Children(func() {
				for _, id := range w.whoChoices() {
					w.whoOption(c, i, id)
				}
			})
		})
	})
}

func (w *subtitleWindow) whoOption(c *ui.Context, i int, id string) {
	isNew := id == "new"
	disabled := isNew && !w.canCreateSpeaker()
	name := T("subSpkNew")
	if !isNew {
		name = w.speakerName(id)
	}
	label := name
	if !isNew {
		if w.whoScope == "one" {
			label = T("subSpkAssignAX", name)
		} else {
			label = T("subSpkMergeAX", name)
		}
	}
	b := ui.ButtonBase(c.Key("opt-"+id)).FillWidth().Height(subSpkMenuRowH).Padding(0, 10).Gap(9).Radius(8).AlignItems(ui.Center).
		Label(label).Disabled(disabled)
	col := subInk2
	switch {
	case disabled:
		col = ui.RGBA(255, 255, 255, 0.24)
	case b.Hovered():
		b.Background(subHover)
		col = subInk
	}
	b.Children(func() {
		if isNew {
			ui.Icon(c, icSubPlus).Size(12, 12).TextColor(col)
		} else {
			ui.Box(c.Key("dot")).Width(7).Height(7).Radius(3.5).Background(w.speakerColor(id)).Shrink(0)
		}
		ui.Text(c, name).FontSize(13).TextColor(col).SingleLine().MinWidth(0).Shrink(1)
	})
	if !b.Clicked() || disabled {
		return
	}
	switch {
	case isNew:
		// Stays open on the new speaker, so it can be named right away.
		if nid := w.createSpeaker(i, w.whoKey); nid != "" {
			w.whoSpeaker, w.whoName = nid, ""
			w.whoGen++
			w.sizeWho()
		}
	case w.whoScope == "one":
		w.assignUtt(i, w.whoKey, id)
		w.hideWho()
	default:
		w.mergeSpeaker(w.whoSpeaker, id)
		w.hideWho()
	}
}
