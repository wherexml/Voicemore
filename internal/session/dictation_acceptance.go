//go:build dictationacceptance

package session

import (
	"context"
	"os"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
)

// AcceptTranslationPipeline drives production model/retry/cancel/sink code
// over HTTP. Audio and cross-app insertion are deliberately outside this check.
func AcceptTranslationPipeline(base string) []map[string]any {
	rows := []map[string]any{}
	check := func(name string, ok bool) { rows = append(rows, map[string]any{"case": name, "passed": ok}) }
	dir, err := os.MkdirTemp("", "voicemore-translation-acceptance-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	settings, _ := config.Open(dir)
	dictionary, _ := dict.Open(dir)
	s := settings.Get()
	s.KeepCancelled = false
	s.TranslationLanguage = "ja"
	_ = settings.Set(s)
	newRun := func(model string) (*Controller, *run, chan string) {
		ctx, stop := context.WithCancel(context.Background())
		out := make(chan string, 4)
		c := &Controller{Settings: settings, Dict: dictionary, ResolveCleanup: func(config.Settings) (cleanup.Options, error) {
			return cleanup.Options{BaseURL: base, Model: model, DisableThinking: true}, nil
		}}
		r := &run{ctx: ctx, stop: stop, translation: "en", cleanup: true, text: "请在周四15点前修复2个问题，不要今天上线。", finalAt: time.Now(), sink: func(text string) { out <- text }}
		c.cur = r
		c.view = View{Phase: Finalizing, TranslationLanguage: r.translation}
		return c, r, out
	}
	c, r, out := newRun("fixture-success")
	c.clean(r, s)
	select {
	case text := <-out:
		check("translated result reaches sink once", text == "Fix 2 issues before 15:00 Thursday; do not deploy today.")
	default:
		check("translated result reaches sink once", false)
	}
	check("target remains frozen despite current settings Japanese", r.rec.CleanedText == "Fix 2 issues before 15:00 Thursday; do not deploy today.")
	c.clean(r, s)
	check("duplicate processing cannot insert twice", len(out) == 0)
	c, r, out = newRun("fixture-error")
	c.clean(r, s)
	v := c.View()
	check("HTTP failure retains source without inserting", v.Phase == Failed && v.CleanupFailed && v.Text == r.text && v.TranslationLanguage == "en" && len(out) == 0)
	c.ResolveCleanup = func(config.Settings) (cleanup.Options, error) {
		return cleanup.Options{BaseURL: base, Model: "fixture-success", DisableThinking: true}, nil
	}
	c.RetryCleanup()
	select {
	case text := <-out:
		check("retry retains translation task and language", text == "Fix 2 issues before 15:00 Thursday; do not deploy today.")
	case <-time.After(3 * time.Second):
		check("retry retains translation task and language", false)
	}
	c, r, out = newRun("fixture-slow")
	finished := make(chan struct{})
	go func() { c.clean(r, s); close(finished) }()
	deadline := time.Now().Add(time.Second)
	for c.View().Phase != Cleaning && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	c.Cancel()
	select {
	case <-finished:
		check("cancelled request finishes", true)
	case <-time.After(2 * time.Second):
		check("cancelled request finishes", false)
	}
	check("cancellation drops late result", len(out) == 0 && c.View().Phase == Idle)
	return rows
}
