//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"voiceinput/internal/dict"
)

// Observation replay isolates edit tracking when the desktop is unavailable.
// It supplements, and never substitutes for, the native fixture.
func observations(out string) {
	original := "这个产品的 mark 的 demo 地址是多少？"
	corrected := "这个产品的 mock 的 demo 地址是多少？"
	type sample struct {
		text string
		ok   bool
	}
	yes := func(s string) sample { return sample{s, true} }
	cases := []struct {
		name  string
		steps []sample
		want  int
	}{
		{"correct_then_send", []sample{yes(original), yes(corrected), yes("")}, 1},
		{"correct_and_append_same_snapshot", []sample{yes(original), yes(corrected + "另外继续输入一大段新的文字，不要把追加文字作为拼写纠正。"), yes("")}, 1},
		{"correct_then_append_then_send", []sample{yes(original), yes(corrected), yes(corrected + "继续输入下一段文字。"), yes("")}, 1},
		{"undo_before_send", []sample{yes(original), yes(corrected), yes(original), yes("")}, 0},
		{"unchanged_send", []sample{yes(original), yes("")}, 0},
		{"append_only", []sample{yes(original), yes(original + "下面还有另外一段内容。"), yes("")}, 0},
		{"rewrite", []sample{yes(original), yes("这是完全不同的一段新消息，讨论明天去哪里吃饭。"), yes("")}, 0},
		{"unreadable", []sample{{"", false}}, 0},
		{"duplicate_baseline", []sample{yes(original + original)}, 0},
		{"oversized", []sample{yes(strings.Repeat("字", 4097) + original)}, 0},
		{"deadline", []sample{yes(original), yes(corrected)}, 1},
		{"partial_then_complete", []sample{yes(original), yes(strings.Replace(original, "mark", "mo", 1)), yes(corrected), yes("")}, 1},
	}
	results := []map[string]any{}
	for _, tc := range cases {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(tc.steps)+1)*100*time.Millisecond)
		revised, status := dict.Observe(ctx, original, func() (string, bool) {
			i := min(int(time.Since(start)/(100*time.Millisecond)), len(tc.steps)-1)
			v := tc.steps[i]
			return v.text, v.ok
		})
		cancel()
		path, err := os.MkdirTemp(out, "replay-")
		must(err)
		d, err := dict.Open(path)
		must(err)
		n := 0
		note := "no visible revision"
		if revised != "" {
			n, note = d.Propose(original, revised, "observed-edit", tc.name)
		}
		reopened, err := dict.Open(path)
		must(err)
		pending := reopened.Pending()
		passed := n == tc.want && len(pending) == tc.want
		if tc.want == 1 {
			passed = passed && len(pending) == 1 && pending[0].Wrong == "mark" && pending[0].Right == "mock"
		}
		results = append(results, map[string]any{"case": tc.name, "passed": passed, "note": note, "status": status, "revised": revised, "pending": pending})
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(out, "observation-results.json"), b, 0600))
	for _, r := range results {
		fmt.Printf("%s: %v\n", r["case"], r["passed"])
	}
	for _, r := range results {
		if r["passed"] != true {
			panic("observation replay failed")
		}
	}
}
