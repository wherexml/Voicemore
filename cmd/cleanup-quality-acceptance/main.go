// Command cleanup-quality-acceptance replays a frozen private corpus through the
// production dictionary, text formatting, real cleanup service and final format.
// Model quality requires human review; HTTP success is not semantic acceptance.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/textstyle"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	corpus := flag.String("corpus", "", "frozen JSON with id, raw and previous fields")
	stream := flag.Bool("stream", false, "read one sample per stdin JSON value; keep connections alive for paired replay")
	frozenDir := flag.String("frozen-dir", "", "optional frozen options.json and dictionary.json directory")
	localBase := flag.String("local-base", "", "optional isolated HTTP loopback endpoint; never sends the saved API key")
	repeat := flag.Int("repeat", 1, "serial repetitions per record")
	standalone := flag.Bool("standalone", false, "explicitly test the saved independent cleanup configuration without changing app settings")
	flag.Parse()
	if (*corpus == "") == !*stream || *repeat < 1 || (*stream && *repeat != 1) {
		panic("choose corpus or stream; positive repeat required, stream uses one response per input")
	}
	type sample struct{ ID, Raw, Previous string }
	var rows []sample
	if !*stream {
		b, e := os.ReadFile(*corpus)
		must(e)
		must(json.Unmarshal(b, &rows))
		if len(rows) == 0 {
			panic("empty corpus")
		}
	}
	home, e := os.UserHomeDir()
	must(e)
	dir := filepath.Join(home, "Library/Application Support/Voice Input")
	cfg, e := config.Open(dir)
	must(e)
	s := cfg.Get()
	// Capture the application's route before any acceptance-only override.
	// Saved independent cleanup settings do not mean daily dictation uses them.
	route := map[string]any{
		"scope":                          "standalone_cleanup_replay",
		"application_uses_managed_voice": providers.ManagedVoice(s),
		"configured_asr_provider":        s.ASRAssign.Provider,
		"configured_asr_model":           s.ASRAssign.Model,
		"configured_cleanup_provider":    s.CleanupAssign.Provider,
		"explicit_standalone":            *standalone,
		"isolated_endpoint_override":     *localBase != "",
		"application_settings_changed":   false,
	}
	dictDir := dir
	if *frozenDir != "" {
		b, e := os.ReadFile(filepath.Join(*frozenDir, "options.json"))
		must(e)
		var frozen struct {
			TextStyle   textstyle.Options `json:"textStyle"`
			PolishRules string            `json:"polishRules"`
		}
		must(json.Unmarshal(b, &frozen))
		s.TextStyle, s.PolishRules = frozen.TextStyle, frozen.PolishRules
		dictDir = *frozenDir
	}
	if *standalone && providers.ManagedVoice(s) {
		s.ASRAssign.Provider = providers.Local
	}
	d, e := dict.Open(dictDir)
	must(e)
	vault, e := secrets.Open(dir)
	must(e)
	opts, e := providers.CleanupOptions(s, func(k string) string { v, e := vault.Get(k); must(e); return v })
	must(e)
	opts.PolishRules = s.PolishRules
	if *localBase != "" {
		u, err := url.Parse(*localBase)
		must(err)
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			panic("local-base must be an HTTP loopback IP URL without credentials, query or fragment")
		}
		opts.BaseURL, opts.APIKey = *localBase, ""
	}
	enc := json.NewEncoder(os.Stdout)
	failed := false
	requests := 0
	run := func(r sample) {
		if r.ID == "" {
			panic("sample ID required")
		}
		for i := 0; i < *repeat; i++ {
			input, _ := d.Apply(r.Raw)
			input, _ = textstyle.Apply(input, s.TextStyle, d.Hotwords())
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.CleanupTimeoutSec)*time.Second)
			start := time.Now()
			var mu sync.Mutex
			reused := false
			var connectionMS, firstByteMS int64 = -1, -1
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
				GotConn: func(info httptrace.GotConnInfo) {
					mu.Lock()
					defer mu.Unlock()
					reused, connectionMS = info.Reused, time.Since(start).Milliseconds()
				},
				GotFirstResponseByte: func() {
					mu.Lock()
					defer mu.Unlock()
					firstByteMS = time.Since(start).Milliseconds()
				},
			})
			out, err := cleanup.Run(ctx, opts, input)
			cancel()
			final, _ := textstyle.Apply(out, s.TextStyle, d.Hotwords())
			msg := ""
			if err != nil {
				msg = err.Error()
				failed = true
			}
			mu.Lock()
			trace := map[string]any{"connection_reused": reused, "connection_ms": connectionMS, "first_byte_ms": firstByteMS}
			mu.Unlock()
			requests++
			must(enc.Encode(map[string]any{"id": r.ID, "round": i + 1, "request_index": requests, "raw": r.Raw, "input": input, "input_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(input))), "previous": r.Previous, "model": opts.Model, "body": out, "final": final, "error": msg, "ms": time.Since(start).Milliseconds(), "http": trace, "route": route, "isolated_endpoint": *localBase, "semanticReviewRequired": true}))
		}
	}
	if *stream {
		dec := json.NewDecoder(os.Stdin)
		for {
			var r sample
			if e := dec.Decode(&r); e == io.EOF {
				break
			} else {
				must(e)
			}
			run(r)
		}
		if requests == 0 {
			panic("empty stream")
		}
	} else {
		for _, r := range rows {
			run(r)
		}
	}
	if failed {
		os.Exit(1)
	}
}
