// Package dict is the personal dictionary: term hints sent to the ASR,
// confirmed wrong→right rules applied to transcripts, and the candidates
// learned from the user's corrections, which only take effect once accepted.
package dict

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"voiceinput/internal/config"
	"voiceinput/internal/textstyle"
)

const (
	KindTerm = "term" // a vocabulary hint, e.g. OptiMAX
	KindRule = "rule" // a wrong spelling → the right one

	// Limits of the ASR's hotwords parameter (docs/asr-integration.md).
	MaxHotwords     = 128
	MaxHotwordChars = 4096
)

type Entry struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Term is the right spelling. Wrong is set for rules only.
	Term  string `json:"term"`
	Wrong string `json:"wrong,omitempty"`
	// Context limits a rule to transcripts containing one of these words,
	// so that Cloud→Claude does not rewrite a real "cloud".
	// A single Han rule instead needs an original phrase with one precise
	// occurrence; only that occurrence can be rewritten (single_han.go).
	Context   []string  `json:"context,omitempty"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

type Candidate struct {
	ID       string `json:"id"`
	Wrong    string `json:"wrong"`
	Right    string `json:"right"`
	Sentence string `json:"sentence"`
	// Source is "history-revision" or "observed-edit".
	Source   string    `json:"source"`
	RecordID string    `json:"recordId,omitempty"`
	Count    int       `json:"count"`
	Conflict string    `json:"conflict,omitempty"`
	Ignored  bool      `json:"ignored,omitempty"`
	At       time.Time `json:"at"`
}

type data struct {
	Entries    []Entry     `json:"entries"`
	Candidates []Candidate `json:"candidates"`
}

type Dict struct {
	mu      sync.Mutex
	path    string
	d       data
	seq     int
	loadErr error
}

func Open(dir string) (*Dict, error) {
	d := &Dict{path: filepath.Join(dir, "dictionary.json")}
	err := config.LoadJSON(d.path, func(b []byte) error {
		if err := config.ObjectJSON(b); err != nil {
			return err
		}
		var next data
		if err := json.Unmarshal(b, &next); err != nil {
			return err
		}
		d.d = next
		return nil
	})
	if err == os.ErrNotExist {
		return d, nil
	}
	if err != nil && !config.JSONRecovered(err) {
		d.loadErr = err
	}
	return d, err
}

func (d *Dict) save(next data) error {
	if d.loadErr != nil {
		return d.loadErr
	}
	err := config.WriteJSON(d.path, next)
	if config.JSONCommitted(err) {
		d.d = next
	}
	return err
}

func (d *Dict) savePruned(next data) error {
	if d.loadErr != nil {
		return d.loadErr
	}
	err := config.WriteJSONPruned(d.path, next)
	if config.JSONCommitted(err) {
		d.d = next
	}
	return err
}

func (d *Dict) draft() data {
	next := data{Entries: slices.Clone(d.d.Entries), Candidates: slices.Clone(d.d.Candidates)}
	for i := range next.Entries {
		next.Entries[i].Context = slices.Clone(next.Entries[i].Context)
	}
	return next
}

func (d *Dict) newID() string {
	d.seq++
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), d.seq)
}

func (d *Dict) Entries(query string) []Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Entry
	for _, e := range d.d.Entries {
		if q == "" || strings.Contains(strings.ToLower(e.Term), q) || strings.Contains(strings.ToLower(e.Wrong), q) {
			e.Context = slices.Clone(e.Context)
			out = append(out, e)
		}
	}
	return out
}

// Pending returns the candidates waiting for the user's decision.
func (d *Dict) Pending() []Candidate {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []Candidate
	for _, c := range d.d.Candidates {
		if !c.Ignored && candidateEligible(c) {
			out = append(out, c)
		}
	}
	return out
}

// Add adds an entry by hand or from a candidate. A term or rule that
// already exists is left alone and reported.
func (d *Dict) Add(e Entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	if err := d.add(&next, e); err != nil {
		return err
	}
	return d.save(next)
}

// Update validates a complete draft before committing it. Keep identity,
// creation time and application order; a failed edit never removes the old rule.
func (d *Dict) Update(id string, e Entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	i := slices.IndexFunc(next.Entries, func(x Entry) bool { return x.ID == id })
	if i < 0 {
		return errors.New("the entry is gone")
	}
	old := next.Entries[i]
	next.Entries = slices.Delete(next.Entries, i, i+1)
	if err := d.add(&next, e); err != nil {
		return err
	}
	updated := next.Entries[len(next.Entries)-1]
	updated.ID, updated.CreatedAt, updated.Source = old.ID, old.CreatedAt, old.Source
	next.Entries = slices.Insert(next.Entries[:len(next.Entries)-1], i, updated)
	return d.save(next)
}

func (d *Dict) add(next *data, e Entry) error {
	e.Term, e.Wrong = strings.TrimSpace(e.Term), strings.TrimSpace(e.Wrong)
	if e.Term == "" {
		return errors.New("the term is empty")
	}
	if strings.ContainsAny(e.Term+e.Wrong, "\r\n") {
		return errors.New("spellings must be a single line")
	}
	contexts := []string{}
	for _, context := range e.Context {
		if context = strings.TrimSpace(context); context != "" && !slices.Contains(contexts, context) {
			contexts = append(contexts, context)
		}
	}
	e.Context = contexts
	if e.Kind == KindRule && (e.Wrong == "" || e.Wrong == e.Term) {
		return errors.New("a rule needs a wrong spelling that differs from the right one")
	}
	if e.Kind != KindRule {
		e.Kind, e.Wrong, e.Context = KindTerm, "", nil
	} else if singleHan(e.Wrong) {
		contexts := []string{}
		for _, context := range e.Context {
			if context = strings.TrimSpace(context); singleHanContext(context, e.Wrong) {
				contexts = append(contexts, context)
			}
		}
		if len(contexts) == 0 {
			return ErrRuleContext
		}
		e.Context = contexts
	}
	for _, x := range next.Entries {
		if x.Kind == e.Kind && x.Term == e.Term && x.Wrong == e.Wrong {
			return fmt.Errorf("%q is already in the dictionary", e.Term)
		}
		if e.Kind == KindRule && x.Kind == KindRule && x.Wrong == e.Wrong {
			return fmt.Errorf("%q is already corrected to %q; edit or delete that rule first", x.Wrong, x.Term)
		}
	}
	e.ID, e.CreatedAt = d.newID(), time.Now()
	e.Context = slices.Clone(e.Context)
	next.Entries = append(next.Entries, e)
	return nil
}

func (d *Dict) Delete(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	next.Entries = slices.DeleteFunc(next.Entries, func(e Entry) bool { return e.ID == id })
	return d.savePruned(next)
}

// Accept turns a candidate into an entry of the kind the user chose, with
// the spellings and the context as the user may have edited them.
func (d *Dict) Accept(id, kind, wrong, right string, context []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := slices.IndexFunc(d.d.Candidates, func(c Candidate) bool { return c.ID == id })
	if i < 0 {
		return errors.New("the candidate is gone")
	}
	c := d.d.Candidates[i]
	if kind == KindRule && singleHan(strings.TrimSpace(wrong)) && len(context) == 0 {
		context = []string{c.Sentence}
	}
	next := d.draft()
	if err := d.add(&next, Entry{Kind: kind, Term: right, Wrong: wrong, Context: context, Source: c.Source + " " + c.RecordID}); err != nil {
		return err
	}
	next.Candidates = slices.Delete(next.Candidates, i, i+1)
	return d.save(next)
}

// Ignore keeps a candidate out of the list; the same correction seen again
// stays ignored.
func (d *Dict) Ignore(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	for i := range next.Candidates {
		if next.Candidates[i].ID == id {
			next.Candidates[i].Ignored = true
		}
	}
	return d.save(next)
}

// Hotwords returns every term, in order and without duplicates: the term
// hints and the right spellings of the rules.
func (d *Dict) Hotwords() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, e := range d.d.Entries {
		if !slices.Contains(out, e.Term) {
			out = append(out, e.Term)
		}
	}
	return out
}

// HotwordParam is the value of the ASR's hotwords parameter for the whole
// dictionary. It fails, instead of truncating, when the dictionary exceeds
// the service's limits or a term contains a comma.
func (d *Dict) HotwordParam() (string, int, error) {
	words := d.Hotwords()
	if len(words) > MaxHotwords {
		return "", len(words), fmt.Errorf("the dictionary has %d terms; the ASR accepts %d", len(words), MaxHotwords)
	}
	for _, w := range words {
		if strings.Contains(w, ",") {
			return "", len(words), fmt.Errorf("the term %q contains a comma, which the ASR's list cannot carry", w)
		}
	}
	p := strings.Join(words, ",")
	if len(p) > MaxHotwordChars {
		return "", len(words), fmt.Errorf("the dictionary's terms take %d characters; the ASR accepts %d", len(p), MaxHotwordChars)
	}
	return p, len(words), nil
}

// ConfirmedCorrections returns an independent, minimal snapshot for one task.
func (d *Dict) ConfirmedCorrections() []textstyle.Correction {
	d.mu.Lock()
	defer d.mu.Unlock()
	var rules []textstyle.Correction
	for _, e := range d.d.Entries {
		if e.Kind == KindRule && e.Wrong != "" {
			rules = append(rules, textstyle.Correction{Wrong: e.Wrong, Term: e.Term, Context: slices.Clone(e.Context)})
		}
	}
	return rules
}

// Apply rewrites whole words with confirmed, context-scoped rules in order.
func (d *Dict) Apply(text string) (string, []string) {
	return textstyle.ApplyCorrections(text, d.ConfirmedCorrections())
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

// Import reads a UTF-8 TXT file containing one keyword per line.
func (d *Dict) Import(path string) (added, total int, err error) {
	if !strings.EqualFold(filepath.Ext(path), ".txt") {
		return 0, 0, errors.New("please choose a TXT file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return 0, 0, errors.New("please save the TXT file as UTF-8")
	}
	text := strings.TrimPrefix(string(b), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	d.mu.Lock()
	defer d.mu.Unlock()
	next := d.draft()
	seen := make(map[string]bool)
	for _, e := range d.d.Entries {
		seen[e.Term] = true
	}
	for _, line := range strings.Split(text, "\n") {
		term := strings.TrimSpace(line)
		if term == "" {
			continue
		}
		total++
		if seen[term] {
			continue
		}
		seen[term] = true
		next.Entries = append(next.Entries, Entry{ID: d.newID(), Kind: KindTerm, Term: term, Source: "import " + filepath.Base(path), CreatedAt: time.Now()})
		added++
	}
	if added == 0 {
		return 0, total, nil
	}
	if err := d.save(next); err != nil {
		return 0, total, err
	}
	return added, total, nil
}

// Export writes unique keywords as UTF-8 text, one per line.
func (d *Dict) Export(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out strings.Builder
	seen := make(map[string]bool)
	for _, e := range d.d.Entries {
		if seen[e.Term] {
			continue
		}
		seen[e.Term] = true
		out.WriteString(e.Term)
		out.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(out.String()), 0600)
}
