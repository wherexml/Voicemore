//go:build darwin

// Real microphone/controller acceptance with a controlled ASR response.
// This measures scheduling, not recognition quality. No clipboard or app insertion.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

type recognition struct {
	nonzero  atomic.Int64
	finished chan time.Time
}

func (*recognition) Label() string                                       { return "controlled acceptance" }
func (*recognition) Check(context.Context) error                         { return nil }
func (r *recognition) Dial(context.Context, *string) (asr.Stream, error) { return r, nil }
func (*recognition) Hotwords() bool                                      { return false }
func (r *recognition) Send(_ context.Context, b []byte) error {
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] != 0 || b[i+1] != 0 {
			r.nonzero.Add(1)
		}
	}
	return nil
}
func (r *recognition) Finish(context.Context) error { r.finished <- time.Now(); return nil }
func (*recognition) Err() error                     { return nil }
func (*recognition) Close()                         {}
func (*recognition) Final() string                  { return "并行收尾调度验证" }
func (*recognition) Pending() string                { return "" }
func (*recognition) SetOnUpdate(func())             {}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
func snapshot() map[string]any {
	b, e := exec.Command(os.Getenv("VOICEMORE_AUDIO_PROBE")).Output()
	must(e)
	var v map[string]any
	must(json.Unmarshal(b, &v))
	return v
}
func main() {
	dir, e := os.MkdirTemp("", "voicemore-parallel-")
	must(e)
	defer os.RemoveAll(dir)
	settings, e := config.Open(dir)
	must(e)
	s := settings.Get()
	s.MuteOutput = true
	s.InteractionSounds = false
	s.CleanupDefault = false
	must(settings.Set(s))
	history, e := store.Open(dir)
	must(e)
	dictionary, e := dict.Open(dir)
	must(e)
	before := snapshot()
	if before["input"].(map[string]any)["running"] != float64(0) {
		panic("microphone busy; no test started")
	}
	r := &recognition{finished: make(chan time.Time, 1)}
	c := &session.Controller{Settings: settings, History: history, Dict: dictionary, ResolveASR: func(config.Settings) (asr.Dialer, error) { return r, nil }}
	delivered := make(chan time.Time, 1)
	c.StartPractice(func(string) { delivered <- time.Now() })
	deadline := time.Now().Add(20 * time.Second)
	for r.nonzero.Load() == 0 && time.Now().Before(deadline) {
		if c.View().Phase == session.Failed {
			panic(c.View().Message)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if r.nonzero.Load() == 0 {
		c.CancelPractice()
		panic("no nonzero PCM")
	}
	time.Sleep(time.Second)
	start := time.Now()
	c.Confirm()
	returned := time.Since(start).Milliseconds()
	c.Confirm()
	var finish, delivery time.Time
	select {
	case finish = <-r.finished:
	case <-time.After(15 * time.Second):
		panic("ASR finalize timeout")
	}
	select {
	case delivery = <-delivered:
	case <-time.After(15 * time.Second):
		panic("text delivery timeout")
	}
	atText := snapshot()
	var after map[string]any
	for deadline = time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		after = snapshot()
		out := after["output"].(map[string]any)
		old := before["output"].(map[string]any)
		if out["rate"] == old["rate"] && out["mute"] == old["mute"] && after["input"].(map[string]any)["running"] == float64(0) {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	restored := time.Since(start).Milliseconds()
	old := before["output"].(map[string]any)
	out := after["output"].(map[string]any)
	passed := returned < 500 && delivery.Sub(start) < time.Second && restored > delivery.Sub(start).Milliseconds() && out["rate"] == old["rate"] && out["mute"] == old["mute"]
	for _, k := range []string{"volume0", "volume1", "volume2"} {
		if v, ok := old[k].(float64); ok {
			a, ok := out[k].(float64)
			passed = passed && ok && a <= v+0.001
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"passed": passed, "controlledASR": true, "nonzeroSamples": r.nonzero.Load(), "confirmReturnMs": returned, "finalizeMs": finish.Sub(start).Milliseconds(), "textMs": delivery.Sub(start).Milliseconds(), "playbackRestoredMs": restored, "before": before, "atText": atText, "after": after}, "", "  ")
	fmt.Println(string(b))
	if !passed {
		os.Exit(1)
	}
}
