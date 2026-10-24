//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"voiceinput/internal/dict"
)

// Replay covers platform evidence failures through ObserveSpan -> persistence ->
// reopen. It is supplemental integration evidence, never a native acceptance.
func replay(root string) bool {
	original := "请使用 mark 查看这个页面。"
	corrected := "请使用 mock 查看这个页面。"
	type step struct {
		value                              string
		focus, read, manual, known, marked bool
	}
	good := func(v string) step { return step{v, true, true, true, true, false} }
	badManual := good(corrected)
	badManual.manual = false
	unknown := good(corrected)
	unknown.known = false
	marked := good(corrected)
	marked.marked = true
	off := good(corrected)
	off.focus = false
	unread := good(corrected)
	unread.read = false
	tests := []struct {
		name   string
		steps  []step
		want   int
		before string
		exact  bool
	}{
		{"committed", []step{good(original), good(corrected), good("")}, 1, "", true},
		{"programmatic", []step{good(original), badManual, good("")}, 0, "", true},
		{"unknown_composition", []step{good(original), unknown}, 0, "", true},
		{"marked_then_timeout", []step{good(original), marked}, 0, "", true},
		{"marked_then_clear", []step{good(original), marked, good("")}, 0, "", true},
		{"marked_then_unreadable", []step{good(original), marked, unread}, 0, "", true},
		{"same_value_recomposition", []step{good(original), good(corrected), marked, good("")}, 0, "", true},
		{"committed_after_composition", []step{good(original), marked, good(corrected), good("")}, 1, "", true},
		{"focus_before_edit", []step{good(original), off, good(corrected)}, 0, "", true},
		{"undo_to_original_then_send", []step{good(original), good(corrected), good(original), good("")}, 0, "", true},
		{"paste_not_changed", []step{good(original), good(original)}, 0, original, true},
		{"read_failure_before_anchor", []step{unread}, 0, "", true},
	}
	var rows []map[string]any
	failed := false
	for _, tc := range tests {
		dir := filepath.Join(root, tc.name)
		must(os.MkdirAll(dir, 0700))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(tc.steps)+1)*100*time.Millisecond)
		revised, status := dict.ObserveSpan(ctx, dict.Span{Inserted: original, Before: tc.before, BeforeKnown: true, Exact: tc.exact}, func() dict.Sample {
			i := min(int(time.Since(start)/(100*time.Millisecond)), len(tc.steps)-1)
			s := tc.steps[i]
			input := time.Time{}
			if s.manual {
				input = start.Add(time.Duration(i) * 100 * time.Millisecond)
			}
			return dict.Sample{Value: s.value, Readable: s.read, Focused: s.focus, Input: input, CompositionKnown: s.known, Composing: s.marked}
		})
		cancel()
		d, e := dict.Open(dir)
		must(e)
		if revised != "" {
			d.Propose(original, revised, "observed-edit", tc.name)
		}
		d, e = dict.Open(dir)
		must(e)
		pending := d.Pending()
		pass := len(pending) == tc.want
		failed = failed || !pass
		rows = append(rows, map[string]any{"case": tc.name, "passed": pass, "status": status, "pending": pending, "revised": revised})
	}
	b, _ := json.MarshalIndent(rows, "", "  ")
	must(os.WriteFile(filepath.Join(root, "results.json"), b, 0600))
	return failed
}
