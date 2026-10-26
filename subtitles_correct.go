package main

import (
	"context"
	"strings"
	"time"
	"unicode"

	"voiceinput/internal/subtitles"
)

// Background correction of sherpa finals (docs/public/LIVE-SUBTITLES.md):
// internal/subtitles has Qwen recognize a final's audio again and reports a
// Correction for the final's revision. Here it replaces the entry's
// original in place, and the new text is translated in a lane of its own:
// one request at a time, never counted in or preempting the live slots.
// Whatever is shown stays until its replacement succeeds.

const (
	subCorrSlots    = 1
	subCorrDeadline = 10 * time.Second
)

// watchDictation follows the app's dictation for the window's whole life:
// each change reaches onDictation on the main thread, in order.
func (w *subtitleWindow) watchDictation() {
	for {
		busy, flip := dictationGate.State()
		w.win.Update(func() { w.onDictation(busy) })
		<-flip
	}
}

// onDictation gives dictation priority over optional work: while it is
// busy the correction lane starts nothing, and a correction translation
// running is cancelled to run again once dictation is idle. Capture, sherpa
// and live translation go on (schedule keeps one live slot).
func (w *subtitleWindow) onDictation(busy bool) {
	if w.dictating == busy {
		return
	}
	w.dictating = busy
	requeued := 0
	if busy {
		for i := range w.entries {
			e := &w.entries[i]
			if job := e.cbusy; job != nil {
				w.cancelCorr(i, "dictation")
				if e.cwant == nil {
					again := *job
					again.id, again.cancelled, again.queued = 0, false, time.Now()
					e.cwant = &again
				}
				requeued++
			}
		}
	}
	w.log.emit("dictation_priority", map[string]any{"busy": busy, "phase": dictationPhase.Load(), "session": w.sessID, "running": w.running,
		"live_active": w.active, "corr_requeued": requeued})
	w.schedule()
}

// correctedShown is an entry whose shown translation is of its corrected text.
func correctedShown(e *subtitleEntry) bool {
	return e.corrected && e.shownFinal && e.shownSrc == e.original
}

func (w *subtitleWindow) onCorrection(ev subtitles.Event) {
	now := time.Now()
	fields := map[string]any{"session": ev.Session, "segment": ev.Segment, "rev": ev.Rev, "text": ev.Text,
		"event_to_apply_ms": now.Sub(ev.At).Milliseconds()}
	if c := ev.Corr; c != nil {
		fields["job"], fields["start_s"], fields["end_s"], fields["queue_ms"], fields["infer_ms"] = c.Job, c.StartS, c.EndS, c.QueueMs, c.InferMs
		fields["final_to_apply_ms"] = now.Sub(c.FinalAt).Milliseconds()
	}
	done := func(why string) {
		fields["applied"], fields["why"] = why == "", why
		w.log.emit("correction_apply", fields)
	}
	i := w.findEntry(ev.Session, ev.Segment)
	if i < 0 {
		done("missing")
		return
	}
	e := &w.entries[i]
	fields["sherpa_text"] = e.original
	switch {
	case !e.final || e.rev != ev.Rev:
		done("revision")
		return
	case e.corrected:
		done("duplicate")
		return
	}
	if c := ev.Corr; c != nil {
		fields["lead_risk"], fields["tail_risk"], fields["preroll_s"] = c.LeadRisk, c.TailRisk, c.PrerollS
		if why := boundaryLoss(e.original, ev.Text, c.LeadRisk, c.TailRisk); why != "" {
			done(why)
			return
		}
	}
	if why := falseCorrection(e.original, ev.Text); why != "" {
		done(why)
		return
	}
	old, wasSame := e.original, e.same
	e.corrected, e.asrText, e.original = true, old, ev.Text
	kind := "changed"
	switch {
	case ev.Text == old:
		kind = "unchanged"
	case foldText(ev.Text) == foldText(old):
		kind = "punct_only"
	}
	fields["kind"] = kind
	done("")
	w.changes++
	s := w.a.settings.Get().Subtitles.Normalized()
	e.same = sameLanguage(subtitleSource(s), s.Target, e.original)
	switch {
	case e.same:
		w.dropJobs(i)
		e.translation, e.shownSrc, e.tr = "", "", trDone
	case kind != "changed" && !wasSame:
		// The translation shown or coming is of the same words.
	default:
		w.wantCorrection(i, ev)
	}
}

// boundaryLoss keeps sherpa's text when a token of it sits at the range's
// start (lead) or end (tail), where the endpoint may cut a word, and Qwen's
// text lacks that first or last character (case, spaces and punctuation
// folded). It does not repair either text; it only declines the correction.
func boundaryLoss(asr, qwen string, lead, tail bool) string {
	a, q := []rune(foldText(asr)), []rune(foldText(qwen))
	switch {
	case len(a) == 0 || len(q) == 0:
	case lead && a[0] != q[0]:
		return "boundary_prefix"
	case tail && a[len(a)-1] != q[len(q)-1]:
		return "boundary_suffix"
	}
	return ""
}

// falseCorrection is why Qwen's text is not taken for sherpa's: nothing
// recognized, or a length so different that something went grossly wrong.
// Passing it says nothing about correctness: the boundary guards and a
// per-caption review do.
func falseCorrection(asr, qwen string) string {
	a, q := runes(foldText(asr)), runes(foldText(qwen))
	switch {
	case q == 0:
		return "empty"
	case a >= 4 && (q*10 < a*4 || q*10 > a*25):
		return "length"
	}
	return ""
}

// foldText is s without case, spaces and punctuation, to compare words.
func foldText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wantCorrection queues the translation of entry i's corrected text,
// replacing what its lane had waiting or running.
func (w *subtitleWindow) wantCorrection(i int, ev subtitles.Event) {
	e := &w.entries[i]
	if e.translation == "" {
		e.tr = trPending
	}
	if w.trCtx == nil {
		if e.translation == "" {
			e.tr = trFailed
		}
		return
	}
	w.cancelCorr(i, "superseded")
	e.cwant = &subtitleJob{session: e.session, segment: e.segment, rev: e.rev, final: true, corr: true, text: e.original, target: e.target,
		reason: "correction", queued: time.Now(), timingSource: "correction", timingRev: e.rev, srcAt: ev.At,
		fedS: ev.FedS, audioS: ev.AudioS, tokenS: ev.TokenEndS}
	w.scheduleCorr()
}

// scheduleCorr starts the newest waiting correction while its lane has
// room; a cancelled request still running keeps one more from starting.
func (w *subtitleWindow) scheduleCorr() {
	for w.trCtx != nil && !w.dictating && w.corrActive < subCorrSlots && w.corrInflight <= subCorrSlots {
		next := -1
		for j := len(w.entries) - 1; j >= 0 && next < 0; j-- {
			if e := &w.entries[j]; e.cwant != nil && e.cbusy == nil {
				next = j
			}
		}
		if next < 0 {
			return
		}
		e := &w.entries[next]
		job := e.cwant
		e.cwant = nil
		w.jobSeq++
		job.id, job.gen = w.jobSeq, w.trGen
		ctx, cancel := context.WithTimeout(w.trCtx, subCorrDeadline)
		e.cbusy, e.cbusyCancel = job, cancel
		w.corrInflight++
		w.corrActive++
		job.started = time.Now()
		w.log.emit("correction_translation_start", map[string]any{"id": job.id, "gen": job.gen, "session": job.session, "segment": job.segment,
			"rev": job.rev, "target": job.target, "chars": runes(job.text), "queue_ms": job.started.Sub(job.queued).Milliseconds(),
			"lane": "correction", "corr_inflight": w.corrInflight, "live_active": w.active, "deadline_ms": subCorrDeadline.Milliseconds()})
		w.request(job, ctx, cancel)
	}
}

// cancelCorr cancels entry i's running correction translation; it keeps
// counting in corrInflight until the request returns.
func (w *subtitleWindow) cancelCorr(i int, why string) {
	e := &w.entries[i]
	job := e.cbusy
	if job == nil {
		return
	}
	job.cancelled = true
	w.corrActive--
	e.cbusyCancel()
	e.cbusy, e.cbusyCancel = nil, nil
	w.log.emit("correction_translation_cancel", map[string]any{"id": job.id, "session": e.session, "segment": e.segment, "why": why,
		"running_ms": time.Since(job.started).Milliseconds()})
}

// onCorrResult applies a correction's translation when it is still of the
// entry's text and target; on failure the shown translation stays, and only
// an entry with none offers a retry.
func (w *subtitleWindow) onCorrResult(r subtitleResult) {
	w.corrInflight--
	if !r.job.cancelled {
		w.corrActive--
	}
	defer w.schedule()
	w.textErr = r.textUnavailable
	job := r.job
	fields := map[string]any{"id": r.id, "session": r.session, "segment": r.segment, "rev": job.rev, "lane": "correction",
		"request_ms": r.ended.Sub(r.started).Milliseconds(), "queue_ms": r.started.Sub(job.queued).Milliseconds(),
		"correction_to_apply_ms": time.Since(job.srcAt).Milliseconds(), "route": r.route, "model": r.model, "host": r.host,
		"ok": r.err == nil, "error": errText(r.err), "src": job.text, "text": r.text, "target": job.target}
	i := w.findEntry(r.session, r.segment)
	if i < 0 || w.entries[i].cbusy == nil || w.entries[i].cbusy.id != r.id {
		fields["applied"], fields["why"] = false, "superseded"
		w.log.emit("correction_translation", fields)
		return
	}
	e := &w.entries[i]
	e.cbusy, e.cbusyCancel = nil, nil
	why := ""
	switch {
	case r.err != nil || r.text == "":
		why = "error"
		if e.translation == "" {
			e.tr = trFailed
		}
	case job.target != e.target:
		why = "target"
	case job.text != e.original:
		why = "stale"
	default:
		e.translation, e.shownSrc, e.shownRev, e.shownFinal, e.tr = r.text, job.text, job.rev, true, trDone
		// The live lane's translation of sherpa's text is no longer wanted.
		if e.busy != nil && e.busy.text != e.original {
			w.cancelBusy(i, "corrected", false)
		}
		if e.want != nil && e.want.text != e.original {
			e.want = nil
		}
	}
	fields["applied"], fields["why"] = why == "", why
	w.log.emit("correction_translation", fields)
	if why == "" {
		w.changes++
		w.log.emit("correction_ui_apply", map[string]any{"id": r.id, "session": r.session, "segment": r.segment, "rev": job.rev})
		w.refresh()
	}
}
