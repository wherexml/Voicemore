package main

// Native-window acceptance: synthetic failures, real UI dispatcher and timers.
// Run only in the isolated Dev app with SEND_PREVIEW_ACCEPTANCE also set.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"voiceinput/internal/session"
)

func (a *app) startErrorAcceptance() {
	dir := os.Getenv("VOICE_INPUT_ERROR_ACCEPTANCE")
	if dir == "" || os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") == "" {
		return
	}
	go func() {
		time.Sleep(2 * time.Second)
		results := []map[string]any{}
		onUI := func(f func()) { done := make(chan struct{}); a.overlay.win.Update(func() { f(); close(done) }); <-done }
		check := func(name string, want bool) {
			onUI(func() {
				results = append(results, map[string]any{"case": name, "pass": a.overlay.win.IsVisible() == want, "visible": a.overlay.win.IsVisible(), "at": time.Now()})
			})
		}

		onUI(func() {
			previous := langIndex.Load()
			defer setLang(int(previous))
			for language := 0; language < 3; language++ {
				setLang(language)
				for _, tc := range []struct {
					name string
					view session.View
					key  string
				}{
					{"silent audio", session.View{Phase: session.Failed, SilentAudio: true, Message: "sub2api ASR_EMPTY_RESULT (task fixture)"}, "errorNoAudio"},
					{"non-silent empty result", session.View{Phase: session.Failed, Message: "sub2api ASR_EMPTY_RESULT (task fixture)"}, "errorNoText"},
					{"unknown service error", session.View{Phase: session.Failed, SilentAudio: true, Message: "sub2api INTERNAL_ERROR (task fixture)"}, "errorDictation"},
					{"cleanup retains text", session.View{Phase: session.Failed, CleanupFailed: true, Text: "retained original", Message: "request failed: private endpoint", Notice: "request failed"}, "errorCleanup"},
				} {
					original := tc.view
					a.overlay.syncView(tc.view)
					actual := a.overlay.v
					results = append(results, map[string]any{"case": tc.name, "language": language, "message": actual.Message, "pass": actual.Message == T(tc.key) && actual.Text == original.Text && tc.view == original && (actual.Notice == "" || actual.Notice == actual.Message)})
				}
			}
			a.overlay.syncView(session.View{Phase: session.Preparing})
		})
		for _, classic := range []bool{false, true} {
			label := "indigo"
			if classic {
				label = "classic"
			}
			failed := session.View{Phase: session.Failed, Message: "sub2api ASR_EMPTY_RESULT (task acceptance-" + label + ")"}
			onUI(func() { a.overlay.syncView(failed); a.overlay.classic = classic; a.overlay.place() })
			time.Sleep(2 * time.Second)
			check(label+" visible before deadline", true)
			onUI(func() { a.overlay.syncView(failed) })
			time.Sleep(1400 * time.Millisecond)
			check(label+" hidden after deadline despite refresh", false)
			onUI(func() { a.overlay.syncView(failed) })
			check(label+" late refresh stays hidden", false)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Preparing}); a.overlay.syncView(failed) })
			time.Sleep(time.Second)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Preparing}) })
			time.Sleep(2400 * time.Millisecond)
			check(label+" old timer preserves new recording", true)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Idle}) })
			time.Sleep(300 * time.Millisecond)
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		os.WriteFile(filepath.Join(dir, "results.json"), data, 0600)
	}()
}
