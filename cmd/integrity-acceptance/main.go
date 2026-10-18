//go:build integrityacceptance

// File/reopen and HTTP/controller acceptance. Uses synthetic data only.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

type evidence struct {
	Root     string           `json:"root"`
	Rows     []map[string]any `json:"checks"`
	Requests []map[string]any `json:"requests"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func bytesAt(path string) string { b, e := os.ReadFile(path); must(e); return string(b) }
func (e *evidence) check(name string, ok bool, details ...any) {
	r := map[string]any{"case": name, "passed": ok}
	if len(details) > 0 {
		r["details"] = details
	}
	e.Rows = append(e.Rows, r)
}
func (e *evidence) dir(name string) string {
	p := filepath.Join(e.Root, name)
	must(os.MkdirAll(p, 0700))
	return p
}

func main() {
	root := flag.String("output", "artifacts/review-fixes-20261017/run", "isolated evidence directory (must not exist)")
	flag.Parse()
	absolute, err := filepath.Abs(*root)
	must(err)
	if _, err = os.Stat(absolute); !errors.Is(err, os.ErrNotExist) {
		panic("output must be a fresh directory")
	}
	must(os.MkdirAll(absolute, 0700))
	e := &evidence{Root: absolute}
	persistence(e)
	dictionary(e)
	shortcuts(e)
	httpAcceptance(e)
	b, err := json.MarshalIndent(e, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(absolute, "results.json"), b, 0600))
	passed := true
	for _, r := range e.Rows {
		if r["passed"] != true {
			passed = false
			fmt.Fprintln(os.Stderr, "FAIL:", r["case"])
		}
	}
	fmt.Printf("%d checks; passed=%v; evidence=%s\n", len(e.Rows), passed, filepath.Join(absolute, "results.json"))
	if !passed {
		os.Exit(1)
	}
}

func persistence(e *evidence) {
	for _, kind := range []string{"settings", "history", "dictionary"} {
		for i, bad := range []string{`{broken`, `{"privateApps":"wrong","entries":4}`, `null`} {
			if kind == "history" {
				bad = []string{`[{broken`, `[{"durationMs":"wrong"}]`, `{"wrong":"shape"}`}[i]
			}
			if kind == "dictionary" && i == 1 {
				bad = `{"entries":"wrong"}`
			}
			dir := e.dir(fmt.Sprintf("unrecoverable-%s-%d", kind, i))
			path := filepath.Join(dir, kind+".json")
			must(os.WriteFile(path, []byte(bad), 0600))
			var openErr, writeErr error
			switch kind {
			case "settings":
				s, err := config.Open(dir)
				openErr = err
				next := s.Get()
				next.CleanupDefault = true
				writeErr = s.Set(next)
				e.check("unreadable settings expose load guard", s.LoadError() != nil)
			case "history":
				s, err := store.Open(dir)
				openErr = err
				writeErr = s.Add(store.Record{ID: "must-not-write"})
				e.check("unreadable history blocks audio writes", s.WriteWAV("blocked.wav", []byte{0, 1}) != nil)
			case "dictionary":
				s, err := dict.Open(dir)
				openErr = err
				writeErr = s.Add(dict.Entry{Term: "must-not-write"})
			}
			e.check(fmt.Sprintf("%s damaged shape %d is preserved and unwritable", kind, i), openErr != nil && writeErr != nil && bytesAt(path) == bad)
		}
	}
	for _, kind := range []string{"settings", "history", "dictionary"} {
		for _, missing := range []bool{false, true} {
			dir := e.dir(fmt.Sprintf("recover-%s-%v", kind, missing))
			path := filepath.Join(dir, kind+".json")
			switch kind {
			case "settings":
				s, err := config.Open(dir)
				must(err)
				v := s.Get()
				v.PrivateApps = []config.PrivateApp{{BundleID: "private.fixture", AppName: "Private Fixture"}}
				must(s.Set(v))
				v.CleanupDefault = true
				must(s.Set(v))
			case "history":
				s, err := store.Open(dir)
				must(err)
				must(s.Add(store.Record{ID: "previous", RawText: "preserved text"}))
				must(s.Add(store.Record{ID: "latest"}))
			case "dictionary":
				s, err := dict.Open(dir)
				must(err)
				must(s.Add(dict.Entry{Term: "previous"}))
				must(s.Add(dict.Entry{Term: "latest"}))
			}
			backup := bytesAt(path + ".bak")
			if missing {
				must(os.Remove(path))
			} else {
				must(os.WriteFile(path, []byte("{broken"), 0600))
			}
			var openErr error
			valid := false
			switch kind {
			case "settings":
				s, err := config.Open(dir)
				openErr = err
				valid = s.LoadError() != nil && s.Get().PrivateBundle("private.fixture") && s.Set(s.Get()) != nil
			case "history":
				s, err := store.Open(dir)
				openErr = err
				valid = len(s.List("")) == 1 && s.List("")[0].ID == "previous"
			case "dictionary":
				s, err := dict.Open(dir)
				openErr = err
				valid = len(s.Entries("")) == 1 && s.Entries("")[0].Term == "previous"
			}
			preserved, _ := filepath.Glob(path + ".corrupt-*")
			if kind == "settings" {
				_, statErr := os.Stat(path)
				unchanged := missing && errors.Is(statErr, os.ErrNotExist) || !missing && bytesAt(path) == "{broken"
				e.check(fmt.Sprintf("settings backup requires explicit privacy repair %v", missing), openErr != nil && valid && unchanged && bytesAt(path+".bak") == backup)
				continue
			}
			e.check(fmt.Sprintf("%s backup restores %s primary", kind, map[bool]string{true: "missing", false: "corrupt"}[missing]), config.JSONRecovered(openErr) && valid && bytesAt(path) == backup && (missing || len(preserved) == 1))
			if len(preserved) == 1 {
				e.check("damaged bytes preserved exactly", bytesAt(preserved[0]) == "{broken")
			}
		}
	}
	dir := e.dir("missing-primary-bad-backup")
	must(os.WriteFile(filepath.Join(dir, "settings.json.bak"), []byte("bad"), 0600))
	s, err := config.Open(dir)
	e.check("bad backup is not mistaken for a fresh install", err != nil && s.LoadError() != nil && s.Set(s.Get()) != nil)
	dir = e.dir("settings-transaction")
	s, err = config.Open(dir)
	must(err)
	v := s.Get()
	v.Providers.Custom = []config.CustomProvider{{ID: "fixture", Models: []string{"old"}}}
	must(s.Set(v))
	v.Providers.Custom[0].Models[0] = "external mutation"
	e.check("settings Set owns provider slices", s.Get().Providers.Custom[0].Models[0] == "old")
	before := s.Get()
	primary := bytesAt(filepath.Join(dir, "settings.json"))
	must(os.Mkdir(filepath.Join(dir, "settings.json.bak"), 0700))
	v = s.Get()
	v.SaveAudio = !v.SaveAudio
	err = s.Set(v)
	e.check("settings failure keeps disk and memory", err != nil && reflect.DeepEqual(before, s.Get()) && bytesAt(filepath.Join(dir, "settings.json")) == primary)
	must(os.Remove(filepath.Join(dir, "settings.json.bak")))
	must(os.WriteFile(filepath.Join(dir, "settings.json.tmp"), []byte("unrelated"), 0600))
	must(s.Set(v))
	again, err := config.Open(dir)
	e.check("settings save and reopen after fault removal", err == nil && again.Get().SaveAudio == v.SaveAudio && bytesAt(filepath.Join(dir, "settings.json.tmp")) == "unrelated")
	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	must(err)
	backupInfo, err := os.Stat(filepath.Join(dir, "settings.json.bak"))
	must(err)
	e.check("primary and backup are private files", info.Mode().Perm() == 0600 && backupInfo.Mode().Perm() == 0600)

	for _, op := range []string{"add", "update", "delete", "clear", "purge-text", "purge-audio"} {
		dir = e.dir("history-failure-" + op)
		h, err := store.Open(dir)
		must(err)
		r := store.Record{ID: "old", RawText: "original", StartedAt: time.Now().AddDate(0, 0, -40), AudioFile: "old.wav", Versions: []store.Version{{Text: "original version"}}, AppliedRules: []string{"original rule"}}
		must(h.WriteWAV("old.wav", []byte{0, 1, 0, 2}))
		must(h.Add(r))
		before := h.List("")
		primary := bytesAt(filepath.Join(dir, "history.json"))
		audio := bytesAt(h.AudioPath("old.wav"))
		must(os.Mkdir(filepath.Join(dir, "history.json.bak"), 0700))
		switch op {
		case "add":
			err = h.Add(store.Record{ID: "new"})
		case "update":
			_, err = h.Update("old", func(r *store.Record) {
				r.Versions[0].Text = "mutated"
				r.AppliedRules[0] = "mutated"
				r.RawText = "mutated"
			})
		case "delete":
			err = h.Delete("old")
		case "clear":
			err = h.Clear()
		case "purge-text":
			err = h.Purge(0, 30, time.Now())
		case "purge-audio":
			err = h.Purge(30, 0, time.Now())
		}
		e.check("failed history "+op+" keeps memory, disk and audio", err != nil && reflect.DeepEqual(before, h.List("")) && bytesAt(filepath.Join(dir, "history.json")) == primary && bytesAt(h.AudioPath("old.wav")) == audio)
		must(os.Remove(filepath.Join(dir, "history.json.bak")))
		if op == "delete" || op == "clear" || op == "purge-text" {
			if op == "delete" {
				err = h.Delete("old")
			} else if op == "clear" {
				err = h.Clear()
			} else {
				err = h.Purge(0, 30, time.Now())
			}
			must(err)
			again, err := store.Open(dir)
			_, audioErr := os.Stat(h.AudioPath("old.wav"))
			e.check("successful "+op+" removes audio and backup text", err == nil && len(again.List("")) == 0 && errors.Is(audioErr, os.ErrNotExist) && !strings.Contains(bytesAt(filepath.Join(dir, "history.json.bak")), "original"))
		}
	}
	// Actual directory permission failure, with a readable old primary.
	dir = e.dir("readonly-directory")
	s, err = config.Open(dir)
	must(err)
	must(s.Set(s.Get()))
	before = s.Get()
	v = s.Get()
	v.SaveAudio = !v.SaveAudio
	must(os.Chmod(dir, 0500))
	err = s.Set(v)
	must(os.Chmod(dir, 0700))
	e.check("read-only directory keeps settings", err != nil && reflect.DeepEqual(before, s.Get()))
	dir = e.dir("audio-atomic-write")
	h, err := store.Open(dir)
	must(err)
	must(h.WriteWAV("existing.wav", []byte{0, 1}))
	oldAudio := bytesAt(h.AudioPath("existing.wav"))
	must(os.Chmod(filepath.Join(dir, "audio"), 0500))
	err = h.WriteWAV("existing.wav", []byte{0, 2, 0, 3})
	newErr := h.WriteWAV("new.wav", []byte{0, 2, 0, 3})
	must(os.Chmod(filepath.Join(dir, "audio"), 0700))
	_, missingErr := os.Stat(h.AudioPath("new.wav"))
	e.check("failed audio writes preserve old bytes and leave no partial file", err != nil && newErr != nil && os.IsNotExist(missingErr) && bytesAt(h.AudioPath("existing.wav")) == oldAudio)
}

func dictionary(e *evidence) {
	shortDir := e.dir("dictionary-short-context")
	short, shortErr := dict.Open(shortDir)
	must(shortErr)
	must(short.Add(dict.Entry{Kind: dict.KindRule, Wrong: "他", Term: "她", Context: []string{"他。"}}))
	shortOut, _ := short.Apply("其他。他。")
	e.check("short phrase cannot match suffix of another Han word", shortOut == "其他。她。", shortOut)
	dir := e.dir("dictionary-context")
	d, err := dict.Open(dir)
	must(err)
	err = d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "他", Term: "她"})
	e.check("unscoped single Han rule is rejected", errors.Is(err, dict.ErrRuleContext) && len(d.Entries("")) == 0)
	_, note := d.Propose("他明天到，名单保持不变。", "她明天到，名单保持不变。", "history-revision", "synthetic")
	cs := d.Pending()
	e.check("real revision produces candidate", len(cs) == 1, note)
	if len(cs) == 1 {
		must(d.Accept(cs[0].ID, dict.KindRule, cs[0].Wrong, cs[0].Right, nil))
	}
	d, err = dict.Open(dir)
	must(err)
	input := "其他内容。他明天到，名单保持不变。现在正在开会，他稍后联系。"
	output, rules := d.Apply(input)
	e.check("reopened single Han rule changes only original phrase", output == "其他内容。她明天到，名单保持不变。现在正在开会，他稍后联系。" && len(rules) == 1, input, output)
	output, rules = d.Apply("其他内容，现在正在开会，他稍后联系。")
	e.check("absent phrase does not authorize single Han replacement", output == "其他内容，现在正在开会，他稍后联系。" && len(rules) == 0)
	must(d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "飞书", Term: "Feishu"}))
	must(d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "mark", Term: "mock"}))
	output, _ = d.Apply("打开飞书，mark 和 bookmark。")
	e.check("multi-Han terms and Latin word boundaries preserved", output == "打开Feishu，mock 和 bookmark。", output)
	for _, context := range [][]string{nil, {" "}, {"他"}, {"会议"}, {"他告诉他"}} {
		err = d.Add(dict.Entry{Kind: dict.KindRule, Wrong: "在", Term: "再", Context: context})
		e.check("invalid single Han context rejected", errors.Is(err, dict.ErrRuleContext), context)
	}
	dir = e.dir("dictionary-legacy-single")
	legacy := map[string]any{"entries": []dict.Entry{{ID: "legacy-one", Kind: dict.KindRule, Wrong: "他", Term: "她"}, {ID: "legacy-two", Kind: dict.KindRule, Wrong: "在", Term: "再"}}, "candidates": []dict.Candidate{}}
	b, err := json.Marshal(legacy)
	must(err)
	must(os.WriteFile(filepath.Join(dir, "dictionary.json"), b, 0600))
	d, err = dict.Open(dir)
	must(err)
	output, rules = d.Apply("其他内容，现在正在开会，我在家。")
	e.check("legacy unsafe rules are inert without deleting them", output == "其他内容，现在正在开会，我在家。" && len(rules) == 0 && len(d.Entries("")) == 2)
	dir = e.dir("dictionary-ambiguous")
	d, err = dict.Open(dir)
	must(err)
	d.Propose("他问他明天来吗", "她问他明天来吗", "history-revision", "synthetic")
	cs = d.Pending()
	if len(cs) == 1 {
		err = d.Accept(cs[0].ID, dict.KindRule, cs[0].Wrong, cs[0].Right, nil)
		e.check("ambiguous single Han candidate stays pending", errors.Is(err, dict.ErrRuleContext) && len(d.Pending()) == 1)
		must(d.Accept(cs[0].ID, dict.KindTerm, "", cs[0].Right, nil))
	} else {
		e.check("ambiguous single Han candidate stays pending", false)
	}
	e.check("ambiguous correction can still be accepted as a hint", len(d.Pending()) == 0 && len(d.Entries("")) == 1 && d.Entries("")[0].Kind == dict.KindTerm)
	for _, op := range []string{"add", "delete", "accept", "ignore", "import", "propose"} {
		dir = e.dir("dictionary-failure-" + op)
		d, err = dict.Open(dir)
		must(err)
		must(d.Add(dict.Entry{Term: "existing"}))
		d.Propose("use mark now", "use mock now", "history-revision", "synthetic")
		beforeEntries, beforePending := d.Entries(""), d.Pending()
		primary := bytesAt(filepath.Join(dir, "dictionary.json"))
		must(os.Remove(filepath.Join(dir, "dictionary.json.bak")))
		must(os.Mkdir(filepath.Join(dir, "dictionary.json.bak"), 0700))
		switch op {
		case "add":
			err = d.Add(dict.Entry{Term: "new"})
		case "delete":
			err = d.Delete(beforeEntries[0].ID)
		case "accept":
			err = d.Accept(beforePending[0].ID, dict.KindRule, "mark", "mock", nil)
		case "ignore":
			err = d.Ignore(beforePending[0].ID)
		case "import":
			path := filepath.Join(dir, "input.txt")
			must(os.WriteFile(path, []byte("imported\n"), 0600))
			_, _, err = d.Import(path)
		case "propose":
			n, note := d.Propose("use dock now", "use doc now", "history-revision", "synthetic")
			if n == 0 && strings.Contains(note, "could not be saved") {
				err = errors.New(note)
			} else {
				err = nil
			}
		}
		e.check("failed dictionary "+op+" has no partial mutation", err != nil && reflect.DeepEqual(beforeEntries, d.Entries("")) && reflect.DeepEqual(beforePending, d.Pending()) && bytesAt(filepath.Join(dir, "dictionary.json")) == primary)
	}
}

func shortcuts(e *evidence) {
	dir := e.dir("shortcut-save")
	s, err := config.Open(dir)
	must(err)
	must(s.Set(s.Get()))
	for _, key := range []string{"A", "1", ";", "Space", "Return", "Enter", "Tab", "Escape", "Backspace", "Left", "Shift+A", "Shift+1", "Shift+Space", "LeftShift+A", "Ctrl++A", "Cmd+Cmd+A", "Cmd+A+B", "Fn+Unknown", " ", "Cmd+"} {
		v := s.Get()
		before := v.TriggerKey
		v.TriggerKey = key
		err = s.Set(v)
		back, openErr := config.Open(dir)
		e.check("unsafe shortcut rejected before persistence: "+key, errors.Is(config.ValidateShortcut(key), config.ErrUnsafeShortcut) && errors.Is(err, config.ErrUnsafeShortcut) && openErr == nil && back.Get().TriggerKey == before)
	}
	for _, key := range []string{"Fn", "RightOption", "LeftShift", "RightShift", "Ctrl+Alt", "RightControl+RightOption", "Fn+K", "Cmd+Shift+A", "Control+Option+K", "CmdOrCtrl+Shift+K", "F18", "Shift+F19", "Cmd++", ""} {
		v := s.Get()
		v.TranslationKey = key
		err = s.Set(v)
		back, openErr := config.Open(dir)
		e.check("safe shortcut survives reopen: "+key, config.ValidateShortcut(key) == nil && err == nil && openErr == nil && back.Get().TranslationKey == key)
	}
	// Simulate a legacy file without invoking any global OS registration.
	dir = e.dir("shortcut-legacy")
	must(os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"schemaVersion":6,"triggerKey":"Shift+A"}`), 0600))
	s, err = config.Open(dir)
	must(err)
	v := s.Get()
	v.SaveAudio = false
	e.check("legacy unsafe binding is detectable while settings remain editable", config.ValidateShortcut(v.TriggerKey) != nil && s.Set(v) == nil)
	v.TriggerKey = "Fn"
	must(s.Set(v))
	back, err := config.Open(dir)
	e.check("legacy unsafe binding can be repaired", err == nil && back.Get().TriggerKey == "Fn")
}

func httpAcceptance(e *evidence) {
	const base = "http://127.0.0.1:58196/v1"
	var mu sync.Mutex
	counts := map[string]int{}
	ln, err := net.Listen("tcp", "127.0.0.1:58196")
	must(err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		must(json.NewDecoder(r.Body).Decode(&request))
		mu.Lock()
		counts[request.Model]++
		e.Requests = append(e.Requests, map[string]any{"model": request.Model, "path": r.URL.Path, "messages": request.Messages})
		mu.Unlock()
		var response string
		switch request.Model {
		case "length", "length-empty":
			content := "half the text"
			if request.Model == "length-empty" {
				content = ""
			}
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "length", "message": map[string]string{"content": content}}}})
			response = string(b)
		case "oversize", "exact-limit", "oversize-whitespace":
			response = `{"choices":[{"finish_reason":"stop","message":{"content":"complete"}}]}`
			response += strings.Repeat(" ", (4<<20)-len(response))
			if request.Model != "exact-limit" {
				response += " "
			}
		case "malformed":
			response = "invalid json"
		case "empty":
			response = `{"choices":[{"message":{"content":" "}}]}`
		case "body-timeout", "cancelled":
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		case "transport-truncated":
			w.Header().Set("Content-Length", "1000")
			response = `{"choices":[`
		case "status":
			w.WriteHeader(503)
			return
		case "no-reason":
			response = `{"choices":[{"message":{"content":"complete"}}]}`
		default:
			response = `{"choices":[{"finish_reason":"stop","message":{"content":"complete"}}]}`
		}
		fmt.Fprint(w, response)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	for _, model := range []string{"length", "length-empty", "oversize", "oversize-whitespace", "exact-limit", "malformed", "empty", "body-timeout", "cancelled", "transport-truncated", "status", "no-reason", "stop"} {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		if model == "cancelled" {
			time.AfterFunc(50*time.Millisecond, cancel)
		}
		out, err := cleanup.Run(ctx, cleanup.Options{BaseURL: base, Model: model}, "完整原文，不要丢失末尾条件。")
		cancel()
		ok := false
		switch model {
		case "length", "length-empty":
			ok = errors.Is(err, cleanup.ErrTruncated) && out == ""
		case "oversize", "oversize-whitespace":
			ok = errors.Is(err, cleanup.ErrResponseTooLarge) && out == ""
		case "malformed", "empty":
			ok = errors.Is(err, cleanup.ErrNoText) && out == ""
		case "body-timeout":
			ok = errors.Is(err, context.DeadlineExceeded)
		case "cancelled":
			ok = errors.Is(err, context.Canceled)
		case "transport-truncated":
			ok = errors.Is(err, cleanup.ErrUnreachable)
		case "status":
			var status *cleanup.StatusError
			ok = errors.As(err, &status) && status.Code == 503
		default:
			ok = err == nil && out == "complete"
		}
		mu.Lock()
		count := counts[model]
		mu.Unlock()
		e.check("HTTP "+model, ok && count == 1, fmt.Sprint(err), out)
	}
	out, err := cleanup.Translate(context.Background(), cleanup.Options{BaseURL: base, Model: "length"}, "原文", "English")
	e.check("translation shares truncated-response rejection", out == "" && errors.Is(err, cleanup.ErrTruncated))
	e.Rows = append(e.Rows, session.AcceptIntegrityPipeline(base, e.dir("controller"))...)
	// Snapshot the HTTP transcript after all synchronous controller checks.
	mu.Lock()
	b, err := json.MarshalIndent(counts, "", "  ")
	mu.Unlock()
	must(err)
	must(os.WriteFile(filepath.Join(e.Root, "request-counts.json"), b, 0600))
	h := sha256.Sum256(b)
	e.check("HTTP request audit saved", len(counts) > 0, fmt.Sprintf("%x", h))
}
