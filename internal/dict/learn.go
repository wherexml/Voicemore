package dict

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Learning compares a transcript with the user's correction of it and
// proposes the small replacements between them as candidates. A correction
// that rewrites the sentence proposes nothing.

const (
	maxHunkTokens  = 6    // per side of one replacement
	maxHunks       = 3    // more than this is a rewrite
	maxChangedPart = 0.35 // of the transcript's tokens
)

// tokenize splits text into Latin words and numbers, single CJK characters,
// and other single characters; spaces are dropped.
func tokenize(s string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, string(word))
			word = word[:0]
		}
	}
	for _, r := range s {
		switch {
		case isWordRune(r) || (len(word) > 0 && (r == '-' || r == '_')):
			word = append(word, r)
		case unicode.IsSpace(r):
			flush()
		default:
			flush()
			out = append(out, string(r))
		}
	}
	flush()
	return out
}

type hunk struct {
	from, to []string
	trailing bool
}

// diff returns the replaced runs between two token lists, by their longest
// common subsequence. Runs only deleted or only inserted have an empty side.
func diff(a, b []string) []hunk {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var hunks []hunk
	var cur hunk
	flush := func() {
		if len(cur.from) > 0 || len(cur.to) > 0 {
			hunks = append(hunks, cur)
			cur = hunk{}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			flush()
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			cur.from = append(cur.from, a[i])
			i++
		default:
			cur.to = append(cur.to, b[j])
			j++
		}
	}
	cur.from = append(cur.from, a[i:]...)
	cur.to = append(cur.to, b[j:]...)
	cur.trailing = true
	flush()
	return hunks
}

// join puts tokens back together: Latin words apart, the rest closed up.
func join(tokens []string) string {
	var b strings.Builder
	for i, t := range tokens {
		if i > 0 {
			p, _ := lastRune(tokens[i-1])
			f, _ := firstRune(t)
			if isWordRune(p) && isWordRune(f) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t)
	}
	return b.String()
}

func hasLetter(tokens []string) bool {
	for _, t := range tokens {
		for _, r := range t {
			if unicode.IsLetter(r) {
				return true
			}
		}
	}
	return false
}

func onlyDigits(tokens []string) bool {
	for _, t := range tokens {
		for _, r := range t {
			if !unicode.IsDigit(r) && r != '.' {
				return false
			}
		}
	}
	return true
}

// Propose records candidates for the replacements between original and
// revised, and says what it did, for the history. Nothing takes effect
// until the user accepts a candidate.
func (d *Dict) Propose(original, revised, source, recordID string) (int, string) {
	a, b := tokenize(original), tokenize(revised)
	if len(a) == 0 || join(a) == join(b) {
		return 0, "no change to learn from"
	}
	hunks := diff(a, b)
	changed := 0
	var repl []hunk
	for _, h := range hunks {
		changed += max(len(h.from), len(h.to))
		// Deleted passages and added ones are edits of the content,
		// not spellings of a term.
		if len(h.from) == 0 || len(h.to) == 0 {
			continue
		}
		if len(h.from) > maxHunkTokens || len(h.to) > maxHunkTokens {
			return 0, "the revision rewrites a passage; no replacement rule proposed"
		}
		// Punctuation and numbers are corrections of fact or style.
		if !hasLetter(h.from) || !hasLetter(h.to) || onlyDigits(h.from) || onlyDigits(h.to) {
			continue
		}
		repl = append(repl, h)
	}
	if float64(changed) > maxChangedPart*float64(len(a)) && changed > maxHunkTokens {
		return 0, "the revision changes most of the text; no replacement rule proposed"
	}
	if len(repl) > maxHunks {
		return 0, "the revision changes many places; no replacement rule proposed"
	}
	if len(repl) == 0 {
		return 0, "no term-like replacement found"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	added := 0
	for _, h := range repl {
		wrong, right := join(h.from), join(h.to)
		if d.propose(&next, wrong, right, snippet(original, wrong), source, recordID) {
			added++
		}
	}
	if err := d.save(next); err != nil {
		return 0, "candidates could not be saved: " + err.Error()
	}
	if added == 0 {
		return 0, "the same correction is already known or was ignored"
	}
	return added, fmt.Sprintf("%d candidate(s) wait for confirmation in the dictionary", added)
}

func (d *Dict) propose(next *data, wrong, right, sentence, source, recordID string) bool {
	for _, e := range next.Entries {
		if e.Kind == KindRule && e.Wrong == wrong && e.Term == right {
			return false
		}
	}
	conflict := ""
	for _, e := range next.Entries {
		if e.Kind == KindRule && e.Wrong == wrong && e.Term != right {
			conflict = fmt.Sprintf("a rule already corrects %q to %q", wrong, e.Term)
		}
	}
	for i := range next.Candidates {
		c := &next.Candidates[i]
		if c.Wrong == wrong && c.Right == right {
			// The same correction again: merged, and still ignored
			// if the user ignored it.
			c.Count++
			return false
		}
		if c.Wrong == wrong && !c.Ignored {
			conflict = fmt.Sprintf("another candidate corrects %q to %q", wrong, c.Right)
		}
	}
	next.Candidates = append(next.Candidates, Candidate{
		ID: d.newID(), Wrong: wrong, Right: right, Sentence: sentence,
		Source: source, RecordID: recordID, Count: 1, Conflict: conflict, At: time.Now(),
	})
	return true
}

// snippet returns the text around the first occurrence of part.
func snippet(text, part string) string {
	r := []rune(text)
	i := strings.Index(text, part)
	if i < 0 {
		if len(r) > 60 {
			return string(r[:60]) + "…"
		}
		return text
	}
	at := len([]rune(text[:i]))
	from, to := max(0, at-20), min(len(r), at+len([]rune(part))+20)
	return string(r[from:to])
}

// LooksLikeCommonWord says a wrong spelling is a plain Latin word, which a
// rule without a context would rewrite wherever it is really said.
func LooksLikeCommonWord(wrong string) bool {
	if wrong == "" {
		return false
	}
	for i, r := range wrong {
		if !isWordRune(r) || unicode.IsDigit(r) || (i > 0 && unicode.IsUpper(r)) {
			return false
		}
	}
	return true
}
