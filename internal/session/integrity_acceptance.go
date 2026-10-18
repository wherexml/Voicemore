//go:build integrityacceptance

package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/macos"
	"voiceinput/internal/store"
)

// AcceptIntegrityPipeline injects an already recognized synthetic transcript,
// then uses production HTTP, retry/original, history and persistence code. The
// sink is a setup/practice sink: it never pastes or sends to another app.
func AcceptIntegrityPipeline(base, root string) []map[string]any {
	rows := []map[string]any{}
	check := func(name string, ok bool, details ...any) {
		rows = append(rows, map[string]any{"case": name, "passed": ok, "details": details})
	}
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	settings, err := config.Open(root)
	must(err)
	s := settings.Get()
	s.SaveAudio = false
	s.AutoSend.Enabled = true
	must(settings.Set(s))
	dictionary, err := dict.Open(root)
	must(err)
	history, err := store.Open(root)
	must(err)
	const source = "请保留完整原文、否定和末尾条件，不要发布。"
	newRun := func(model, translation string) (*Controller, *run, chan string) {
		ctx, cancel := context.WithCancel(context.Background())
		out := make(chan string, 4)
		c := &Controller{Settings: settings, Dict: dictionary, History: history, ResolveCleanup: func(config.Settings) (cleanup.Options, error) {
			return cleanup.Options{BaseURL: base, Model: model}, nil
		}}
		r := &run{id: store.NewID(), ctx: ctx, stop: cancel, cleanup: true, translation: translation, text: source, finalAt: time.Now(), sink: func(text string) { out <- text }, rec: store.Record{RawText: source}}
		c.cur, c.view = r, View{Phase: Finalizing}
		return c, r, out
	}
	for _, translation := range []string{"", "en"} {
		c, r, out := newRun("length", translation)
		c.clean(r, s)
		v := c.View()
		check("truncated cleanup retains original and never inserts/sends "+translation, v.Phase == Failed && v.CleanupFailed && v.Text == source && len(out) == 0 && !r.inserted && r.rec.CleanedText == "" && r.rec.SendShortcut == "")
		c.ResolveCleanup = func(config.Settings) (cleanup.Options, error) {
			return cleanup.Options{BaseURL: base, Model: "stop"}, nil
		}
		c.RetryCleanup()
		select {
		case got := <-out:
			check("explicit retry inserts complete result once "+translation, got == "complete")
		case <-time.After(3 * time.Second):
			check("explicit retry inserts complete result once "+translation, false)
		}
		c.RetryCleanup()
		check("second retry does not duplicate insertion "+translation, len(out) == 0)
	}
	c, r, out := newRun("length", "")
	c.clean(r, s)
	c.InsertOriginal()
	select {
	case got := <-out:
		check("use-original keeps every clause after truncation", got == source)
	case <-time.After(3 * time.Second):
		check("use-original keeps every clause after truncation", false)
	}
	c, r, out = newRun("oversize", "")
	c.clean(r, s)
	v := c.View()
	check("oversize response offers original and retry without output", v.CleanupFailed && v.Text == source && len(out) == 0 && !r.inserted)

	must(history.Add(store.Record{ID: "historical", RawText: source}))
	before, err := os.ReadFile(filepath.Join(root, "history.json"))
	must(err)
	c.ResolveCleanup = func(config.Settings) (cleanup.Options, error) {
		return cleanup.Options{BaseURL: base, Model: "length"}, nil
	}
	err = c.Recleanup("historical", source)
	after, readErr := os.ReadFile(filepath.Join(root, "history.json"))
	must(readErr)
	old, _ := history.Get("historical")
	check("historical truncation does not append a partial version", err != nil && string(before) == string(after) && len(old.Versions) == 0 && old.RawText == source, fmt.Sprint(err))
	c.ResolveCleanup = func(config.Settings) (cleanup.Options, error) {
		return cleanup.Options{BaseURL: base, Model: "stop"}, nil
	}
	must(c.Recleanup("historical", source))
	reopened, err := store.Open(root)
	must(err)
	old, _ = reopened.Get("historical")
	check("successful historical cleanup persists a complete version", len(old.Versions) == 1 && old.Versions[0].Text == "complete" && old.RawText == source)

	// A completed insertion plus a disk failure must not be mislabeled as an
	// insertion failure. Feed the persisted outcome directly; no native paste.
	c, r, _ = newRun("stop", "")
	r.sink = nil // this case injects a completed ordinary insertion, not practice
	r.pcm = []byte{0, 1, 0, 2}
	s.SaveAudio = true
	r.prepared = true
	r.rec.ID = r.id
	r.rec.Status = store.StatusInserted
	r.rec.InsertedText = source
	problems := make(chan error, 8)
	c.OnPersistenceError = func(err error) { problems <- err }
	backup := filepath.Join(root, "history.json.bak")
	must(os.Remove(backup))
	must(os.Mkdir(backup, 0700))
	c.persist(r, s, &macos.Target{BundleID: "synthetic.fixture"})
	c.done(r, "")
	v = c.View()
	check("history save failure is visible and retains already-inserted text", len(problems) > 0 && !r.recSaved && r.rec.Status == store.StatusInserted && v.Retained && v.Text == source && strings.Contains(v.Message, "History could not be saved"))
	_, audioErr := os.Stat(history.AudioPath(r.id + ".wav"))
	check("history save failure removes only its newly orphaned audio", os.IsNotExist(audioErr))
	copied := make(chan string, 1)
	c.CopyText = func(s string) { copied <- s }
	c.CopyFailed()
	select {
	case got := <-copied:
		check("unsaved text remains copyable", got == source)
	default:
		check("unsaved text remains copyable", false)
	}
	// History actions must return save errors rather than reporting success.
	err = c.Recleanup("historical", source)
	old, _ = history.Get("historical")
	check("historical save error propagates without an in-memory version", err != nil && len(old.Versions) == 1)
	must(os.Remove(backup))
	// The observer may update history after Done has already armed its timer.
	// A real save failure must keep the notice beyond that old timer.
	c, r, _ = newRun("stop", "")
	r.sink = nil
	r.id, r.rec.ID, r.rec.Status, r.rec.InsertedText = "historical", "historical", store.StatusInserted, source
	r.recDone, r.recSaved = true, true
	must(os.Mkdir(backup, 0700))
	c.done(r, "")
	c.note(r, func(rec *store.Record) { rec.ObserveNote = "late update" })
	time.Sleep(1100 * time.Millisecond)
	v = c.View()
	check("late history failure survives the previous Done timer", v.Phase == Failed && v.Retained && v.Text == source)
	must(os.Remove(backup))
	return rows
}
