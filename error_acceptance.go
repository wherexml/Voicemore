package main

// Native-window acceptance: synthetic failures, real UI dispatcher and timers.
// Run only in the isolated Dev app with SEND_PREVIEW_ACCEPTANCE also set.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"voiceinput/internal/config"
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
					{"digital silence before request", session.View{Phase: session.Failed, SilentAudio: true, Message: T("errorNoAudio")}, "errorNoAudio"},
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
		for _, label := range []string{"indigo", "classic", "bubble"} {
			classic, bubble := label == "classic", label == "bubble"
			failed := session.View{Phase: session.Failed, Message: "sub2api ASR_EMPTY_RESULT (task acceptance-" + label + ")"}
			onUI(func() {
				a.overlay.syncView(failed)
				a.overlay.classic, a.overlay.bubble = classic, bubble
				a.overlay.place()
			})
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
			onUI(func() {
				results = append(results, map[string]any{"case": label + " new recording clears old error detail", "pass": a.overlay.v.Message == "" && a.overlay.panelKind() == panelNone})
			})
			time.Sleep(2400 * time.Millisecond)
			check(label+" old timer preserves new recording", true)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Idle}) })
			time.Sleep(300 * time.Millisecond)
			for _, tc := range []struct {
				name string
				view session.View
			}{
				{"clipboard", session.View{Phase: session.Done, Message: T("copiedFallback")}},
				{"no speech", session.View{Phase: session.Done, Message: T("noSpeech")}},
				{"success", session.View{Phase: session.Done}},
				{"retained text", session.View{Phase: session.Failed, Retained: true, Text: "synthetic retained text", Message: "insertion failed"}},
				{"cleanup failure", session.View{Phase: session.Failed, CleanupFailed: true, Text: "synthetic original", Message: "cleanup failed"}},
				{"save failure", session.View{Phase: session.Failed, Retained: true, Text: "synthetic unsaved text", Notice: "save failed", Message: "save failed"}},
			} {
				onUI(func() {
					a.overlay.syncView(session.View{Phase: session.Preparing})
					a.overlay.syncView(tc.view)
					a.overlay.classic, a.overlay.bubble = classic, bubble
					a.overlay.place()
				})
				time.Sleep(2500 * time.Millisecond)
				check(label+" "+tc.name+" still visible at 2.5s", true)
				onUI(func() { a.overlay.syncView(tc.view) })
				time.Sleep(time.Second)
				check(label+" "+tc.name+" entire window hidden after 3s plus fade", false)
				onUI(func() { a.overlay.syncView(tc.view) })
				check(label+" "+tc.name+" late refresh stays hidden", false)
			}
			onUI(func() {
				a.overlay.syncView(session.View{Phase: session.Preparing})
				a.overlay.syncView(session.View{Phase: session.Done, Message: T("copiedFallback")})
			})
			time.Sleep(time.Second)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Recording}) })
			time.Sleep(2400 * time.Millisecond)
			check(label+" copied timer preserves next recording", true)
			onUI(func() { a.overlay.syncView(session.View{Phase: session.Idle}) })
			time.Sleep(300 * time.Millisecond)
		}
		// Exercise the actual no-run failure path and native close dispatcher,
		// using a separate corrupt configuration, never the Dev user's settings.
		fixture, fixtureErr := os.MkdirTemp("", "voicemore-preflight-error-")
		if fixtureErr == nil {
			defer os.RemoveAll(fixture)
			fixtureErr = os.WriteFile(filepath.Join(fixture, "settings.json"), []byte("{"), 0600)
		}
		if fixtureErr == nil {
			broken, _ := config.Open(fixture)
			if broken != nil && broken.LoadError() != nil {
				var previous *session.Controller
				onUI(func() {
					previous = a.ctrl
					a.ctrl = &session.Controller{Settings: broken, OnChange: a.overlay.sync}
				})
				for _, action := range []string{"dismiss", "escape-handler"} {
					onUI(a.ctrl.ToggleTranslation)
					time.Sleep(200 * time.Millisecond)
					check("preflight "+action+" visible", true)
					onUI(func() {
						if action == "dismiss" {
							a.ctrl.Dismiss()
						} else {
							a.cancelInput()
						}
					})
					time.Sleep(500 * time.Millisecond)
					check("preflight "+action+" hides native window", false)
					onUI(func() {
						results = append(results, map[string]any{"case": "preflight " + action + " controller idle", "pass": a.ctrl.View().Phase == session.Idle})
					})
				}
				onUI(func() { a.ctrl = previous })
			} else {
				results = append(results, map[string]any{"case": "preflight fixture rejects corrupt settings", "pass": false})
			}
		} else {
			results = append(results, map[string]any{"case": "preflight fixture setup", "pass": false})
		}
		data, _ := json.MarshalIndent(results, "", "  ")
		os.WriteFile(filepath.Join(dir, "results.json"), data, 0600)
	}()
}
