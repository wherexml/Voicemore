package dict

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrRuleContext = errors.New("a single-character Chinese rule needs an unambiguous original phrase; add it as a term hint instead")

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
func replaceSingleHan(text string, rule Entry) (string, int) {
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
