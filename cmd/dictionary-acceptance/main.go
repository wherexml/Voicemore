//go:build darwin

// Real native paste/readback and persisted dictionary acceptance; --cloud
// replays the reported recording through the configured Doubao adapter.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/store"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	runtime.LockOSThread()
	observationMode := flag.Bool("observations", false, "replay edit sequences and verify persisted candidates")
	protocolMode := flag.Bool("protocol", false, "check real WebSocket framing against local protocol fixture")
	cloudMode := flag.Bool("cloud", false, "replay an existing record through configured Doubao")
	recordID := flag.String("record", "", "saved record ID to replay (private; not copied)")
	flag.Parse()
	dir, err := filepath.Abs("artifacts/dictionary-fix")
	must(err)
	must(os.MkdirAll(dir, 0700))
	if *observationMode {
		observations(dir)
		return
	}
	if *protocolMode {
		protocol(dir)
		return
	}
	if *cloudMode {
		if *recordID == "" {
			panic("--record is required for --cloud")
		}
		cloud(dir, *recordID)
		return
	}
	os.Remove(filepath.Join(dir, "command"))
	os.Remove(filepath.Join(dir, "ready"))
	fixture := exec.Command("open", "-n", "-W", filepath.Join(dir, "Fixture.app"), "--args", dir)
	must(fixture.Start())
	defer os.WriteFile(filepath.Join(dir, "command"), []byte("quit"), 0600)
	time.Sleep(time.Second)
	originalClipboard, err := exec.Command("pbpaste").Output()
	must(err)
	defer func() {
		c := exec.Command("pbcopy")
		in, e := c.StdinPipe()
		must(e)
		must(c.Start())
		in.Write(originalClipboard)
		in.Close()
		c.Wait()
	}()
	original := "这个产品的 mark 的 demo 地址是多少？"
	results := []map[string]any{}
	for _, name := range []string{"send", "append", "undo", "unchanged"} {
		must(os.WriteFile(filepath.Join(dir, "command"), []byte(name), 0600))
		ready := false
		for i := 0; i < 100; i++ {
			b, _ := os.ReadFile(filepath.Join(dir, "ready"))
			if string(b) == name {
				ready = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ready {
			panic("fixture not ready")
		}
		focused := false
		for i := 0; i < 100; i++ {
			target := macos.CaptureTarget()
			focused = target.BundleID == "dev.steve.voice-input.dictionary-fixture"
			target.Release()
			if focused {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !focused {
			panic("fixture not foreground")
		}

		type observation struct {
			text, status string
			samples      []map[string]any
			hasElement   bool
		}
		done := make(chan observation, 1)
		pasteStarted := time.Now()
		res, err := macos.Insert(original, macos.InsertOptions{RestoreDelayMs: 650, OnPasted: func(t *macos.Target) {
			go func() {
				var samples []map[string]any
				hasElement := t.HasElement()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				text, status := dict.Observe(ctx, original, func() (string, bool) {
					started := time.Since(pasteStarted).Milliseconds()
					v, ok := t.ReadValue()
					samples = append(samples, map[string]any{"startMs": started, "endMs": time.Since(pasteStarted).Milliseconds(), "readable": ok, "text": v})
					return v, ok
				})
				done <- observation{text: text, status: status, samples: samples, hasElement: hasElement}
			}()
		}})
		must(err)
		if res.ClipboardOnly {
			panic("native paste unavailable")
		}
		observed := <-done
		res.Target.Release()
		tmp, err := os.MkdirTemp(dir, "dict-"+name+"-")
		must(err)
		d, err := dict.Open(tmp)
		must(err)
		n, note := d.Propose(original, observed.text, "observed-edit", name)
		reopened, err := dict.Open(tmp)
		must(err)
		pending := reopened.Pending()
		want := 0
		if name == "send" || name == "append" {
			want = 1
		}
		passed := n == want && len(pending) == want && observed.status != "observeNoRead"
		if want == 1 {
			passed = passed && pending[0].Wrong == "mark" && pending[0].Right == "mock"
		}
		results = append(results, map[string]any{"case": name, "passed": passed, "observed": observed.text, "status": observed.status, "note": note, "pending": pending, "clipboardRestored": res.ClipboardRestored, "capturedElement": observed.hasElement, "samples": observed.samples})
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(dir, "native-results.json"), b, 0600))
	fmt.Println(string(b))
	for _, r := range results {
		if r["passed"] != true {
			panic("acceptance failed; see JSON evidence")
		}
	}
}
func cloud(out, recordID string) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library/Application Support/Voice Input")
	cfg, err := config.Open(dir)
	must(err)
	sec, err := secrets.Open(dir)
	must(err)
	dialer, err := providers.ASR(cfg.Get(), func(k string) string { v, e := sec.Get(k); must(e); return v })
	must(err)
	if cfg.Get().ASRAssign.Provider != "volc" || !dialer.Hotwords() {
		panic("expected Doubao with hotwords")
	}
	hist, err := store.Open(dir)
	must(err)
	rec, ok := hist.Get(recordID)
	if !ok {
		panic("reported recording unavailable")
	}
	pcm, err := hist.ReadPCM(rec.AudioFile)
	must(err)
	d, err := dict.Open(dir)
	must(err)
	terms, _, err := d.HotwordParam()
	must(err)
	results := []map[string]any{}
	for _, tc := range []struct {
		name  string
		terms *string
	}{{"without_dictionary", nil}, {"with_dictionary", &terms}} {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		text, err := asr.Transcribe(ctx, dialer, tc.terms, pcm)
		cancel()
		results = append(results, map[string]any{"case": tc.name, "text": text, "error": fmt.Sprint(err), "passed": err == nil, "recognizedMock": strings.Contains(strings.ToLower(text), "mock")})
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(out, "cloud-results.json"), b, 0600))
	fmt.Println(string(b))
	for _, r := range results {
		if r["passed"] != true {
			panic("acceptance failed; see JSON evidence")
		}
	}
}
