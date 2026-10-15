package dict

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// Observe watches only the pasted field. The latest complete snapshot is kept
// until send/clear, loss of the original span, or the observation deadline.
// Sampling does not commit intermediate keystrokes as dictionary candidates.
// Call immediately after paste dispatch, before waiting for clipboard restore.
func Observe(ctx context.Context, inserted string, read func() (string, bool)) (string, string) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	baselineDeadline := time.Now().Add(time.Second)
	var prefix, suffix, latest, previous string
	a := tokenize(inserted)
	if inserted == "" || len(a) > 1024 {
		return "", "observeNoRead"
	}
	anchored := false
	for {
		value, ok := read()
		if !anchored {
			if ok && strings.Count(value, inserted) == 1 && inserted != "" && utf8.RuneCountInString(value) <= 4096 {
				at := strings.Index(value, inserted)
				prefix, suffix = value[:at], value[at+len(inserted):]
				latest, anchored = inserted, true
			} else if time.Now().After(baselineDeadline) {
				return "", "observeNoRead"
			}
		} else if value != previous || !ok {
			// A cleared/replaced field is a send boundary. Never inspect the next
			// message as a revision of this one; keep the last observed correction.
			if !ok || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 4096 ||
				len(value) < len(prefix)+len(suffix) || !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
				return latest, "observeOutside"
			}
			revised := value[len(prefix) : len(value)-len(suffix)]
			// Ignore a pure appended run when the original transcript still has an
			// unchanged ending. Content typed after the dictation is not a correction.
			b := tokenize(revised)
			if len(a) > 1024 || len(b) > 1024 {
				return latest, "observeOutside"
			}
			hunks := diff(a, b)
			if len(hunks) > 0 {
				tail := hunks[len(hunks)-1]
				if tail.trailing && len(tail.from) == 0 && len(tail.to) > 0 && len(b) >= len(tail.to) {
					// Only remove an actual suffix, not an insertion in the middle.
					n := len(b) - len(tail.to)
					if join(b[n:]) == join(tail.to) {
						b = b[:n]
					}
				}
			}
			// Losing most of the original anchors means this is a different message
			// or a rewrite, not a small spelling correction. Keep earlier evidence.
			changed := 0
			for _, h := range diff(a, b) {
				changed += max(len(h.from), len(h.to))
			}
			if changed > maxHunkTokens && float64(changed) > maxChangedPart*float64(len(a)) {
				return latest, "observeOutside"
			}
			latest = join(b)
		}
		previous = value
		select {
		case <-ctx.Done():
			if !anchored {
				return "", "observeNoRead"
			}
			return latest, "observeSame"
		case <-ticker.C:
		}
	}
}
