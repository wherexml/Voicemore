// Package textstyle applies conservative, deterministic dictation formatting.
// Quoted text, code and URLs are kept verbatim. It does not rewrite sentences.
package textstyle

import (
	"regexp"
	"strings"
	"unicode"
)

type Options struct {
	TrimPeriod       bool `json:"trimPeriod"`
	CalmExclamations bool `json:"calmExclamations"`
	RemoveFillers    bool `json:"removeFillers"`
	JoinAcronyms     bool `json:"joinAcronyms"`
}

func Defaults() Options { return Options{true, true, true, true} }

var protected = regexp.MustCompile("(?s)```.*?(?:```|$)|`[^`\\n]*(?:`|$)|“[^”]*(?:”|$)|「[^」]*(?:」|$)|『[^』]*(?:』|$)|\"[^\"\\n]*(?:\"|$)|https?://[^\\s<>]+")
var letters = regexp.MustCompile(`[A-Z](?:[ \t]+[A-Z])+`)
var word = regexp.MustCompile(`^[A-Za-z]{2,20}$`)

func latin(r rune) bool    { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func han(r rune) bool      { return r >= '\u4e00' && r <= '\u9fff' }
func boundary(r rune) bool { return strings.ContainsRune("，,。！？!?；;：:\n", r) }
func space(r rune) bool    { return r == ' ' || r == '\t' }
func fillers(t string) string {
	r := []rune(t)
	for i := 0; i < len(r); {
		if i > 0 && !boundary(r[i-1]) {
			i++
			continue
		}
		j := i
		for j < len(r) && space(r[j]) {
			j++
		}
		if j == len(r) || !strings.ContainsRune("嗯呃啊", r[j]) {
			i++
			continue
		}
		k := j + 1
		for k < len(r) && r[k] == r[j] {
			k++
		}
		for k < len(r) && space(r[k]) {
			k++
		}
		if k < len(r) && !boundary(r[k]) {
			i++
			continue
		}
		if k < len(r) {
			k++
		}
		r = append(r[:i], r[k:]...)
	}
	for i := 1; i < len(r); {
		if r[i] == '啊' && han(r[i-1]) {
			j := i
			for j < len(r) && r[j] == '啊' {
				j++
			}
			if j == len(r) || boundary(r[j]) {
				r = append(r[:i], r[j:]...)
				continue
			}
		}
		i++
	}
	return string(r)
}
func calm(t string) string {
	r := []rune(t)
	var b strings.Builder
	for i := 0; i < len(r); {
		if r[i] != '!' && r[i] != '！' {
			b.WriteRune(r[i])
			i++
			continue
		}
		j := i
		full := false
		for j < len(r) && (r[j] == '!' || r[j] == '！') {
			full = full || r[j] == '！'
			j++
		}
		if j < len(r) && (r[j] == '?' || r[j] == '？') {
			i = j
			continue
		}
		if full || i > 0 && han(r[i-1]) {
			b.WriteRune('。')
		} else if i > 0 && latin(r[i-1]) && (j == len(r) || unicode.IsSpace(r[j])) {
			b.WriteRune('.')
		} else {
			b.WriteString(string(r[i:j]))
		}
		i = j
	}
	return b.String()
}
func Apply(text string, o Options, terms []string) (string, []string) {
	if o == (Options{}) {
		return text, nil
	}
	var changes []string
	record := func(label, before, after string) string {
		if before != after {
			found := false
			for _, v := range changes {
				found = found || v == label
			}
			if !found {
				changes = append(changes, label)
			}
		}
		return after
	}
	words := map[string]string{}
	for _, w := range strings.Fields("APP API UI UX TXT JSON HTML CSS SQL CPU GPU ASR TTS LLM SDK URL HTTP HTTPS PR UTM MCP Typeless") {
		words[strings.ToUpper(w)] = w
	}
	for _, w := range terms {
		if word.MatchString(w) {
			k := strings.ToUpper(w)
			if _, ok := words[k]; !ok {
				words[k] = w
			}
		}
	}
	plain := func(t string) string {
		if o.JoinAcronyms {
			old := t
			var b strings.Builder
			pos := 0
			for _, m := range letters.FindAllStringIndex(t, -1) {
				b.WriteString(t[pos:m[0]])
				v := t[m[0]:m[1]]
				before := []rune(t[:m[0]])
				after := []rune(t[m[1]:])
				if (len(before) == 0 || !latin(before[len(before)-1])) && (len(after) == 0 || !latin(after[0])) {
					key := strings.ReplaceAll(strings.ReplaceAll(v, " ", ""), "\t", "")
					if w, ok := words[key]; ok {
						v = w
					}
				}
				b.WriteString(v)
				pos = m[1]
			}
			b.WriteString(t[pos:])
			t = record("缩写连写", old, b.String())
		}
		if o.RemoveFillers {
			t = record("语气词", t, fillers(t))
		}
		if o.CalmExclamations {
			t = record("感叹号降调", t, calm(t))
		}
		return t
	}
	type part struct {
		t         string
		protected bool
	}
	var parts []part
	pos := 0
	for _, m := range protected.FindAllStringIndex(text, -1) {
		parts = append(parts, part{plain(text[pos:m[0]]), false}, part{text[m[0]:m[1]], true})
		pos = m[1]
	}
	parts = append(parts, part{plain(text[pos:]), false})
	if o.TrimPeriod {
		for i := len(parts) - 1; i >= 0; i-- {
			p := &parts[i]
			if strings.TrimSpace(p.t) == "" {
				continue
			}
			if !p.protected {
				old := p.t
				t := strings.TrimRightFunc(p.t, unicode.IsSpace)
				if strings.HasSuffix(t, "。") {
					t = strings.TrimSuffix(t, "。")
				} else if strings.HasSuffix(t, ".") && !strings.HasSuffix(t, "..") {
					f := strings.Fields(t)
					if len(f) > 0 && strings.Count(f[len(f)-1], ".") == 1 {
						t = strings.TrimSuffix(t, ".")
					}
				}
				p.t = record("句尾句号", old, strings.TrimRightFunc(t, unicode.IsSpace))
			}
			break
		}
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.t)
	}
	return strings.TrimSpace(b.String()), changes
}
