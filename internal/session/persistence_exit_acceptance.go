//go:build integrityacceptance

package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/store"
)

// PersistenceExitAcceptance runs production persistence and cancellation with
// synthetic input. The caller supplies the real UI dispatcher and quit request.
type PersistenceExitAcceptance struct {
	c           *Controller
	r           *run
	root, fault string
	exitEntered chan struct{}
	cancelDone  chan struct{}
	persistDone chan struct{}
	exiting     atomic.Bool
	errors      atomic.Int32
	visible     atomic.Int32
}

func StartPersistenceExitAcceptance(root, fault string, onMain func(func()), quit func()) (*PersistenceExitAcceptance, error) {
	if fault != "index" && fault != "audio" && fault != "both" && fault != "success" && fault != "private" {
		return nil, fmt.Errorf("unknown fault %q", fault)
	}
	settings, err := config.Open(root)
	if err != nil {
		return nil, err
	}
	s := settings.Get()
	s.SaveAudio = true
	if fault == "private" {
		s.PrivateApps = []config.PrivateApp{{BundleID: "private.fixture"}}
	}
	if err = settings.Set(s); err != nil {
		return nil, err
	}
	history, err := store.Open(root)
	if err != nil {
		return nil, err
	}
	if err = history.Add(store.Record{ID: "existing", RawText: "SYNTHETIC_EXISTING"}); err != nil {
		return nil, err
	}
	if fault == "index" || fault == "both" {
		if err = os.Mkdir(filepath.Join(root, "history.json.bak"), 0700); err != nil {
			return nil, err
		}
	}
	if fault == "audio" || fault == "both" {
		if err = os.Mkdir(history.AudioPath("new.wav"), 0700); err != nil {
			return nil, err
		}
	}
	ctx, stop := context.WithCancel(context.Background())
	c := &Controller{Settings: settings, History: history}
	r := &run{id: "new", ctx: ctx, stop: stop, prepared: true, pcm: []byte{0, 1, 0, 2}, text: "SYNTHETIC_INSERTED", rec: store.Record{ID: "new", Status: store.StatusInserted, RawText: "SYNTHETIC_INSERTED", InsertedText: "SYNTHETIC_INSERTED"}}
	c.cur, c.view = r, View{Phase: Finalizing}
	f := &PersistenceExitAcceptance{c: c, r: r, root: root, fault: fault, exitEntered: make(chan struct{}), cancelDone: make(chan struct{}), persistDone: make(chan struct{})}
	notifying := make(chan struct{})
	var first sync.Once
	notify := func() {
		// Hold the first notification until OnWillQuit is on the main thread.
		// The old code then cycles: Cancel waits for saving; this waits for UI.
		first.Do(func() { close(notifying) })
		<-f.exitEntered
		onMain(func() {
			if !f.exiting.Load() {
				f.visible.Add(1)
			}
		})
	}
	c.OnPersistenceError = func(error) { f.errors.Add(1); notify() }
	c.OnHistory = notify
	go func() {
		defer close(f.persistDone)
		defer stop()
		c.persist(r, s, &macos.Target{BundleID: "private.fixture"})
		// The second settlement must not add another record or notification.
		c.persist(r, s, &macos.Target{BundleID: "private.fixture"})
	}()
	go func() {
		select {
		case <-notifying:
		case <-f.persistDone: // private runs deliberately produce no notification
		}
		onMain(quit)
	}()
	return f, nil
}

// WillQuit is invoked synchronously by the native App.OnWillQuit callback.
func (f *PersistenceExitAcceptance) WillQuit() {
	f.exiting.Store(true)
	close(f.exitEntered)
	f.c.Cancel()
	close(f.cancelDone)
}

// Results is called after the native event loop has shut down.
func (f *PersistenceExitAcceptance) Results() []map[string]any {
	rows := []map[string]any{}
	check := func(name string, ok bool, details ...any) {
		rows = append(rows, map[string]any{"case": name, "passed": ok, "details": details})
	}
	select {
	case <-f.persistDone:
		check("persistence and duplicate settlement return after native shutdown", true)
	case <-time.After(3 * time.Second):
		check("persistence and duplicate settlement return after native shutdown", false)
		return rows
	}
	select {
	case <-f.cancelDone:
		check("native OnWillQuit completes synchronous Cancel", true)
	default:
		check("native OnWillQuit completes synchronous Cancel", false)
	}
	expectedErrors := map[string]int32{"index": 1, "audio": 1, "both": 2, "success": 0, "private": 0}[f.fault]
	check("every persistence failure is reported", f.errors.Load() == expectedErrors, f.errors.Load(), expectedErrors)
	check("notifications do not reopen UI during quit", f.visible.Load() == 0)
	history, err := store.Open(f.root)
	check("history reopens without recovery", err == nil, fmt.Sprint(err))
	rec, saved := history.Get("new")
	expectSaved := f.fault == "audio" || f.fault == "success"
	check("settled record state agrees with reopened disk", saved == expectSaved && f.r.recSaved == expectSaved && f.r.recDone, saved, f.r.recSaved)
	if saved {
		check("exit preserves the successful insertion outcome", rec.Status == store.StatusInserted && rec.InsertedText == "SYNTHETIC_INSERTED")
		check("repeated settlement does not duplicate history", len(history.List("")) == 2)
		if f.fault == "audio" {
			check("audio failure remains recorded", rec.AudioFile == "" && rec.AudioRemoved != "")
		}
	} else {
		check("failed or private settlement preserves existing history only", len(history.List("")) == 1)
	}
	info, statErr := os.Stat(history.AudioPath("new.wav"))
	regularWAV := statErr == nil && info.Mode().IsRegular()
	check("only successfully saved ordinary history retains a WAV", regularWAV == (f.fault == "success"))
	return rows
}
