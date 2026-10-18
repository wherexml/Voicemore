//go:build darwin && integrityacceptance

// Visible native fixture. Drive through CUA; this program never posts keys.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func main() {
	output := flag.String("output", "artifacts/review-fixes-20261017/native", "fresh isolated directory")
	flag.Parse()
	root, err := filepath.Abs(*output)
	if err != nil {
		panic(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		panic("output must be fresh")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		panic(err)
	}
	settings, err := config.Open(root)
	if err != nil {
		panic(err)
	}
	if err = settings.Set(settings.Get()); err != nil {
		panic(err)
	}
	mygo.App.SetName("Voicemore Integrity Acceptance")
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(root, "ui-data"))
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	mygo.App.OnWindowAllClosed(func() { mygo.App.Quit() })
	mygo.App.WhenReady(func() {
		macos.OnMain = mygo.RunOnMain
		rows := []map[string]any{}
		status, preview := "Ready", ""
		var stop func()
		var win *mygo.Window
		save := func() {
			b, _ := json.MarshalIndent(rows, "", "  ")
			if err := os.WriteFile(filepath.Join(root, "results.json"), b, 0600); err != nil {
				panic(err)
			}
		}
		begin := func() {
			if stop != nil {
				stop()
				stop = nil
			}
			status, preview = "Recording", ""
			before := settings.Get().TriggerKey
			var err error
			stop, err = macos.CaptureShortcut(win.NativeHandle(), func(value string) { preview = value; win.Invalidate() }, func(value string) {
				if stop != nil {
					stop()
					stop = nil
				}
				outcome := "cancelled"
				var saveErr error
				if value != "" {
					saveErr = config.ValidateShortcut(value)
					if saveErr == nil {
						v := settings.Get()
						v.TriggerKey = value
						saveErr = settings.Set(v)
					}
					outcome = "saved"
					if saveErr != nil {
						outcome = "rejected"
					}
				}
				status = outcome + ": " + value
				rows = append(rows, map[string]any{"captured": value, "before": before, "saved": settings.Get().TriggerKey, "outcome": outcome, "error": fmt.Sprint(saveErr)})
				save()
				win.Invalidate()
			})
			if err != nil {
				status = err.Error()
			}
			win.Invalidate()
		}
		win = mygo.NewWindow(mygo.WindowOptions{Title: "Shortcut recording acceptance", Width: 600, Height: 300, DisableMaximize: true, Content: ui.View(func(c *ui.Context) {
			ui.Column(c).Padding(24).Gap(16).Children(func() {
				ui.Text(c, "Shortcut recording acceptance").FontSize(20)
				ui.Text(c, "Current: "+settings.Get().TriggerKey)
				ui.Text(c, status)
				ui.Text(c, preview)
				if ui.Button(c.Key("record"), "Record shortcut").Clicked() {
					begin()
				}
				if ui.Button(c.Key("finish"), "Finish verification").Clicked() {
					if stop != nil {
						stop()
						stop = nil
					}
					err := mygo.GlobalShortcut.Register("Cmd+Shift+F18", func() {})
					rows = append(rows, map[string]any{"case": "safe accelerator registers with macOS", "passed": err == nil, "error": fmt.Sprint(err)})
					if err == nil {
						mygo.GlobalShortcut.Unregister("Cmd+Shift+F18")
					}
					reopened, openErr := config.Open(root)
					rows = append(rows, map[string]any{"case": "native recorded key survives reopening", "passed": openErr == nil && reopened.Get().TriggerKey == settings.Get().TriggerKey, "error": fmt.Sprint(openErr)})
					save()
					mygo.App.Quit()
				}
			})
		})})
		win.Show()
		win.Focus()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}
