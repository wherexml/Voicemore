//go:build dictationacceptance

// Focused acceptance over real HTTP sockets and the production controller.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/session"
)

func main() {
	live := flag.Bool("live", false, "also call the current configured text model with synthetic transcripts")
	flag.Parse()
	rows := []map[string]any{}
	migrationDir, e := os.MkdirTemp("", "voicemore-controls-migration-")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(migrationDir)
	fresh, e := config.Open(migrationDir)
	rows = append(rows, map[string]any{"case": "new install starts with no device binding", "passed": e == nil && !fresh.Get().DJIButtonEnabled})
	_ = config.WriteJSON(filepath.Join(migrationDir, "settings.json"), map[string]any{"schemaVersion": 5, "triggerKey": "Fn", "triggerMode": "hold"})
	legacy, e := config.Open(migrationDir)
	legacySettings := legacy.Get()
	rows = append(rows, map[string]any{"case": "legacy install preserves existing DJI and keyboard binding", "passed": e == nil && legacySettings.DJIButtonEnabled && legacySettings.TriggerKey == "Fn" && legacySettings.TriggerMode == "hold"})
	legacySettings.DJIButtonEnabled = false
	legacySettings.TranslationKey = "Ctrl+Shift+K"
	_ = legacy.Set(legacySettings)
	reopened, e := config.Open(migrationDir)
	rows = append(rows, map[string]any{"case": "removed DJI and custom translation survive reopening", "passed": e == nil && !reopened.Get().DJIButtonEnabled && reopened.Get().TranslationKey == "Ctrl+Shift+K"})

	// Temporary-file persistence acceptance for multiple real-device descriptors.
	generic := reopened.Get()
	generic.DeviceButtons = []config.DeviceButtonBinding{
		{ID: "one", Device: config.ButtonDevice{ID: "usb-one", Name: "USB controller", VendorID: 1, ProductID: 2, UsagePage: 12, Usage: 1}, Page: 12, Usage: 233, Enabled: true, Action: "dictation"},
		{ID: "two", Device: config.ButtonDevice{ID: "usb-two", Name: "USB controller 2", VendorID: 3, ProductID: 4, UsagePage: 12, Usage: 1}, Page: 12, Usage: 205, Enabled: true, Action: "translation"},
	}
	_ = reopened.Set(generic)
	multi, e := config.Open(migrationDir)
	rows = append(rows, map[string]any{"case": "two device button bindings and translation persist", "passed": e == nil && len(multi.Get().DeviceButtons) == 2 && multi.Get().HasDeviceTranslation()})
	isolated := multi.Get()
	isolated.DeviceButtons[0].Enabled = false
	rows = append(rows, map[string]any{"case": "device binding snapshots do not mutate store", "passed": multi.Get().DeviceButtons[0].Enabled})
	isolated.DeviceButtons = isolated.DeviceButtons[:1]
	_ = multi.Set(isolated)
	removed, e := config.Open(migrationDir)
	rows = append(rows, map[string]any{"case": "disable and remove preserve remaining binding", "passed": e == nil && len(removed.Get().DeviceButtons) == 1 && !removed.Get().DeviceButtons[0].Enabled && !removed.Get().HasDeviceTranslation()})

	// Same-interface mouse buttons use independent records and actions. All
	// mutations below are in this temporary store, never the user's settings.
	mouse := config.ButtonDevice{ID: "acceptance-mouse", Name: "Acceptance mouse", VendorID: 1, ProductID: 2, UsagePage: 1, Usage: 2}
	one := config.DeviceButtonBinding{ID: "mouse-four", Device: mouse, Page: 9, Usage: 4, Enabled: true, Action: "dictation"}
	two := config.DeviceButtonBinding{ID: "mouse-five", Device: mouse, Page: 9, Usage: 5, Enabled: true, Action: "translation"}
	same := removed.Get()
	same.DeviceButtons = nil
	same.PutDeviceButton(one)
	same.PutDeviceButton(two)
	writeErr := removed.Set(same)
	sameStore, openErr := config.Open(migrationDir)
	same = sameStore.Get()
	rows = append(rows, map[string]any{"case": "same mouse two buttons persist with dictation and translation", "passed": writeErr == nil && openErr == nil && len(same.DeviceButtons) == 2 && same.DeviceButtons[0] == one && same.DeviceButtons[1] == two && same.HasDeviceTranslation()})
	other := mouse
	other.ID = "another-mouse"
	rows = append(rows, map[string]any{"case": "duplicate guard scopes to device and button and permits editing itself", "passed": same.DeviceButtonTaken(mouse, 9, 4, "") && same.DeviceButtonTaken(mouse, 9, 5, "") && !same.DeviceButtonTaken(mouse, 9, 4, one.ID) && !same.DeviceButtonTaken(mouse, 9, 6, "") && !same.DeviceButtonTaken(other, 9, 4, "") && !same.DeviceButtonTaken(mouse, 12, 4, "")})
	one.Enabled = false
	same.PutDeviceButton(one)
	disabledTaken := same.DeviceButtonTaken(mouse, 9, 4, "")
	same.RemoveDeviceButton(one.ID)
	writeErr = sameStore.Set(same)
	finalMouse, openErr := config.Open(migrationDir)
	rows = append(rows, map[string]any{"case": "disabling and removing one mouse button preserves the other translation", "passed": disabledTaken && writeErr == nil && openErr == nil && len(finalMouse.Get().DeviceButtons) == 1 && finalMouse.Get().DeviceButtons[0] == two && finalMouse.Get().HasDeviceTranslation()})

	var mu sync.Mutex
	requests := []map[string]any{}
	l, err := net.Listen("tcp", "127.0.0.1:58193")
	if err != nil {
		panic(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
			Thinking *bool                            `json:"enable_thinking"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompt := ""
		if len(body.Messages) > 0 {
			prompt = body.Messages[0].Content
		}
		mu.Lock()
		requests = append(requests, map[string]any{"model": body.Model, "translationPrompt": strings.HasPrefix(prompt, "Translate the user-provided transcript into English."), "thinkingDisabled": body.Thinking != nil && !*body.Thinking})
		mu.Unlock()
		switch body.Model {
		case "fixture-error":
			w.WriteHeader(503)
			return
		case "fixture-slow":
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Fix 2 issues before 15:00 Thursday; do not deploy today."}}]}`)
	})}
	go srv.Serve(l)
	rows = append(rows, session.AcceptTranslationPipeline("http://127.0.0.1:58193/v1")...)
	srv.Close()
	mu.Lock()
	protocol := len(requests) > 0
	for _, r := range requests {
		protocol = protocol && r["translationPrompt"] == true && r["thinkingDisabled"] == true
	}
	mu.Unlock()
	rows = append(rows, map[string]any{"case": "translation prompt and thinking protocol", "passed": protocol, "requests": requests})
	if *live {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, "Library/Application Support/Voice Input")
		settings, e := config.Open(dir)
		if e != nil {
			panic(e)
		}
		vault, e := secrets.Open(dir)
		if e != nil {
			panic(e)
		}
		opts, e := providers.CleanupOptions(settings.Get(), func(account string) string { v, _ := vault.Get(account); return v })
		if e != nil {
			panic(e)
		}
		for _, target := range []string{"English", "Japanese"} {
			input := "请在周四15点前修复2个问题，不要今天上线。文件名是README.md。"
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			began := time.Now()
			output, e := cleanup.Translate(ctx, opts, input, target)
			cancel()
			row := map[string]any{"case": "live translation to " + target, "passed": e == nil && output != "", "model": opts.Model, "input": input, "output": output, "milliseconds": time.Since(began).Milliseconds()}
			if e != nil {
				row["error"] = e.Error()
			}
			rows = append(rows, row)
		}
	}
	json.NewEncoder(os.Stdout).Encode(rows)
	for _, r := range rows {
		if r["passed"] != true {
			os.Exit(1)
		}
	}
}
