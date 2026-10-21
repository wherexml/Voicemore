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
	"voiceinput/internal/macos"
	"voiceinput/internal/session"
)

func (a *app) startSendPreviewAcceptance() {
	if os.Getenv("VOICE_INPUT_UPDATE_UI_ACCEPTANCE") != "" {
		a.startUpdateUIAcceptance()
		return
	}
	if os.Getenv("VOICE_INPUT_ERROR_ACCEPTANCE") != "" {
		a.startErrorAcceptance()
		return
	}
	dir := os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE")
	if dir == "" {
		return
	}
	// Use the production Escape listener, without binding dictation or DJI inputs.
	stopEscape, escapeErr := macos.StartHotkey("", nil, nil, a.cancelInput)
	if escapeErr != nil {
		os.WriteFile(filepath.Join(dir, "escape-error.txt"), []byte(escapeErr.Error()), 0600)
	}
	go func() {
		if stopEscape != nil {
			defer stopEscape()
		}
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
					var fixtureTarget *macos.Target
					if valid {
						fixtureTarget = target.Retain()
					}
					target.Release()
					if valid {
						go func() {
							defer fixtureTarget.Release()
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
											o.closing = false
											a.sendPreview.exiting = false
											if !o.win.IsVisible() {
												o.v2 = v2State{}
											}
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
									pause := 800 * time.Millisecond
									if cmd == "flow-fast" {
										pause = 100 * time.Millisecond
									}
									time.Sleep(pause)
								}
							}
							// Scope all acceptance writes to the already captured fixture AX field.
							// Never synthesize global paste/Enter: another foreground app may be in use.
							original, readable := fixtureTarget.ReadValue()
							inserted := readable && fixtureTarget.ReplaceValue(original, "好的，我们明天下午三点见。")
							send := false
							if inserted && cmd != "input-only" && cmd != "flow-input-only" {
								send = a.sendPreview.wait(context.Background(), "好的，我们明天下午三点见。", fixtureTarget, delay, func(string) {})
							}
							b, _ := json.Marshal(map[string]any{"command": cmd, "inserted": inserted, "sendDecision": send, "inputMethod": "captured-fixture-AX", "sendKeyDispatched": false})
							os.WriteFile(filepath.Join(dir, "preview-result.json"), b, 0600)
						}()
					}
				}
			}
			a.sendPreview.win.Update(func() {
				p := a.sendPreview
				captions := []map[string]any{}
				for _, layer := range a.overlay.v2.motion.captions {
					alpha, y := layer.sample(time.Now())
					captions = append(captions, map[string]any{"text": layer.text, "alpha": alpha, "y": y, "leaving": layer.leaving})
				}
				b, _ := json.Marshal(map[string]any{"phase": a.overlay.v.Phase, "windowVisible": p.win.IsVisible(), "visible": p.visible, "pending": p.pending, "text": p.draft, "bounds": p.win.Bounds(), "window": fmt.Sprint(p.win.NativeHandle()), "overlayWindow": fmt.Sprint(a.overlay.win.NativeHandle()), "capsuleBottom": p.win.Bounds().Y + p.win.Bounds().Height - a.overlay.room,
					"exiting": p.exiting, "at": time.Now().UnixMilli(), "motion": map[string]any{
						"kind": a.overlay.v2.motion.kind, "paintAt": a.overlay.v2.motion.paintAt.UnixMilli(),
						"pill": a.overlay.v2.motion.bounds, "labelWidth": a.overlay.v2.motion.labelWidth,
						"liveWidth": a.overlay.v2.motion.liveWidth, "markWidth": a.overlay.v2.motion.markWidth,
						"toolsWidth": a.overlay.v2.motion.toolsWidth, "toolsAlpha": a.overlay.v2.motion.toolsAlpha,
						"liveAlpha": a.overlay.v2.motion.liveAlpha, "markWeights": a.overlay.v2.motion.markWeight,
						"barHeight": a.overlay.v2.motion.barHeight, "captions": captions, "opacity": a.overlay.opacity(time.Now()),
					}})
				os.WriteFile(filepath.Join(dir, "preview-state.json"), b, 0600)
				if f, err := os.OpenFile(filepath.Join(dir, "timeline.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
					f.Write(append(b, '\n'))
					f.Close()
				}
			})
			time.Sleep(20 * time.Millisecond)
		}
	}()
}
