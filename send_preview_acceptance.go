package main

// Opt-in native E2E driver. Only the dedicated input acceptance fixture can
// receive input, never the user's current chat. The UI and send path are real.
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func (a *app) startSendPreviewAcceptance() {
	dir := os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE")
	if dir == "" {
		return
	}
	go func() {
		last := ""
		for !a.quitting {
			data, _ := os.ReadFile(filepath.Join(dir, "preview-command"))
			cmd := string(data)
			if cmd != "" && cmd != last {
				last = cmd
				if cmd == "close" {
					a.sendPreview.win.Update(a.sendPreview.close)
				} else if cmd == "focus" {
					a.sendPreview.win.Update(func() { a.sendPreview.win.Focus() })
				} else if cmd == "inspect" {
					a.sendPreview.win.Update(func() { a.sendPreview.showRecent() })
				} else if cmd == "start" || cmd == "start-slow" || cmd == "input-only" {
					target := macos.CaptureTarget()
					b, _ := json.Marshal(map[string]any{"bundleID": target.BundleID, "command": cmd})
					os.WriteFile(filepath.Join(dir, "preview-target.json"), b, 0600)
					valid := target.BundleID == "dev.steve.voice-input.acceptance-fixture"
					target.Release()
					if valid {
						go func() {
							delay := 2 * time.Second
							if cmd == "start-slow" {
								delay = 30 * time.Second
							}
							cfg := config.DefaultAutoSend()
							cfg.Enabled = cmd != "input-only"
							res, err := macos.Insert("好的，我们明天下午三点见。", macos.InsertOptions{RestoreDelayMs: 200, AutoSend: cfg, BeforeSend: func(t *macos.Target) bool {
								return a.sendPreview.wait(context.Background(), "好的，我们明天下午三点见。", t, delay, func(string) {})
							}})
							if res.Target != nil {
								a.sendPreview.remember("好的，我们明天下午三点见。", res.Target, func(string) {})
								res.Target.Release()
								res.Target = nil
							}
							b, _ := json.Marshal(map[string]any{"result": res, "error": err})
							os.WriteFile(filepath.Join(dir, "preview-result.json"), b, 0600)
						}()
					}
				}
			}
			a.sendPreview.win.Update(func() {
				p := a.sendPreview
				b, _ := json.Marshal(map[string]any{"editing": p.editing, "visible": p.visible, "pending": p.pending, "text": p.draft, "applied": p.applied, "writable": p.writable, "bounds": p.win.Bounds(), "panelHeight": p.panelHeight})
				os.WriteFile(filepath.Join(dir, "preview-state.json"), b, 0600)
			})
			time.Sleep(100 * time.Millisecond)
		}
	}()
}
