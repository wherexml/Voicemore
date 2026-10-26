package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"voiceinput/internal/subtitles"
)

// The explicit save of a subtitle session (docs/public/LIVE-SUBTITLES.md):
// one slot, written only by Save (or Save in the new-session prompt), as a
// whole file replaced atomically. It holds text, translations, targets,
// time ranges, token text and times, speakers' names and merges, automatic
// labels and manual choices; never audio or voice embeddings. A file that
// is not exactly this shape within its bounds is rejected whole and left
// as it is until the next save.

const (
	subStoreVersion   = 1
	subStoreMaxBytes  = 8 << 20
	subStoreMaxEntry  = 5000
	subStoreMaxText   = 4000 // runes of an original or translation
	subStoreMaxTokens = 4000
	subStoreMaxUtts   = 64
	subStoreMaxSpk    = 64
)

var subSpeakerID = regexp.MustCompile(`^[am][0-9]{1,4}$`)

type savedSession struct {
	V        int                       `json:"v"`
	ID       string                    `json:"id"`
	SavedAt  int64                     `json:"savedAt"`
	Speakers map[string]speakerProfile `json:"speakers"`
	Entries  []savedEntry              `json:"entries"`
}

type savedEntry struct {
	Session     uint64       `json:"session"`
	Segment     int          `json:"segment"`
	Original    string       `json:"original"`
	ASR         string       `json:"asr,omitempty"` // the recognizer's text, when Qwen corrected it
	Translation string       `json:"translation,omitempty"`
	Target      string       `json:"target"`
	Same        bool         `json:"same,omitempty"`
	Start       int64        `json:"start"`
	End         int64        `json:"end"`
	At          int64        `json:"at,omitempty"` // when it was final, Unix ms
	Tokens      []savedToken `json:"tokens,omitempty"`
	Utts        []savedUtt   `json:"utts,omitempty"`
}

type savedToken struct {
	T string `json:"t"`
	S int64  `json:"s"`
}

type savedUtt struct {
	K      int    `json:"k"`
	A      int    `json:"a"`
	B      int    `json:"b"`
	S      int64  `json:"s"`
	E      int64  `json:"e"`
	Auto   string `json:"auto"`
	Manual string `json:"manual,omitempty"`
}

// subtitleStorePath is the slot: the fixture's own file, so the sample
// never replaces a real session. VOICE_INPUT_SUBTITLES_DATA isolates it
// for Dev acceptance.
func subtitleStorePath(fixture bool) string {
	dir := devPath(subtitlesDataEnv)
	if dir == "" {
		data, _ := mygo.App.Path(mygo.PathUserData)
		dir = filepath.Join(data, "subtitles", "sessions")
	}
	name := "current.json"
	if fixture {
		name = "fixture.json"
	}
	return filepath.Join(dir, name)
}

func newSessionID() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 36) + "-" + strconv.FormatUint(uint64(time.Now().UnixNano()%1e6), 36)
}

// dirty is unsaved content: anything since the last save, or a session
// never saved that has entries.
func (w *subtitleWindow) dirty() bool {
	return w.changes != w.savedChanges && len(w.entries) > 0
}

// snapshot is the session as saved; made on the main thread.
func (w *subtitleWindow) snapshot() savedSession {
	s := savedSession{V: subStoreVersion, ID: w.storeID, SavedAt: time.Now().UnixMilli(), Speakers: map[string]speakerProfile{}}
	target := w.a.settings.Get().Subtitles.Normalized().Target
	for id, p := range w.spk.profiles {
		s.Speakers[id] = *p
	}
	for _, e := range w.entries {
		if e.original == "" {
			continue
		}
		se := savedEntry{Session: e.session, Segment: e.segment, Original: e.original, Target: e.target, Same: e.same,
			Start: e.start, End: e.end}
		if se.Target == "" {
			se.Target = target // the fixture's entries, which never set one
		}
		if e.corrected {
			se.ASR = e.asrText
		}
		// Only a translation of the text as it is now; a draft of an earlier revision is not kept.
		if e.translation != "" && e.shownFinal {
			se.Translation = e.translation
		}
		if !e.finalAt.IsZero() {
			se.At = e.finalAt.UnixMilli()
		}
		for _, t := range e.tokens {
			se.Tokens = append(se.Tokens, savedToken{T: t.Text, S: t.Sample})
		}
		for _, u := range e.utts {
			se.Utts = append(se.Utts, savedUtt{K: u.key, A: u.a, B: u.b, S: u.start, E: u.end, Auto: u.auto, Manual: u.manual})
		}
		s.Entries = append(s.Entries, se)
	}
	return s
}

// save writes the session off the main thread. done, when the session is
// still the one saved, gets whether the write succeeded with nothing
// changed since the snapshot. Content changed meanwhile stays unsaved; a
// save that finishes after New session touches nothing of the new one.
func (w *subtitleWindow) save(done func(complete bool)) {
	if w.saving {
		return
	}
	snap := w.snapshot()
	seq, gen := w.changes, w.storeGen
	path := subtitleStorePath(w.fixture)
	w.saving = true
	go func() {
		started := time.Now()
		n, err := writeSubtitleStore(path, snap, w.fixture)
		w.win.Update(func() {
			w.saving = false
			current := gen == w.storeGen
			w.log.emit("session_save", map[string]any{"ok": err == nil, "error": errText(err), "bytes": n, "entries": len(snap.Entries),
				"speakers": len(snap.Speakers), "ms": time.Since(started).Milliseconds(), "current": current, "changed_since": w.changes != seq})
			if !current {
				return
			}
			w.saveFailed = err != nil
			if err == nil {
				w.savedChanges = seq
			}
			w.refresh()
			if done != nil {
				done(err == nil && w.changes == seq)
			}
		})
	}()
}

// saveAndNew freezes the session (capture, recognition, translation and
// speaker labels stop, nothing late applies), saves all of it, and starts
// a new session only when that exact content was written. Otherwise the
// frozen session stays, paused, with its state shown.
func (w *subtitleWindow) saveAndNew() {
	if w.saving {
		return
	}
	w.stopFeed()
	w.stopLive()
	w.paused = true
	w.save(func(complete bool) {
		if complete {
			w.newSession()
		}
	})
}

// writeSubtitleStore replaces the slot with a complete file or not at all.
func writeSubtitleStore(path string, s savedSession, fixture bool) (int, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return 0, err
	}
	if len(b) > subStoreMaxBytes || len(s.Entries) > subStoreMaxEntry {
		return len(b), errors.New("session too large to save")
	}
	if _, err := parseSubtitleStore(b, fixture); err != nil {
		return len(b), fmt.Errorf("would not read back: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return len(b), err
	}
	f, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return len(b), err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return len(b), err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return len(b), err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return len(b), err
	}
	if err := f.Close(); err != nil {
		return len(b), err
	}
	if err := os.Rename(tmp, path); err != nil {
		return len(b), err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return len(b), nil
}

// parseSubtitleStore accepts only this version's shape within its bounds.
func parseSubtitleStore(b []byte, fixture bool) (savedSession, error) {
	var s savedSession
	if len(b) == 0 || len(b) > subStoreMaxBytes {
		return s, errors.New("size")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, fmt.Errorf("json: %w", err)
	}
	var more json.RawMessage
	if err := d.Decode(&more); err != io.EOF {
		return s, errors.New("trailing data")
	}
	switch {
	case s.V != subStoreVersion:
		return s, errors.New("version")
	case s.ID == "" || len(s.ID) > 64:
		return s, errors.New("id")
	case len(s.Entries) > subStoreMaxEntry:
		return s, errors.New("entries")
	case len(s.Speakers) > subStoreMaxSpk:
		return s, errors.New("speakers")
	}
	for id, p := range s.Speakers {
		if !subSpeakerID.MatchString(id) || p.N < 1 || p.N > 999 || utf8.RuneCountInString(p.Name) > subSpkNameMax ||
			p.Name != strings.TrimSpace(p.Name) || !utf8.ValidString(p.Name) {
			return s, fmt.Errorf("speaker %q", id)
		}
		if p.Into != "" && (p.Into == id || !hasSpeaker(s.Speakers, p.Into)) {
			return s, fmt.Errorf("merge of %q", id)
		}
	}
	for id := range s.Speakers {
		at, steps := id, 0
		for s.Speakers[at].Into != "" {
			at = s.Speakers[at].Into
			steps++
			if steps > len(s.Speakers) {
				return s, errors.New("merge cycle")
			}
		}
	}
	targets := map[string]bool{}
	for _, t := range subtitleTargets {
		targets[t.id] = true
	}
	seen := map[[2]int64]bool{}
	for k, e := range s.Entries {
		key := [2]int64{int64(e.Session), int64(e.Segment)}
		switch {
		case seen[key]:
			return s, fmt.Errorf("entry %d duplicate", k)
		case fixture != (e.Session == 0) || e.Segment < 0:
			return s, fmt.Errorf("entry %d session", k)
		case e.Original == "" || !textOK(e.Original) || !textOK(e.Translation) || !textOK(e.ASR):
			return s, fmt.Errorf("entry %d text", k)
		case !targets[e.Target]:
			return s, fmt.Errorf("entry %d target", k)
		case len(e.Tokens) > subStoreMaxTokens || len(e.Utts) > subStoreMaxUtts:
			return s, fmt.Errorf("entry %d bounds", k)
		case e.Start < -1 || e.End < -1 || (e.Start >= 0 && e.End < e.Start):
			return s, fmt.Errorf("entry %d range", k)
		}
		seen[key] = true
		if err := validTiming(e); err != nil {
			return s, fmt.Errorf("entry %d %v", k, err)
		}
		for _, u := range e.Utts {
			if !(speakerSpecial(u.Auto) || hasSpeaker(s.Speakers, u.Auto)) || u.Manual != "" && !hasSpeaker(s.Speakers, u.Manual) {
				return s, fmt.Errorf("entry %d utterance speaker", k)
			}
		}
	}
	return s, nil
}

// validTiming checks an entry's audio, tokens and utterances as alignment
// makes them: tokens in order within the range (with the recognizer's
// clamping slack), utterances covering the tokens in order without gaps,
// each within the range; without a range, no tokens and one utterance at
// most, without audio.
func validTiming(e savedEntry) error {
	const slack = subSampleRate / 2
	if e.Start < 0 {
		if e.Start != -1 || e.End != -1 || len(e.Tokens) > 0 || len(e.Utts) > 1 {
			return errors.New("range")
		}
		for _, u := range e.Utts {
			if u.K != 0 || u.A != 0 || u.B != 0 || u.S != -1 || u.E != -1 {
				return errors.New("utterance")
			}
		}
		return nil
	}
	if e.End < e.Start {
		return errors.New("range")
	}
	prev := e.Start - slack
	for _, t := range e.Tokens {
		if t.T == "" || len(t.T) > 64 || !utf8.ValidString(t.T) || t.S < prev || t.S > e.End+slack {
			return errors.New("token")
		}
		prev = t.S
	}
	if len(e.Utts) == 0 {
		return nil
	}
	if len(e.Tokens) == 0 {
		u := e.Utts[0]
		if len(e.Utts) != 1 || u.K != 0 || u.A != 0 || u.B != 0 || u.S != e.Start || u.E != e.End {
			return errors.New("utterance")
		}
		return nil
	}
	at, from := 0, e.Start
	for k, u := range e.Utts {
		last := k == len(e.Utts)-1
		ok := u.K == u.A && u.A == at && u.B > u.A && u.B <= len(e.Tokens) && u.S == from && u.E >= u.S && u.E <= e.End
		ok = ok && (!last || u.B == len(e.Tokens) && u.E == e.End)
		if !ok {
			return errors.New("utterance")
		}
		at, from = u.B, u.E
	}
	return nil
}

// readBounded reads a regular file of at most limit bytes, never more:
// a larger one is rejected without reading it.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("size")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("size")
	}
	return b, err
}

func hasSpeaker(m map[string]speakerProfile, id string) bool {
	_, ok := m[id]
	return ok
}

func textOK(s string) bool { return utf8.ValidString(s) && utf8.RuneCountInString(s) <= subStoreMaxText }

// restore loads the saved slot into a new window. A missing slot is an
// empty session; a rejected one is logged, left untouched, and the window
// starts empty. A restored session opens paused: nothing captures until
// the user resumes.
func (w *subtitleWindow) restore() {
	path := subtitleStorePath(w.fixture)
	b, err := readBounded(path, subStoreMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var s savedSession
	if err == nil {
		s, err = parseSubtitleStore(b, w.fixture)
	}
	if err != nil {
		w.log.emit("session_restore", map[string]any{"ok": false, "why": err.Error(), "bytes": len(b)})
		return
	}
	maxAuto := 0
	for id, p := range s.Speakers {
		p := p
		w.spk.profiles[id] = &p
		if strings.HasPrefix(id, "a") {
			n, _ := strconv.Atoi(id[1:])
			maxAuto = max(maxAuto, n)
		}
	}
	// Restored speakers are names only: no voice of them is known, so new
	// audio never matches them.
	w.spk.tracker.Reset(maxAuto + 1)
	for _, se := range s.Entries {
		e := subtitleEntry{session: se.Session, segment: se.Segment, rev: 1, original: se.Original, final: true, same: se.Same,
			target: se.Target, start: se.Start, end: se.End, spkDone: true, corrected: se.ASR != "", asrText: se.ASR}
		if se.At > 0 {
			e.finalAt = time.UnixMilli(se.At)
		}
		switch {
		case se.Same:
			e.tr = trDone
		case se.Translation != "":
			e.translation, e.shownSrc, e.shownRev, e.shownFinal, e.tr = se.Translation, se.Original, 1, true, trDone
		default:
			e.tr = trFailed
		}
		for _, t := range se.Tokens {
			e.tokens = append(e.tokens, subtitles.Token{Text: t.T, Sample: t.S})
		}
		for _, u := range se.Utts {
			e.utts = append(e.utts, subUtt{key: u.K, a: u.A, b: u.B, start: u.S, end: u.E, auto: u.Auto, manual: u.Manual})
		}
		w.entries = append(w.entries, e)
		w.sessID = max(w.sessID, se.Session)
	}
	w.storeID = s.ID
	w.paused = len(w.entries) > 0
	w.restored = w.paused
	w.log.emit("session_restore", map[string]any{"ok": true, "entries": len(w.entries), "speakers": len(s.Speakers), "bytes": len(b)})
}
