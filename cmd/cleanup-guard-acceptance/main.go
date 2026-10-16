// Command cleanup-guard-acceptance verifies non-blocking cleanup over HTTP.
// The historical command name is kept for existing local workflows.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
)

type sample struct {
	Name, Input, Output string
	Accept              bool
}
type capture struct {
	base http.RoundTripper
	body string
}

func (c *capture) RoundTrip(r *http.Request) (*http.Response, error) {
	c.body = ""
	res, err := c.base.RoundTrip(r)
	if err != nil {
		return res, err
	}
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	c.body = string(b)
	res.Body = io.NopCloser(bytes.NewReader(b))
	return res, err
}
func main() {
	live := flag.Bool("live", false, "run the fidelity corpus against the configured model")
	recordID := flag.String("record-id", "", "with -live, replay exactly this local history record")
	repeat := flag.Int("repeat", 1, "number of serial repetitions")
	flag.Parse()
	if *repeat < 1 || (*recordID != "" && !*live) {
		panic("repeat must be positive; record-id requires -live")
	}
	cases := []sample{
		{"equivalent", "这2个页面，1个月27元，第2项用skills和icon", "这两个页面，一个月二十七元，第二项用skill和icons", true},
		{"filler", "我想做1个交互上的调整，让提示更清楚", "我想调整交互，让提示更清楚", true},
		{"self-correction", "买3个，不对，买5个", "买5个", true},
		{"no-lexical-rejection", "使用skills", "使用技能", true},
		{"empty-response", "保留内容", "", false},
	}

	opts := cleanup.Options{BaseURL: "http://127.0.0.1:58188/v1", Model: "fixture"}
	var current string
	if *live {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, "Library/Application Support/Voice Input")

		cfg, err := config.Open(dir)
		if err != nil {
			panic(err)
		}
		sec, err := secrets.Open(dir)
		if err != nil {
			panic(err)
		}
		opts, err = providers.CleanupOptions(cfg.Get(), func(k string) string {
			v, e := sec.Get(k)
			if e != nil {
				panic(e)
			}
			return v
		})
		if err != nil {
			panic(err)
		}
		cases = []sample{
			{Name: "filler-and-count", Input: "我想做1个交互上的调整，只保留1个确认按钮，另外2个按钮先不要删"},
			{Name: "quantity", Input: "先买1个服务器，预算每月27元，不要买2个，等测试通过再加"},
			{Name: "comparison", Input: "他们的导航比较清楚，我希望我们的导航也这么清楚，但不要照搬他们的颜色"},
			{Name: "self-correction", Input: "先买3个，不对，是5个，只改数量，不要改型号"},
			{Name: "english", Input: "使用skills和icon，保留README.md路径，不要翻译英文名称"},
		}
		if *recordID != "" {
			b, err := os.ReadFile(filepath.Join(dir, "history.json"))
			if err != nil {
				panic(err)
			}
			var records []struct{ ID, RawText, CorrectedText string }
			if err := json.Unmarshal(b, &records); err != nil {
				panic(err)
			}
			cases = nil
			for _, r := range records {
				if r.ID == *recordID {
					input := r.CorrectedText
					if input == "" {
						input = r.RawText
					}
					cases = append(cases, sample{Name: r.ID, Input: input})
					break
				}
			}
			if len(cases) == 0 {
				panic("history record not found")
			}
		}
	} else {
		l, err := net.Listen("tcp", "127.0.0.1:58188")
		if err != nil {
			panic(err)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": current}}}})
		})}
		go srv.Serve(l)
		defer srv.Close()
	}
	cap := &capture{base: http.DefaultTransport}
	http.DefaultClient.Transport = cap
	rows := []map[string]any{}
	passed := true
	for round := 1; round <= *repeat; round++ {
		for _, s := range cases {
			current = s.Output
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			start := time.Now()
			out, err := cleanup.Run(ctx, opts, s.Input)
			cancel()
			message := ""
			if err != nil {
				message = err.Error()
			}
			ok := (err == nil) == s.Accept
			if *live {
				ok = err == nil && out != ""
			}

			passed = passed && ok
			rows = append(rows, map[string]any{"id": s.Name, "input": s.Input, "output": out, "response": cap.body, "error": message, "ms": time.Since(start).Milliseconds(), "passed": ok, "expectedAccept": s.Accept, "round": round})
		}
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": passed, "live": *live, "model": opts.Model, "results": rows, "semanticReviewRequired": *live})
	if !passed {
		os.Exit(1)
	}
}
