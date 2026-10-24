package dict

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Span binds an observation to the text one paste inserted into one field.
type Span struct {
	Inserted string
	// Before is the field just before the paste; BeforeKnown says it was read.
	Before      string
	BeforeKnown bool
	// With Exact, Prefix and Suffix are the field on either side of the
	// selection the paste replaced, and the paste counts as verified only
	// when the field reads exactly Prefix+Inserted+Suffix.
	Prefix, Suffix string
	Exact          bool
	// TrustReader counts a change of unknown provenance (zero Input) as
	// manual. Only Observe sets it, for replays and fixtures; production
	// fails closed.
	TrustReader bool
}

// Sample is one read of the bound field.
type Sample struct {
	Value    string
	Readable bool
	// Focused says the field still has the keyboard in the frontmost app.
	Focused bool
	// Input is when the latest physical key or mouse press happened; zero
	// when the platform cannot tell.
	Input time.Time
	// Undo says an undo shortcut was held at the sample.
	Undo bool
	// Composing says an input method's marked (uncommitted) text is in the
	// field. It means something only with CompositionKnown, which says the
	// field exposes its marked range.
	Composing, CompositionKnown bool
}

const (
	observeTick = 25 * time.Millisecond
	anchorWait  = 1500 * time.Millisecond
	// A change seen this long after the latest press is not the user's.
	manualWindow  = 1500 * time.Millisecond
	maxFieldRunes = 4096
)

// Observe is ObserveSpan for a reader of the field's text only, for replays
// and fixtures: the reader supplies committed snapshots; focus and input
// provenance are trusted. Production uses ObserveSpan with native metadata.
func Observe(ctx context.Context, inserted string, read func() (string, bool)) (string, string) {
	return ObserveSpan(ctx, Span{Inserted: inserted, TrustReader: true}, func() Sample {
		value, ok := read()
		return Sample{Value: value, Readable: ok, Focused: true, CompositionKnown: true}
	})
}

// ObserveSpan watches only the pasted field, and in it only the inserted
// span. It first waits for the paste to be read back where it was made.
// Afterwards it holds the current revision of the span only while that
// revision is complete: known to have no marked text and reached by manual input. Any later change withdraws it
// until the new state is complete in turn, so returning to the original
// clears an earlier correction at once. A send/clear, loss of the span, loss
// of readability or focus, a change nobody typed, cancellation (a new
// dictation) or the deadline ends observation with the revision held then;
// an undo ends it with nothing. It returns that revision ("" or the inserted
// text when nothing may be learned) and a status message key.
// Call immediately after paste dispatch, before waiting for clipboard restore.
func ObserveSpan(ctx context.Context, span Span, probe func() Sample) (string, string) {
	inserted := span.Inserted
	a := tokenize(inserted)
	if strings.TrimSpace(inserted) == "" || len(a) > 1024 {
		return "", "observeNoRead"
	}
	ticker := time.NewTicker(observeTick)
	defer ticker.Stop()
	start := time.Now()
	var (
		prefix, suffix, previous string
		anchored, read, unfocus  bool
		anchorAt                 time.Time
	)
	// latest is the complete revision held now; pending is the last one
	// seen, which may still be composed or typed.
	latest, pending := inserted, inserted
	unanchored := func() string {
		switch {
		case unfocus:
			return "observeFocus"
		case read:
			return "observeUnverified"
		}
		return "observeNoRead"
	}
	for {
		s := probe()
		now := time.Now()
		if !anchored {
			read = read || s.Readable
			unfocus = s.Readable && !s.Focused
			if p, sfx, ok := locate(span, s); ok {
				prefix, suffix, previous = p, sfx, s.Value
				anchored, anchorAt = true, now
			} else if now.Sub(start) > anchorWait {
				return "", unanchored()
			}
		} else {
			// An undo reverts what was just done; none of it is evidence.
			if s.Undo {
				return "", "observeUndo"
			}
			// Boundaries never promote an incomplete revision: a composition
			// or a word in progress is not a correction.
			if !s.Focused {
				return latest, "observeFocus"
			}
			if !s.Readable {
				return latest, "observeOutside"
			}
			if s.Value != previous {
				manual := (s.Input.After(anchorAt) && now.Sub(s.Input) <= manualWindow) || (span.TrustReader && s.Input.IsZero())
				revised, kept := within(s.Value, prefix, suffix)
				switch {
				case !manual:
					// The app, an extension or a sync changed the field, or
					// nothing says who did.
					return latest, "observeProgrammatic"
				case !kept:
					// A cleared/replaced field is a send boundary. Never inspect
					// the next message as a revision of this one.
					return latest, "observeOutside"
				}
				next, ok := correction(inserted, a, revised)
				if !ok {
					return latest, "observeOutside"
				}
				if next != pending {
					pending = next
					// The field moved on from what was held: withdraw it. Back at
					// the original there is nothing to learn, whatever came before.
					latest = inserted
				}
				previous = s.Value
			}
			// Composition can begin without changing AXValue. Withdraw a
			// previously complete value as soon as the field marks it.
			if s.CompositionKnown && s.Composing {
				latest = inserted
			}
			if pending != latest && complete(s) {
				latest = pending
			}
		}
		select {
		case <-ctx.Done():
			if !anchored {
				return "", unanchored()
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return latest, "observeInterrupted"
			}
			if latest == inserted && pending != inserted {
				return "", "observeUnconfirmed"
			}
			return latest, "observeSame"
		case <-ticker.C:
		}
	}
}

// complete never infers an IME commit from silence. Unsupported fields
// remain observable, but their unconfirmed revisions are not learned.
func complete(s Sample) bool { return s.CompositionKnown && !s.Composing }

// locate verifies the paste and splits the field around it.
func locate(span Span, s Sample) (string, string, bool) {
	if !s.Readable || !s.Focused || utf8.RuneCountInString(s.Value) > maxFieldRunes {
		return "", "", false
	}
	if span.Exact {
		if span.BeforeKnown && s.Value == span.Before {
			return "", "", false
		}
		if s.Value != span.Prefix+span.Inserted+span.Suffix {
			return "", "", false
		}
		return span.Prefix, span.Suffix, true
	}
	// Without the paste's own range: the text must be new to the field, be
	// there once, and leave the earlier field around it unchanged.
	if span.BeforeKnown && strings.Contains(span.Before, span.Inserted) {
		return "", "", false
	}
	if strings.Count(s.Value, span.Inserted) != 1 {
		return "", "", false
	}
	at := strings.Index(s.Value, span.Inserted)
	prefix, suffix := s.Value[:at], s.Value[at+len(span.Inserted):]
	if span.BeforeKnown && prefix+suffix != span.Before {
		return "", "", false
	}
	return prefix, suffix, true
}

// within returns the text between the bound prefix and suffix, while the
// field still has both and is not empty.
func within(value, prefix, suffix string) (string, bool) {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxFieldRunes ||
		len(value) < len(prefix)+len(suffix) || !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	return value[len(prefix) : len(value)-len(suffix)], true
}

// correction removes typing around the original from a revised span, and
// refuses a rewrite. An unchanged span is the inserted text itself.
func correction(inserted string, a []string, revised string) (string, bool) {
	b := tokenize(revised)
	if len(b) > 1024 {
		return "", false
	}
	hunks := diff(a, b)
	if len(hunks) == 0 {
		return inserted, true
	}
	// Ignore a pure appended run when the original transcript still has an
	// unchanged ending. Content typed after the dictation is not a correction.
	tail := hunks[len(hunks)-1]
	if tail.trailing && len(tail.from) == 0 && len(tail.to) > 0 && len(b) >= len(tail.to) {
		// Only remove an actual suffix, not an insertion in the middle.
		n := len(b) - len(tail.to)
		if join(b[n:]) == join(tail.to) {
			b = b[:n]
		}
	}
	// Likewise a pure run typed in front of the unchanged beginning.
	head := hunks[0]
	if !head.trailing && len(head.from) == 0 && len(a) > 0 && len(head.to) > 0 && len(head.to) < len(b) &&
		slices.Equal(b[:len(head.to)], head.to) && b[len(head.to)] == a[0] {
		b = b[len(head.to):]
	}
	// Losing most of the original anchors means this is a different message
	// or a rewrite, not a small spelling correction. Keep earlier evidence.
	changed := 0
	hunks = diff(a, b)
	for _, h := range hunks {
		changed += max(len(h.from), len(h.to))
	}
	if changed > maxHunkTokens && float64(changed) > maxChangedPart*float64(len(a)) {
		return "", false
	}
	if len(hunks) == 0 {
		return inserted, true
	}
	return join(b), true
}
