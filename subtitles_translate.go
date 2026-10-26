package main

import (
	"context"
	"strings"
	"time"
	"unicode"

	"voiceinput/internal/cleanup"
	"voiceinput/internal/subtitles"
)

// Streaming translation of live subtitles (docs/public/LIVE-SUBTITLES.md).
// A segment's partial text is translated early once a clause of it is
// stable, the same caption updates as the draft grows, and the final text
// corrects it in place. Every job carries generation, session, segment,
// revision and target, and a result applies only to the entry state it was
// made for. Each entry has at most one job waiting (a newer one replaces it)
// and one running. At most subMaxInflight run at once: live work (the
// running session's open segment and finals of the last subLiveFinal) goes
// first and may preempt history, which holds one slot at most. A draft has
// its own short deadline and is preempted by a newer draft once it is slow,
// so neither old history nor a slow request builds a FIFO backlog.
// Retranslations of Qwen-corrected finals run in a lane of their own
// (subtitles_correct.go): they never take or preempt these slots.

const (
	subMaxInflight  = 2
	subStableDelay  = 600 * time.Millisecond  // a revision unchanged this long is stable
	subDraftMaxWait = 1800 * time.Millisecond // while revisions keep coming, draft the agreed prefix at least this often
	subDraftMinGap  = 400 * time.Millisecond  // between drafts of one entry, except a stable one
	subDraftMinGrow = 4                       // runes a draft must add to the last one, unless it ends a clause

	subDraftDeadline     = 3 * time.Second        // a draft request gives up after this; finals keep subtitleTranslateT
	subDraftPreemptAfter = 1200 * time.Millisecond // a running draft this old yields to a newer draft of its entry
	subLiveFinal         = 3 * time.Second        // a final this recent is still live work
)

type subtitleJob struct {
	id      uint64
	gen     uint64
	session uint64
	segment int
	rev     int
	final   bool
	corr    bool // a corrected final, in the correction lane
	text    string
	target  string
	reason  string // final, boundary, stable, maxwait, retry, retarget, session_end, correction

	queued    time.Time // when it was wanted
	started   time.Time // when its request started
	cancelled bool      // cancelled while running; it no longer holds an active slot

	// Audio timing of the text, from the revision whose hypothesis it is
	// (timingSource "exact_revision"), else from the current hypothesis,
	// whose last token is later than the prefix: then any speech latency
	// computed from it is a lower bound ("hypothesis_lower_bound").
	timingSource string
	timingRev    int
	srcAt        time.Time // when that revision came from the engine
	fedS         float64   // audio fed to the engine by then (an upper bound of its audio end)
	audioS       float64   // the engine's own audio position, -1 when none
	tokenS       float64   // the engine's last token instant (estimated), -1 when none
}

type subtitleResult struct {
	job             *subtitleJob // owned by the main thread; the request only carries it back
	id, gen         uint64
	session         uint64
	segment         int
	text            string
	err             error
	started, ended  time.Time
	route, model    string
	host            string
	textUnavailable bool
}

func (w *subtitleWindow) startTranslator() {
	w.stopTranslator()
	w.trCtx, w.trCancel = context.WithCancel(context.Background())
	w.log.emit("translator_start", map[string]any{"gen": w.trGen, "max_inflight": subMaxInflight})
	w.schedule()
}

// stopTranslator cancels every translation of this generation; finals
// still due offer a retry, drafts already shown stay.
func (w *subtitleWindow) stopTranslator() {
	w.trGen++
	if w.trCancel != nil {
		w.trCancel()
	}
	w.trCtx, w.trCancel, w.inflight, w.active = nil, nil, 0, 0
	w.corrInflight, w.corrActive = 0, 0
	for i := range w.entries {
		e := &w.entries[i]
		e.want, e.busy, e.busyCancel = nil, nil, nil
		e.cwant, e.cbusy, e.cbusyCancel = nil, nil, nil
		if e.final && !e.same && !e.shownFinal && e.session != 0 {
			e.tr = trFailed
		}
	}
}

// dropJobs cancels what entry i has waiting or running, in both lanes.
func (w *subtitleWindow) dropJobs(i int) {
	w.cancelBusy(i, "dropped", false)
	w.cancelCorr(i, "dropped")
	w.entries[i].want, w.entries[i].cwant = nil, nil
}

// cancelBusy cancels entry i's running job; it frees its active slot now,
// while inflight keeps counting it until the request returns. requeue makes
// it wait to run again, unless a newer job already waits.
func (w *subtitleWindow) cancelBusy(i int, why string, requeue bool) {
	e := &w.entries[i]
	job := e.busy
	if job == nil {
		return
	}
	job.cancelled = true
	w.active--
	e.busyCancel()
	e.busy, e.busyCancel = nil, nil
	w.log.emit("translation_cancel", map[string]any{"id": job.id, "session": e.session, "segment": e.segment, "final": job.final,
		"why": why, "running_ms": time.Since(job.started).Milliseconds()})
	if requeue && e.want == nil {
		again := *job
		again.id, again.cancelled, again.queued = 0, false, time.Now()
		e.want = &again
	}
}

// isLive is work for what is being said now: the running session's open
// segment, or one of its finals within subLiveFinal.
func (w *subtitleWindow) isLive(i int) bool {
	e := &w.entries[i]
	return w.running && e.session == w.sessID && (!e.final || time.Since(e.finalAt) < subLiveFinal)
}

// timingFor is the audio timing of text in entry i: of the revision whose
// hypothesis is exactly text, else of the current one, as a lower bound.
func timingFor(e *subtitleEntry, text string) (subtitles.Event, string) {
	if e.ev.Text == text {
		return e.ev, "exact_revision"
	}
	for k := len(e.hyps) - 1; k >= 0; k-- {
		if e.hyps[k].Text == text {
			return e.hyps[k], "exact_revision"
		}
	}
	return e.ev, "hypothesis_lower_bound"
}

// onRevision follows a partial of entry i: it checks the draft now, and once
// more after subStableDelay, when no newer revision may have come.
func (w *subtitleWindow) onRevision(i int) {
	e := &w.entries[i]
	s := w.a.settings.Get().Subtitles.Normalized()
	if e.target != s.Target {
		e.target, e.translation, e.shownSrc, e.shownRev, e.draftSrc = s.Target, "", "", 0, ""
	}
	e.same = sameLanguage(subtitleSource(s), s.Target, e.original)
	if e.same {
		return
	}
	e.stable = commonPrefix(e.prev, e.original)
	e.prev = e.original
	w.considerDraft(i, false)
	session, segment, rev := e.session, e.segment, e.rev
	time.AfterFunc(subStableDelay, func() {
		w.win.Update(func() {
			if j := w.findEntry(session, segment); j >= 0 && w.entries[j].rev == rev && !w.entries[j].final {
				w.considerDraft(j, true)
			}
		})
	})
}

// considerDraft is the stabilization policy for unpunctuated streaming
// text: draft the clause that two revisions agree on up to its last clause
// mark; the whole text once it has held for subStableDelay; the agreed
// prefix when subDraftMaxWait passed without a draft. A draft needs a
// meaningful length and new material, so it never re-translates per character.
func (w *subtitleWindow) considerDraft(i int, settled bool) {
	e := &w.entries[i]
	if e.final || e.same || w.trCtx == nil {
		return
	}
	now := time.Now()
	cand, reason := "", ""
	if settled {
		cand, reason = e.original, "stable"
	} else {
		if b := clausePrefix(e.stable); meaningful(b) {
			cand, reason = b, "boundary"
		}
		since := e.draftAt
		if since.IsZero() {
			since = e.firstAt
		}
		if now.Sub(since) >= subDraftMaxWait && meaningful(e.stable) && runes(e.stable) > runes(cand) {
			cand, reason = e.stable, "maxwait"
		}
	}
	if !meaningful(cand) || cand == e.draftSrc {
		return
	}
	corrected := !strings.HasPrefix(cand, e.draftSrc)
	if !corrected && runes(cand)-runes(e.draftSrc) < subDraftMinGrow && !endsClause(cand) {
		return
	}
	if !settled && now.Sub(e.draftAt) < subDraftMinGap {
		return
	}
	e.draftSrc, e.draftAt = cand, now
	t, source := timingFor(e, cand)
	w.log.emit("draft_clause", map[string]any{"session": e.session, "segment": e.segment, "rev": e.rev, "reason": reason, "chars": runes(cand),
		"corrected": corrected, "src": cand, "timing_source": source, "timing_rev": t.Rev, "text_is_timed_hypothesis": source == "exact_revision",
		"fed_s": t.FedS, "engine_audio_s": t.AudioS, "token_end_s": t.TokenEndS, "hypothesis_age_ms": now.Sub(t.At).Milliseconds()})
	w.want(i, false, cand, reason)
}

// requestFinal translates the final text of entry i, unless it is in the
// target language or its shown draft is already of exactly this text.
func (w *subtitleWindow) requestFinal(i int, reason string) {
	e := &w.entries[i]
	s := w.a.settings.Get().Subtitles.Normalized()
	if e.target != s.Target {
		e.target, e.translation, e.shownSrc, e.shownRev, e.shownFinal = s.Target, "", "", 0, false
	}
	e.same = sameLanguage(subtitleSource(s), s.Target, e.original)
	switch {
	case e.same:
		w.dropJobs(i)
		e.tr, e.translation = trDone, ""
		return
	case e.translation != "" && e.shownSrc == e.original:
		w.dropJobs(i)
		e.shownFinal, e.tr = true, trDone
		w.log.emit("ui_apply", map[string]any{"session": e.session, "segment": e.segment, "rev": e.rev, "final": true, "why": "draft_equals_final"})
		return
	case w.trCtx == nil:
		e.tr = trFailed
		return
	}
	e.tr = trPending
	w.want(i, true, e.original, reason)
}

// want makes job the one entry i waits for, replacing a waiting one. A
// final makes a running draft obsolete: it is cancelled, unless it is of
// the same text and young enough to finish within its draft deadline, and
// then serves as the final. A newer draft preempts a running draft that is
// already slow; healthy drafts (well under a second) finish first.
func (w *subtitleWindow) want(i int, final bool, text, reason string) {
	e := &w.entries[i]
	if b := e.busy; b != nil && !b.final {
		age := time.Since(b.started)
		switch {
		case final && b.text == text && age < subDraftPreemptAfter:
			b.final, b.rev, e.want = true, e.rev, nil
			return
		case final:
			w.cancelBusy(i, "final_supersedes_draft", false)
		case age >= subDraftPreemptAfter:
			w.cancelBusy(i, "slow_draft_superseded", false)
		}
	}
	t, source := timingFor(e, text)
	e.want = &subtitleJob{session: e.session, segment: e.segment, rev: e.rev, final: final, text: text, target: e.target, reason: reason,
		queued: time.Now(), timingSource: source, timingRev: t.Rev, srcAt: t.At, fedS: t.FedS, audioS: t.AudioS, tokenS: t.TokenEndS}
	w.schedule()
}

// schedule starts waiting jobs while there is room: live work first,
// newest first. History holds one active slot at most; when live work finds
// every slot taken, it preempts the oldest running history job, which waits
// to run again. Cancelled requests still running count against a hard cap
// of subMaxInflight+1 until they return.
func (w *subtitleWindow) schedule() {
	defer w.scheduleCorr()
	// While dictation is busy its cleanup may share the text service: live
	// work keeps one slot and history waits; nothing running is cancelled.
	slots := subMaxInflight
	if w.dictating {
		slots = 1
	}
	for w.trCtx != nil && w.inflight <= subMaxInflight {
		next, live := -1, false
		for j := len(w.entries) - 1; j >= 0 && next < 0; j-- {
			if e := &w.entries[j]; e.want != nil && e.busy == nil && w.isLive(j) {
				next, live = j, true
			}
		}
		history := 0
		for j := range w.entries {
			if w.entries[j].busy != nil && !w.isLive(j) {
				history++
			}
		}
		for j := len(w.entries) - 1; j >= 0 && next < 0 && history == 0 && !w.dictating; j-- {
			if e := &w.entries[j]; e.want != nil && e.busy == nil {
				next = j
			}
		}
		if next < 0 {
			return
		}
		if w.active >= slots {
			victim := -1
			for j := range w.entries {
				if live && w.entries[j].busy != nil && !w.isLive(j) {
					victim = j
					break
				}
			}
			if victim < 0 {
				return
			}
			w.cancelBusy(victim, "preempted_by_live", true)
		}
		w.launch(next)
	}
}

func (w *subtitleWindow) launch(i int) {
	e := &w.entries[i]
	job := e.want
	e.want = nil
	w.jobSeq++
	job.id, job.gen = w.jobSeq, w.trGen
	deadline := subtitleTranslateT
	if !job.final {
		deadline = subDraftDeadline
	}
	ctx, cancel := context.WithTimeout(w.trCtx, deadline)
	e.busy, e.busyCancel = job, cancel
	w.inflight++
	w.active++
	job.started = time.Now()
	w.log.emit("translation_start", map[string]any{"id": job.id, "gen": job.gen, "session": job.session, "segment": job.segment, "rev": job.rev,
		"final": job.final, "reason": job.reason, "target": job.target, "chars": runes(job.text), "queue_ms": job.started.Sub(job.queued).Milliseconds(),
		"inflight": w.inflight, "active": w.active, "live": w.isLive(i), "deadline_ms": deadline.Milliseconds(), "lane": "live"})
	w.request(job, ctx, cancel)
}

// request translates job's text and hands the result to onResult on the
// main thread.
func (w *subtitleWindow) request(job *subtitleJob, ctx context.Context, cancel context.CancelFunc) {
	r := subtitleResult{job: job, id: job.id, gen: job.gen, session: job.session, segment: job.segment, started: job.started}
	text, target := job.text, job.target
	go func() {
		opts, route, optErr := w.a.subtitleTextOptions()
		res, err := "", optErr
		if err == nil {
			res, err = cleanup.Translate(ctx, opts, text, subtitleTargetName(target))
		}
		cancel()
		r.text, r.err, r.ended = strings.TrimSpace(res), err, time.Now()
		r.route, r.model, r.host, r.textUnavailable = route, opts.Model, hostOf(opts.BaseURL), optErr != nil
		w.win.Update(func() { w.onResult(r) })
	}()
}

// onResult applies a translation when it is still the entry's: a final of
// the final text, or a draft newer than the shown one whose source the
// current text still begins with. Anything else is stale and dropped.
func (w *subtitleWindow) onResult(r subtitleResult) {
	if r.gen != w.trGen {
		return // stopTranslator reset inflight with the generation
	}
	if r.job.corr {
		w.onCorrResult(r)
		return
	}
	w.inflight--
	if !r.job.cancelled {
		w.active--
	}
	defer w.schedule()
	w.textErr = r.textUnavailable
	applied := time.Now()
	fields := map[string]any{"id": r.id, "session": r.session, "segment": r.segment, "request_ms": r.ended.Sub(r.started).Milliseconds(),
		"route": r.route, "model": r.model, "host": r.host, "ok": r.err == nil, "error": errText(r.err), "text": r.text}
	i := w.findEntry(r.session, r.segment)
	if i < 0 || w.entries[i].busy == nil || w.entries[i].busy.id != r.id {
		fields["applied"], fields["why"] = false, "superseded"
		w.log.emit("translation", fields)
		return
	}
	e := &w.entries[i]
	job := e.busy
	e.busy, e.busyCancel = nil, nil
	why := ""
	switch {
	case r.err != nil || r.text == "":
		why = "error"
		if job.final && !correctedShown(e) {
			e.tr = trFailed
		}
	case job.target != e.target:
		why = "target"
	case job.final && correctedShown(e) && job.text != e.original:
		why = "corrected" // the corrected text's translation is already shown
	case job.final && (job.text == e.original || e.corrected && job.text == e.asrText):
		// Before its correction is translated, sherpa's final text still serves.
		e.translation, e.shownSrc, e.shownRev, e.shownFinal, e.tr = r.text, job.text, job.rev, true, trDone
	case !job.final && !e.shownFinal && job.rev > e.shownRev && strings.HasPrefix(e.original, job.text):
		e.translation, e.shownSrc, e.shownRev = r.text, job.text, job.rev
	default:
		why = "stale"
	}
	for k, v := range map[string]any{"rev": job.rev, "entry_rev": e.rev, "final": job.final, "reason": job.reason, "target": job.target, "src": job.text,
		"applied": why == "", "why": why, "timing_source": job.timingSource, "timing_rev": job.timingRev,
		"text_is_timed_hypothesis": job.timingSource == "exact_revision",
		"fed_s": job.fedS, "engine_audio_s": job.audioS, "token_end_s": job.tokenS,
		"queue_ms": r.started.Sub(job.queued).Milliseconds(), "revision_to_apply_ms": applied.Sub(job.srcAt).Milliseconds()} {
		fields[k] = v
	}
	w.log.emit("translation", fields)
	if why == "" {
		w.changes++
		w.log.emit("ui_apply", map[string]any{"id": r.id, "session": r.session, "segment": r.segment, "rev": job.rev, "final": job.final,
			"note": "state applied on the main thread; drawn on the next frame"})
		w.refresh()
	}
}

// retarget translates the history again for a new target: finals through
// the scheduler (newest first, one history slot), the live entry from its
// current text. Without a translator each final offers a retry.
func (w *subtitleWindow) retarget(target string) {
	for i := range w.entries {
		e := &w.entries[i]
		if e.session == 0 || e.target == target {
			continue
		}
		w.dropJobs(i)
		w.changes++
		e.target, e.translation, e.shownSrc, e.shownRev, e.shownFinal, e.draftSrc = target, "", "", 0, false, ""
		if e.final {
			w.requestFinal(i, "retarget")
		} else {
			s := w.a.settings.Get().Subtitles.Normalized()
			e.same = sameLanguage(subtitleSource(s), target, e.original)
			w.considerDraft(i, true)
		}
	}
}

func (w *subtitleWindow) retry(i int) {
	if w.trCtx == nil {
		w.startTranslator()
	}
	w.requestFinal(i, "retry")
}

func runes(s string) int { return len([]rune(s)) }

// commonPrefix is the longest rune prefix of a and b.
func commonPrefix(a, b string) string {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return string(ra[:n])
}

func isClauseMark(r rune) bool { return strings.ContainsRune("，。！？；：、,.!?;:", r) }

func endsClause(s string) bool {
	r := []rune(strings.TrimSpace(s))
	return len(r) > 0 && isClauseMark(r[len(r)-1])
}

// clausePrefix is s up to and including its last clause mark, or "".
func clausePrefix(s string) string {
	r := []rune(s)
	for i := len(r) - 1; i >= 0; i-- {
		if isClauseMark(r[i]) {
			return string(r[:i+1])
		}
	}
	return ""
}

// meaningful is enough text to translate on its own: four Han characters or
// three words, so that an interjection waits for more or for its final.
func meaningful(s string) bool {
	han := 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	return han >= 4 || len(strings.Fields(s)) >= 3
}
