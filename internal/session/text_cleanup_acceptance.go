//go:build textcleanupacceptance

package session

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/store"
)

// AcceptTextCleanup exercises saved settings, the production finalization pipeline,
// real HTTP cleanup and the practice output sink using synthetic transcripts only.
func AcceptTextCleanup(root string) []map[string]any {
	must := func(e error) {
		if e != nil {
			panic(e)
		}
	}
	rows := []map[string]any{}
	check := func(name string, pass bool) { rows = append(rows, map[string]any{"case": name, "passed": pass}) }
	must(os.MkdirAll(root, 0700))
	settings, e := config.Open(root)
	must(e)
	s := settings.Get()
	s.SaveAudio = false
	s.PolishRules = "保留 <E2E> & English\n不要添加结论。"
	s.PolishRulesOpen = false
	must(settings.Set(s))
	reopened, e := config.Open(root)
	must(e)
	check("rules and expansion persist", reopened.Get().PolishRules == s.PolishRules && !reopened.Get().PolishRulesOpen)
	history, e := store.Open(root)
	must(e)
	dictionary, e := dict.Open(root)
	must(e)
	var mu sync.Mutex
	requests := []map[string]any{}
	listener, e := net.Listen("tcp", "127.0.0.1:58197")
	must(e)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"完成。"}}]}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	const raw = "嗯，保留 A P P，二十个，不要发布。"
	for _, managed := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			s.CleanupDefault = enabled
			must(settings.Set(s))
			c := &Controller{Settings: settings, History: history, Dict: dictionary, ResolveCleanup: func(config.Settings) (cleanup.Options, error) {
				return cleanup.Options{BaseURL: "http://127.0.0.1:58197", Model: "fixture"}, nil
			}}
			if managed {
				s.ASRAssign = config.Assignment{Provider: "sub2api", Model: "doubao-asr-2.0-cleanup"}
				must(settings.Set(s))
			}
			c.SetCleanupNext(enabled)
			check("effective switch honors choice", c.CleanupNext() == enabled)
			output := ""
			ctx, cancel := context.WithCancel(context.Background())
			r := &run{id: store.NewID(), ctx: ctx, stop: cancel, cleanup: enabled, sink: func(t string) { output = t }, rec: store.Record{ID: store.NewID(), RawText: raw}}
			if managed {
				r.processed = &asr.ProcessedResult{ID: "synthetic-receipt", RawText: raw, Text: "云端正文。", UsageLogID: 17, ChargeUSD: "0.01"}
			}
			c.cur = r
			mu.Lock()
			before := len(requests)
			mu.Unlock()
			// A pending run keeps its selected mode if the next-run default changes.
			s.CleanupDefault = !enabled
			must(settings.Set(s))
			check("in-flight choice overrides next default", c.CleanupNext() == enabled)
			c.finish(r, s, raw, nil)
			mu.Lock()
			after := len(requests)
			mu.Unlock()
			if !enabled {
				check("off delivers raw without local cleanup HTTP", output == raw && after == before && len(r.rec.AppliedRules) == 0)
			}
			if enabled && !managed {
				check("on uses HTTP and final formatting", output == "完成" && after == before+1)
			}
			if managed {
				check("cloud receipt retained with selectable raw output", r.rec.CleanedText == "云端正文。" && r.rec.VoiceBilling.UsageLogID == 17 && after == before && (!enabled || output == "云端正文。"))
			}
			cancel()
		}
	}
	_, e = cleanup.Translate(context.Background(), cleanup.Options{BaseURL: "http://127.0.0.1:58197", Model: "fixture", PolishRules: s.PolishRules}, raw, "English")
	must(e)
	mu.Lock()
	translation, marshalErr := json.Marshal(requests[len(requests)-1])
	mu.Unlock()
	must(marshalErr)
	check("translation excludes polish preferences", !strings.Contains(string(translation), "E2E"))
	mu.Lock()
	b, e := json.Marshal(requests)
	mu.Unlock()
	must(e)
	check("custom rules reach cleanup system prompt", strings.Contains(string(b), "E2E") && strings.Contains(string(b), "不要添加结论"))
	must(os.WriteFile(filepath.Join(root, "requests.json"), b, 0600))
	// An intentionally empty rule must stay empty across reopen.
	s.PolishRules = ""
	must(settings.Set(s))
	reopened, e = config.Open(root)
	must(e)
	check("empty custom rules remain empty", reopened.Get().PolishRules == "")
	// Saved formatting options survive switching off and back on.
	s.CleanupDefault = false
	s.TextStyle.TrimPeriod = false
	must(settings.Set(s))
	s.CleanupDefault = true
	must(settings.Set(s))
	reopened, e = config.Open(root)
	must(e)
	check("format choices survive toggling", !reopened.Get().TextStyle.TrimPeriod)
	return rows
}
