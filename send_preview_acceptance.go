package main

// Opt-in native E2E driver. Only the dedicated input acceptance fixture can
// receive input, never the user's current chat. The UI and send path are real.
import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"strings"
	"time"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/session"
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
				} else if cmd == "start" || cmd == "start-slow" || cmd == "input-only" || strings.HasPrefix(cmd, "flow") {
					target := macos.CaptureTarget()
					b, _ := json.Marshal(map[string]any{"bundleID": target.BundleID, "command": cmd})
					os.WriteFile(filepath.Join(dir, "preview-target.json"), b, 0600)
					valid := target.BundleID == "dev.steve.voice-input.acceptance-fixture"
					target.Release()
					if valid {
						go func() {
							delay := 2 * time.Second
							if cmd == "start-slow" || cmd == "flow-slow" {
								delay = 30 * time.Second
							}
							if strings.HasPrefix(cmd, "flow") {
								for _, phase := range []session.Phase{session.Preparing, session.Recording, session.Finalizing, session.Cleaning, session.Inserting} {
									done := make(chan struct{})
									a.overlay.win.Update(func() {
										o := a.overlay
										o.v = session.View{Phase: phase, Final: "好的，我们明天下午三点见。"}
										if phase == session.Preparing {
											o.sendFinished = false
											o.classic = false
											o.expanded = false
											o.area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
											o.fade(1)
											macos.ConfigureOverlay(o.win.NativeHandle())
										}
										o.place()
										o.win.ShowInactive()
										close(done)
									})
									<-done
									time.Sleep(800 * time.Millisecond)
								}
							}
							cfg := config.DefaultAutoSend()
							cfg.Enabled = cmd != "input-only"
							res, err := macos.Insert("好的，我们明天下午三点见。", macos.InsertOptions{RestoreDelayMs: 200, AutoSend: cfg, BeforeSend: func(t *macos.Target) bool {
								return a.sendPreview.wait(context.Background(), "好的，我们明天下午三点见。", t, delay, func(string) {})
							}})
							if res.Target != nil {
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
				b, _ := json.Marshal(map[string]any{"phase": a.overlay.v.Phase, "windowVisible": p.win.IsVisible(), "visible": p.visible, "pending": p.pending, "text": p.draft, "bounds": p.win.Bounds(), "window": fmt.Sprint(p.win.NativeHandle()), "overlayWindow": fmt.Sprint(a.overlay.win.NativeHandle()), "capsuleBottom": p.win.Bounds().Y + p.win.Bounds().Height - a.overlay.room})
				os.WriteFile(filepath.Join(dir, "preview-state.json"), b, 0600)
				if f, err := os.OpenFile(filepath.Join(dir, "timeline.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
					f.Write(append(b, '\n'))
					f.Close()
				}
			})
			time.Sleep(100 * time.Millisecond)
		}
	}()
}
