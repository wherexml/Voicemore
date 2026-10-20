// Command cleanup-quality-acceptance replays a frozen private corpus through the
// production dictionary, text formatting, real cleanup service and final format.
// Model quality requires human review; HTTP success is not semantic acceptance.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
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
	repeat := flag.Int("repeat", 1, "serial repetitions per record")
	standalone := flag.Bool("standalone", false, "explicitly test the saved independent cleanup configuration without changing app settings")
	flag.Parse()
	if *corpus == "" || *repeat < 1 {
		panic("corpus and positive repeat required")
	}
	var rows []struct{ ID, Raw, Previous string }
	b, e := os.ReadFile(*corpus)
	must(e)
	must(json.Unmarshal(b, &rows))
	if len(rows) == 0 {
		panic("empty corpus")
	}
	home, e := os.UserHomeDir()
	must(e)
	dir := filepath.Join(home, "Library/Application Support/Voice Input")
	cfg, e := config.Open(dir)
	must(e)
	s := cfg.Get()
	if *standalone && providers.ManagedVoice(s) {
		s.ASRAssign.Provider = providers.Local
	}
	d, e := dict.Open(dir)
	must(e)
	vault, e := secrets.Open(dir)
	must(e)
	opts, e := providers.CleanupOptions(s, func(k string) string { v, e := vault.Get(k); must(e); return v })
	must(e)
	opts.PolishRules = s.PolishRules
	enc := json.NewEncoder(os.Stdout)
	failed := false
	for _, r := range rows {
		for i := 0; i < *repeat; i++ {
			input, _ := d.Apply(r.Raw)
			input, _ = textstyle.Apply(input, s.TextStyle, d.Hotwords())
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.CleanupTimeoutSec)*time.Second)
			start := time.Now()
			out, err := cleanup.Run(ctx, opts, input)
			cancel()
			final, _ := textstyle.Apply(out, s.TextStyle, d.Hotwords())
			msg := ""
			if err != nil {
				msg = err.Error()
				failed = true
			}
			must(enc.Encode(map[string]any{"id": r.ID, "round": i + 1, "raw": r.Raw, "input": input, "previous": r.Previous, "model": opts.Model, "body": out, "final": final, "error": msg, "ms": time.Since(start).Milliseconds(), "semanticReviewRequired": true}))
		}
	}
	if failed {
		os.Exit(1)
	}
}
