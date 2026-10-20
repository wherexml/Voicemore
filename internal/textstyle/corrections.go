package textstyle

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Correction contains only a confirmed replacement and its scope. Learning
// candidates, provenance and history IDs must never be serialized here.
type Correction struct {
	Wrong   string   `json:"wrong"`
	Term    string   `json:"term"`
	Context []string `json:"context,omitempty"`
}

func ApplyCorrections(text string, rules []Correction) (string, []string) {
	var applied []string
	for _, e := range rules {
		if e.Wrong == "" || !hasContext(text, e.Context) {
			continue
		}
		var out string
		var n int
		if singleHan(e.Wrong) {
			out, n = replaceSingleHan(text, e)
		} else {
			out, n = replaceWhole(text, e.Wrong, e.Term)
		}
		if n > 0 {
			text = out
			applied = append(applied, e.Wrong+" → "+e.Term)
		}
	}
	return text, applied
}

func hasContext(text string, context []string) bool {
	if len(context) == 0 {
		return true
	}
	low := strings.ToLower(text)
	for _, c := range context {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" && strings.Contains(low, c) {
			return true
		}
	}
	return false
}

func isWordRune(r rune) bool {
	return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// replaceWhole replaces occurrences of wrong that are not part of a longer
// Latin word or number: "Cloud" does not match in "Cloudflare".
func replaceWhole(text, wrong, right string) (string, int) {
	var b strings.Builder
	n, rest := 0, text
	for {
		i := strings.Index(rest, wrong)
		if i < 0 {
			break
		}
		end := i + len(wrong)
		before, _ := lastRune(rest[:i])
		after, _ := firstRune(rest[end:])
		first, _ := firstRune(wrong)
		last, _ := lastRune(wrong)
		if (isWordRune(first) && isWordRune(before)) || (isWordRune(last) && isWordRune(after)) {
			b.WriteString(rest[:end])
		} else {
			b.WriteString(rest[:i])
			b.WriteString(right)
			n++
		}
		rest = rest[end:]
	}
	b.WriteString(rest)
	return b.String(), n
}

func firstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func lastRune(s string) (rune, bool) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, false
	}
	return r[len(r)-1], true
}

func singleHan(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	return n == len(s) && unicode.Is(unicode.Han, r)
}

func singleHanContext(context, wrong string) bool {
	return strings.TrimSpace(context) != wrong && strings.Count(context, wrong) == 1
}

// A single Han character is not a word. Authorize only the precise occurrence
// inside an original phrase, never every occurrence in a matching transcript.
// This also makes legacy unscoped single-character rules inert, without
// deleting or rewriting the user's dictionary during startup.
func replaceSingleHan(text string, rule Correction) (string, int) {
	allowed := map[int]bool{}
	for _, context := range rule.Context {
		context = strings.TrimSpace(context)
		if !singleHanContext(context, rule.Wrong) {
			continue
		}
		for from := 0; from < len(text); {
			i := strings.Index(text[from:], context)
			if i < 0 {
				break
			}
			at := from + i
			// A short original phrase such as "他。" must not also match
			// the suffix of "其他。". Cut-off long snippets are conservative:
			// prefer a missed correction over changing part of another word.
			first, _ := utf8.DecodeRuneInString(context)
			last, _ := utf8.DecodeLastRuneInString(context)
			before, _ := utf8.DecodeLastRuneInString(text[:at])
			after, _ := utf8.DecodeRuneInString(text[at+len(context):])
			if !(unicode.Is(unicode.Han, first) && unicode.Is(unicode.Han, before)) &&
				!(unicode.Is(unicode.Han, last) && unicode.Is(unicode.Han, after)) {
				allowed[at+strings.Index(context, rule.Wrong)] = true
			}
			from = at + len(context)
		}
	}
	var out strings.Builder
	n, from := 0, 0
	for from < len(text) {
		i := strings.Index(text[from:], rule.Wrong)
		if i < 0 {
			break
		}
		at := from + i
		out.WriteString(text[from:at])
		if allowed[at] {
			out.WriteString(rule.Term)
			n++
		} else {
			out.WriteString(rule.Wrong)
		}
		from = at + len(rule.Wrong)
	}
	out.WriteString(text[from:])
	return out.String(), n
}
