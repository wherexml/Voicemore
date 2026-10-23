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
	if len(a) > 1024 || len(b) > 1024 {
		return 0, "the revision is too large to learn from"
	}
	if len(a) == 0 || join(a) == join(b) {
		return 0, "no change to learn from"
	}
	hunks := diff(a, b)
	// Passive field observation is weaker evidence than an explicit history
	// revision: replacing an entire short message is usually new content.
	// The former six-token exemption admitted short whole-message rewrites.
	if source == "observed-edit" {
		changed, anchored := 0, false
		for _, h := range hunks {
			changed += max(len(h.from), len(h.to))
		}
		for _, t := range a {
			if hasLetter([]string{t}) {
				for _, u := range b {
					if t == u {
						anchored = true
						break
					}
				}
			}
		}
		if !anchored || float64(changed) > maxChangedPart*float64(len(a)) {
			return 0, "the observed edit replaces the message; no replacement rule proposed"
		}
	}
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
		// Do not turn clauses or uncommitted apostrophe-separated Pinyin into
		// vocabulary. This only affects proposed learning, never dictation output
		// or existing confirmed rules. Keep normal technical term punctuation.
		if source == "observed-edit" && !observedTermPair(join(h.from), join(h.to)) {
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
		sentence := snippet(original, wrong)
		if !candidateEligible(Candidate{Wrong: wrong, Right: right, Sentence: sentence, Source: source}) {
			continue
		}
		if d.propose(&next, wrong, right, sentence, source, recordID) {
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

// Passive edits are spelling evidence only. Changes between discourse
// particles express tone, not reusable terminology. Explicit revisions and
// manually configured rules remain available.
func observedTermPair(wrong, right string) bool {
	if !termShape(tokenize(wrong)) || !termShape(tokenize(right)) {
		return false
	}
	particles := "啊呀呢吧嘛吗嗎呐吶哦噢哎唉嗯啦呗唄哟喲"
	isParticle := func(s string) bool {
		return len([]rune(s)) == 1 && strings.Contains(particles, s)
	}
	return !isParticle(wrong) && !isParticle(right)
}

// Reassess legacy passive suggestions without rewriting the user's file or
// touching accepted entries. Old records contain only a local source snippet,
// so ambiguous positions cannot establish reliable replacement evidence.
func candidateEligible(c Candidate) bool {
	if c.Source != "observed-edit" {
		return true
	}
	if c.Wrong == "" || c.Right == "" || c.Wrong == c.Right || !observedTermPair(c.Wrong, c.Right) {
		return false
	}
	a := tokenize(c.Sentence)
	from, to := tokenize(c.Wrong), tokenize(c.Right)
	if len(a) > 1024 || len(from) == 0 || len(from) > maxHunkTokens || len(to) > maxHunkTokens || !hasLetter(from) || !hasLetter(to) {
		return false
	}
	// Stored pairs were joined from tokens; the original can still contain
	// spaces between Han and Latin ("V 六" versus "V六"). Match tokens rather
	// than falsely discarding those legitimate legacy suggestions.
	at := -1
	for i := 0; i+len(from) <= len(a); i++ {
		if join(a[i:i+len(from)]) == join(from) {
			if at >= 0 {
				return false
			}
			at = i
		}
	}
	if at < 0 || (!hasLetter(a[:at]) && !hasLetter(a[at+len(from):])) {
		return false
	}
	b := append([]string{}, a[:at]...)
	b = append(b, to...)
	b = append(b, a[at+len(from):]...)
	changed := 0
	for _, h := range diff(a, b) {
		changed += max(len(h.from), len(h.to))
	}
	return float64(changed) <= maxChangedPart*float64(len(a))
}

func termShape(tokens []string) bool {
	apostrophes := 0
	for _, r := range join(tokens) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r):
		case r == '\'' || r == '’':
			apostrophes++
		case strings.ContainsRune("._-+/#", r):
		default:
			return false
		}
	}
	return apostrophes <= 1
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
