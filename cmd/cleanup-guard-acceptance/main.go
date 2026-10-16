// Command cleanup-guard-acceptance verifies production cleanup over HTTP.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	live := flag.Bool("live", false, "replay today's failed local history against configured local model")
	flag.Parse()
	cases := []sample{
		{"equivalent", "这2个页面，1个月27元，第2项用skills和icon", "这两个页面，一个月二十七元，第二项用skill和icons", true},
		{"count-lost", "购买1个服务器", "购买服务器", false},
		{"amount-changed", "每月27元", "每月28元", false},
		{"substring-number", "购买1个服务器", "购买10个服务器", false},
		{"decimal", "预算1元", "预算1.5元", false},
		{"identifier", "购买1个服务器", "购买A1服务器", false},
		{"english-lost", "使用skills和icon", "使用技能和图标", false},
		{"english-substring", "使用skill", "使用skillsExtra", false},
		{"filler", "这1个功能需要进行1个视觉上的左对齐", "这个功能需要视觉上的左对齐", true},
		{"mixed-count", "这1个功能需要2个按钮", "这个功能需要按钮", false},
		{"list-mask", "需要1个按钮和2个页面", "1. 需要按钮\n2. 需要页面", false},
		{"actual-ordinal", "第2个页面", "第二个页面", true},
		{"negative", "温度-5度", "温度5度", false},
		{"leading-decimal", "1.5元", "5元", false},
		{"zero", "预算0元", "预算10元", false},
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
		var b []byte

		var records []struct{ ID, StartedAt, RawText, CorrectedText, CleanupError string }
		b, _ = os.ReadFile(filepath.Join(dir, "history.json"))
		json.Unmarshal(b, &records)
		cases = nil
		for _, r := range records {
			if len(r.StartedAt) >= 10 && r.StartedAt[:10] == "2026-10-16" && r.CleanupError != "" {
				s := r.CorrectedText
				if s == "" {
					s = r.RawText
				}
				cases = append(cases, sample{Name: r.ID, Input: s})
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
		var rejected *cleanup.ChangedError
		ok := (err == nil) == s.Accept
		if *live {
			ok = err == nil || errors.As(err, &rejected)
		}
		var raw struct {
			Choices []struct{ Message struct{ Content string } }
		}
		json.Unmarshal([]byte(cap.body), &raw)
		oldMissing := []string{}
		if len(raw.Choices) > 0 {
			for _, t := range regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?|[A-Za-z][A-Za-z0-9]*`).FindAllString(s.Input, -1) {
				if !strings.Contains(strings.ToLower(raw.Choices[0].Message.Content), strings.ToLower(t)) {
					oldMissing = append(oldMissing, t)
				}
			}
		}

		passed = passed && ok
		rows = append(rows, map[string]any{"id": s.Name, "input": s.Input, "output": out, "response": cap.body, "error": message, "ms": time.Since(start).Milliseconds(), "passed": ok, "expectedAccept": s.Accept, "oldMissing": oldMissing})
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": passed, "live": *live, "model": opts.Model, "results": rows})
	if !passed {
		os.Exit(1)
	}
}
