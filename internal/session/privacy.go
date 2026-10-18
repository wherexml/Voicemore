package session

import (
	"fmt"
	"os"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/store"
)

// Private apps (config.PrivateApps): a dictation whose start app or actual
// insertion app is listed still inserts as usual, but nothing of it is
// written to the history, as text or audio, and its edits are never
// observed. The record is not saved and deleted later: it stays in memory
// until the run is settled, and only the overlay holds the text meanwhile,
// for copying or retrying. Older records are never touched.

// markPrivate makes r private when bundle is a private app, in the
// settings the dictation started with or in the current ones, and reports
// whether r is private. Once private, a run stays private.
func (c *Controller) markPrivate(r *run, s config.Settings, bundle string) bool {
	p := c.Settings.LoadError() != nil || s.PrivateBundle(bundle) || c.Settings.Get().PrivateBundle(bundle)
	c.mu.Lock()
	defer c.mu.Unlock()
	if p {
		r.private = true
	}
	return r.private
}

// note changes the run's record: in memory until it is persisted, then in
// the history. The record of a private run never leaves memory.
func (c *Controller) note(r *run, fn func(rec *store.Record)) {
	c.mu.Lock()
	if !r.recDone {
		fn(&r.rec)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	r.saving.Lock() // a write in progress finishes first
	r.saving.Unlock()
	c.mu.Lock()
	saved := r.recSaved
	c.mu.Unlock()
	if saved {
		c.update(r, fn)
	}
}

// persist settles the record finish prepared: written with its recording,
// as the settings keep audio, or dropped when the run is private. end is
// the app the text went to; nil when it is not known (nothing inserted, or
// cancelled while inserting), and then the app focused now decides. Only
// the first call does anything. It may wait for the main thread: callers
// on the main thread run it in a goroutine.
func (c *Controller) persist(r *run, s config.Settings, end *macos.Target) {
	var problems []error
	notifyHistory := false
	r.saving.Lock()
	defer func() {
		// UI callbacks can synchronously wait for the main thread, whose quit
		// handler calls Cancel -> note. Publish the settled record and release
		// its saving lock before any callback can wait for that thread.
		r.saving.Unlock()
		for _, err := range problems {
			c.historyFailure(r, err)
		}
		if notifyHistory {
			c.historyChanged()
		}
	}()
	c.mu.Lock()
	open := r.prepared && !r.recDone
	c.mu.Unlock()
	if !open {
		return
	}
	bundle := ""
	if end != nil {
		bundle = end.BundleID
	} else {
		bundle = focusedBundle()
	}
	private := c.markPrivate(r, s, bundle)
	c.mu.Lock()
	r.recDone = true
	rec, pcm := r.rec, r.pcm
	c.mu.Unlock()
	if private {
		return
	}
	if s.SaveAudio && len(pcm) > 0 {
		if werr := c.History.WriteWAV(r.id+".wav", pcm); config.JSONCommitted(werr) {
			rec.AudioFile = r.id + ".wav"
			if werr != nil {
				problems = append(problems, werr)
			}
		} else {
			rec.AudioRemoved = c.tr("detailAudio", werr.Error())
			problems = append(problems, werr)
		}
	}
	err := c.History.Add(rec)
	c.mu.Lock()
	r.recSaved = config.JSONCommitted(err)
	r.rec = rec
	c.mu.Unlock()
	if err != nil {
		if !config.JSONCommitted(err) && rec.AudioFile != "" {
			_ = os.Remove(c.History.AudioPath(rec.AudioFile))
		}
		problems = append(problems, err)
	}
	notifyHistory = true
}

// keepCancelled keeps a dictation cancelled before its transcript, as the
// settings ask, unless it is private.
func (c *Controller) keepCancelled(r *run, s config.Settings, pcm []byte) {
	if c.markPrivate(r, s, focusedBundle()) {
		return
	}
	rec := store.Record{ID: r.id, StartedAt: r.started, DurationMs: pcmMs(pcm), Status: store.StatusCancelled, StatusDetail: c.tr("detailCancelled")}
	if providers.ManagedVoice(s) {
		rec.VoiceTaskID = r.rec.VoiceTaskID
	}
	if s.SaveAudio {
		if err := c.History.WriteWAV(r.id+".wav", pcm); config.JSONCommitted(err) {
			rec.AudioFile = r.id + ".wav"
			if err != nil {
				c.historyFailure(r, err)
			}
		} else {
			c.historyFailure(r, err)
		}
	}
	if err := c.History.Add(rec); err != nil {
		if !config.JSONCommitted(err) && rec.AudioFile != "" {
			_ = os.Remove(c.History.AudioPath(rec.AudioFile))
		}
		c.historyFailure(r, err)
	}
	c.historyChanged()
}

func (c *Controller) historyFailure(r *run, err error) {
	c.mu.Lock()
	r.saveErr = err
	if c.cur == r && c.view.Phase == Done {
		c.retainUnsaved(r)
	}
	c.mu.Unlock()
	c.changed()
	if c.OnPersistenceError != nil {
		c.OnPersistenceError(err)
	}
}

// retainUnsaved is called under c.mu. Text may have reached its destination
// even if saving failed; the insertion outcome in r.rec remains unchanged.
func (c *Controller) retainUnsaved(r *run) {
	text := r.rec.InsertedText
	if text == "" {
		text = r.rec.CleanedText
	}
	if text == "" {
		text = r.text
	}
	if text == "" {
		text = r.rec.Transcript()
	}
	msg := c.tr("historySaveFailed", fmt.Sprint(r.saveErr))
	c.view = View{Phase: Failed, Retained: text != "", Text: text, Message: msg, Notice: msg}
}

// focusedBundle is the Bundle ID of the frontmost app, or empty.
func focusedBundle() string {
	t := macos.CaptureTarget()
	defer t.Release()
	return t.BundleID
}
