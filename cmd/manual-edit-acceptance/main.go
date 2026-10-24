//go:build darwin

// Exercises native field edits with delivered keyboard events and persisted candidates.
// Keyboard events are automated, not proof of physical keyboard or IME coverage.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf16"
	"voiceinput/internal/dict"
	"voiceinput/internal/macos"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var fixtureBundle string

func main() {
	flag.StringVar(&fixtureBundle, "bundle", "dev.steve.voice-input.manual-edit-fixture", "fixture bundle identity")
	replayMode := flag.Bool("replay", false, "platform-evidence replay; not native acceptance")
	root := flag.String("output", "", "new absolute evidence directory with Fixture.app")
	flag.Parse()
	if !filepath.IsAbs(*root) {
		panic("absolute output required")
	}
	if *replayMode {
		if replay(*root) {
			os.Exit(1)
		}
		return
	}
	failed := false
	mygo.App.SetName("Manual Edit Acceptance")
	mygo.App.WhenReady(func() {
		macos.OnMain = mygo.RunOnMain
		go func() { failed = run(*root); mygo.App.Quit() }()
	})
	must(mygo.App.Run())
	if failed {
		os.Exit(1)
	}
}
func run(root string) bool {
	stopKeys := make(chan struct{})
	defer close(stopKeys)
	go deliverKeys(root, stopKeys)
	write := func(command string) { must(os.WriteFile(filepath.Join(root, "command"), []byte(command), 0600)) }
	state := func() map[string]any {
		var v map[string]any
		b, _ := os.ReadFile(filepath.Join(root, "state.json"))
		_ = json.Unmarshal(b, &v)
		return v
	}
	wait := func(predicate func(map[string]any) bool) bool {
		for i := 0; i < 1500; i++ {
			if predicate(state()) {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	fixture := exec.Command(filepath.Join(root, "Fixture.app/Contents/MacOS/fixture"), root)
	must(fixture.Start())
	defer write("quit")
	time.Sleep(time.Second) // Let AppKit finish launch before acquiring the first target.
	original := "这个产品的 mark 的 demo 地址是多少？"
	results := []map[string]any{}
	for _, name := range []string{"manual", "surrounding", "append", "undo", "unchanged", "append_only", "programmatic", "focus_loss", "marked_only", "marked_commit", "duplicate", "cancel", "undo_immediate", "timeout"} {
		write("setup:" + name)
		time.Sleep(800 * time.Millisecond)
		ready := wait(func(s map[string]any) bool {
			return s["command"] == "setup:"+name && s["focused"] == true && s["active"] == true && s["keyWindow"] == true
		})
		target := macos.CaptureTarget()
		owned := target.BundleID == fixtureBundle
		target.Release()
		if !ready || !owned {
			results = append(results, map[string]any{"case": name, "passed": false, "blocked": "fixture not foreground"})
			break
		}
		type observation struct {
			text, status string
			samples      []macos.InputSample
			binding      macos.PasteBinding
		}
		done := make(chan observation, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		res, err := macos.Insert(original, macos.InsertOptions{RestoreDelayMs: 650, OnPasted: func(t *macos.Target) {
			go func() {
				b := t.Binding()
				var samples []macos.InputSample
				v, status := dict.ObserveSpan(ctx, dict.Span{Inserted: original, Before: b.Before, BeforeKnown: b.Known, Prefix: b.Prefix, Suffix: b.Suffix, Exact: b.Exact}, func() dict.Sample {
					sample := t.Sample()
					samples = append(samples, sample)
					return dict.Sample{Value: sample.Value, Readable: sample.Readable, Focused: sample.Focused, Input: sample.Input, Undo: sample.Undo, Composing: sample.Composing, CompositionKnown: sample.CompositionKnown}
				})
				done <- observation{text: v, status: status, samples: samples, binding: b}
			}()
		}})
		consumed := wait(func(s map[string]any) bool { return s["pasted"] == true })
		write("edit:" + name)
		if name == "cancel" {
			cancel()
		}
		var obs observation
		if err == nil && !res.ClipboardOnly {
			obs = <-done
		} else {
			obs.status = "paste-unavailable"
		}
		cancel()
		wait(func(s map[string]any) bool { return s["command"] == "edit:"+name && s["finished"] == true })
		if res.Target != nil {
			res.Target.Release()
		}
		must(os.MkdirAll(filepath.Join(root, name), 0700))
		d, e := dict.Open(filepath.Join(root, name))
		must(e)
		note := "no observed text"
		if obs.text != "" {
			_, note = d.Propose(original, obs.text, "observed-edit", name)
		}
		d, e = dict.Open(filepath.Join(root, name))
		must(e)
		pending := d.Pending()
		want := 0
		if name == "manual" || name == "surrounding" || name == "append" || name == "marked_commit" || name == "duplicate" || name == "timeout" {
			want = 1
		}
		fieldState := state()
		delivered := true
		if want == 1 || name == "undo" || name == "undo_immediate" || name == "append_only" {
			keys, _ := fieldState["keys"].(float64)
			delivered = keys >= 2
		}
		passed := ready && consumed && delivered && err == nil && len(pending) == want
		if want == 1 && len(pending) == 1 {
			passed = passed && pending[0].Wrong == "mark" && pending[0].Right == "mock"
		}
		row := map[string]any{"case": name, "passed": passed, "ready": ready, "pasteConsumed": consumed, "status": obs.status, "observed": obs.text, "pending": pending, "field": fieldState, "editKeysDelivered": delivered, "wanted": want, "proposalNote": note, "samples": obs.samples, "binding": obs.binding}
		if err != nil {
			row["error"] = err.Error()
		}
		results = append(results, row)
		b, _ := json.MarshalIndent(results, "", "  ")
		must(os.WriteFile(filepath.Join(root, "results.json"), b, 0600))
		fmt.Printf("%s: %v (paste=%v status=%s candidates=%d want=%d)\n", name, passed, consumed, obs.status, len(pending), want)
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(root, "results.json"), b, 0600))
	failed := false
	for _, r := range results {
		failed = failed || r["passed"] != true
	}
	return failed
}

// The signed runner posts automation keys; the fixture records actual delivery.
func deliverKeys(root string, stop <-chan struct{}) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	var create func(uintptr, uint16, bool) uintptr
	var flags func(uintptr, uint64)
	var unicode func(uintptr, uint64, *uint16)
	var post func(uint32, uintptr)
	var release func(uintptr)
	purego.RegisterLibFunc(&create, lib, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&flags, lib, "CGEventSetFlags")
	purego.RegisterLibFunc(&unicode, lib, "CGEventKeyboardSetUnicodeString")
	purego.RegisterLibFunc(&post, lib, "CGEventPost")
	purego.RegisterLibFunc(&release, cf, "CFRelease")
	last := 0
	for {
		select {
		case <-stop:
			return
		case <-time.After(10 * time.Millisecond):
		}
		var r struct {
			ID    int
			Code  uint16
			Text  string
			Flags uint64
		}
		b, _ := os.ReadFile(filepath.Join(root, "key.json"))
		if json.Unmarshal(b, &r) != nil || r.ID == last {
			continue
		}
		last = r.ID
		target := macos.CaptureTarget()
		owned := target.BundleID == fixtureBundle
		target.Release()
		record, _ := json.Marshal(map[string]any{"id": r.ID, "code": r.Code, "owned": owned, "time": time.Now().Format(time.RFC3339Nano)})
		f, e := os.OpenFile(filepath.Join(root, "key-delivery.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if e == nil {
			f.Write(append(record, '\n'))
			f.Close()
		}
		if !owned {
			continue
		}
		macos.OnMain(func() {
			for _, down := range []bool{true, false} {
				e := create(0, r.Code, down)
				if e == 0 {
					continue
				}
				flags(e, r.Flags)
				if r.Text != "" {
					u := utf16.Encode([]rune(r.Text))
					unicode(e, uint64(len(u)), &u[0])
				}
				post(0, e)
				release(e)
			}
		})
	}
}
