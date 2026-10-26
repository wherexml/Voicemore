//go:build officialtranslationacceptance

package session

import (
	"context"
	"os"
	"strings"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/store"
)

// AcceptOfficialTranslation drives the production adapter, controller delivery
// and disk history against an isolated local service. No microphone or paste.
func AcceptOfficialTranslation(base string) []map[string]any {
	rows := []map[string]any{}
	check := func(n string, ok bool) { rows = append(rows, map[string]any{"case": n, "passed": ok}) }
	dir, e := os.MkdirTemp("", "official-translation-controller-")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(dir)
	settings, _ := config.Open(dir)
	history, _ := store.Open(dir)
	dictionary, _ := dict.Open(dir)
	s := settings.Get()
	s.SaveAudio = true
	s.ObserveEdits = false
	s.CleanupDefault = false
	s.ASRAssign = config.Assignment{Provider: "sub2api", Model: cloudasr.Sub2APIModel}
	s.TranslationLanguage = "ja"
	if e = settings.Set(s); e != nil {
		panic(e)
	}
	c := &Controller{Settings: settings, History: history, Dict: dictionary, ResolveASR: func(config.Settings) (asr.Dialer, error) {
		return cloudasr.Sub2API{Base: base, APIKey: "local-fixture-key-only", TextOptions: &cloudasr.VoiceTextOptions{Version: 1}}, nil
	}}
	for _, language := range []string{"en", ""} {
		id := store.NewID()
		ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		d, err := c.taskDialer(s, language)
		if err != nil {
			panic(err)
		}
		d = d.(interface{ WithTaskID(string) asr.Dialer }).WithTaskID("acceptance-" + strings.ReplaceAll(id, ".", "-"))
		pcm := make([]byte, 640)
		pcm[0] = 1
		raw, p, err := asr.TranscribeResult(ctx, d, nil, pcm)
		check("network receipt "+language, err == nil && p != nil)
		if err != nil {
			rows[len(rows)-1]["error"] = err.Error()
		}
		if p == nil {
			stop()
			continue
		}
		out := []string{}
		r := &run{id: id, started: time.Now(), ctx: ctx, stop: stop, pcm: pcm, translation: language, cleanup: false, processed: p, sink: func(t string) { out = append(out, t) }, rec: store.Record{ID: id, StartedAt: time.Now(), TranslationLanguage: language}}
		c.cur = r
		c.view = View{Phase: Finalizing, TranslationLanguage: language}
		c.finish(r, s, raw, nil)
		want := p.Text
		if language == "" {
			want = p.RawText
		}
		check("cleanup-off delivers correct body "+language, len(out) == 1 && out[0] == want)
		reopened, err := store.Open(dir)
		saved, ok := reopened.Get(id)
		check("disk history retains source receipt language "+language, err == nil && ok && saved.RawText == raw && saved.CleanedText == p.Text && saved.TranslationLanguage == language && saved.VoiceBilling != nil)
		if language != "" {
			err = c.RecoverVoiceResult(id)
			check("recover original language despite Japanese current setting", err == nil)
			c.Dismiss()
			err = c.Retranscribe(id)
			saved, _ = history.Get(id)
			check("history retranscription preserves English and delivers translation with cleanup off", err == nil && len(saved.Versions) == 1 && saved.Versions[0].Text == p.Text)
		}
		c.finish(r, s, raw, nil)
		check("late completion cannot deliver twice "+language, len(out) == 1)
		c.Dismiss()
		stop()
	}
	for _, action := range []string{"dismiss", "cancel", "practice-cancel"} {
		c.cur = nil
		c.view = View{Phase: Failed, Message: "preflight failure"}
		switch action {
		case "dismiss":
			c.Dismiss()
		case "cancel":
			c.Cancel()
		default:
			c.CancelPractice()
		}
		want := Idle
		if action == "practice-cancel" {
			want = Failed
		}
		check("no-run error "+action, c.View().Phase == want)
	}
	return rows
}
