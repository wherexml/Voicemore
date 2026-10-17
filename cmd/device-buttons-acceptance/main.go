//go:build darwin && devicebuttonsacceptance

// Native discovery acceptance. Opening a real interface requires -capture.
package main

import (
	"encoding/json"
	"flag"
	"github.com/egoist/mygo"
	"os"
	"strconv"
	"strings"
	"time"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func main() {
	capture := flag.String("capture", "", "open only this real HID interface for capture; does not inject input")
	legacy := flag.Bool("legacy", false, "exercise a saved binding with its pre-fingerprint identity")
	listen := flag.Duration("listen", 0, "wait for real selected-device button presses; never injects input")
	wanted := flag.Int("events", 1, "number of real presses to receive when listening")
	bindButtons := flag.String("bind-buttons", "", "comma-separated mouse usages for ordinary bound-button interception instead of capture")
	bindKeys := flag.String("bind-keys", "", "comma-separated page:usage pairs for selective keyboard or consumer bindings")
	stopFile := flag.String("stop-file", "", "keep interception active until this test marker exists or -listen expires")
	flag.Parse()
	if *listen > 0 && (*capture == "" || *wanted < 1) {
		panic("-listen requires -capture and positive -events")
	}
	if (*bindButtons != "" || *bindKeys != "") && (*capture == "" || *legacy) {
		panic("-bind-buttons requires -capture and cannot use -legacy")
	}
	failed := false
	mygo.App.SetName("Voicemore Device Discovery Acceptance")
	mygo.App.WhenReady(func() {
		macos.OnMain = mygo.RunOnMain
		stop, err := macos.StartDeviceButtonMonitor(nil, "", nil)
		if err != nil {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": false, "error": err.Error()})
			failed = true
			mygo.App.Quit()
			return
		}
		go func() {
			time.Sleep(350 * time.Millisecond)
			devices := macos.ButtonDevices()
			out := []map[string]any{}
			for _, d := range devices {
				out = append(out, map[string]any{"id": d.ID, "name": d.Name, "page": d.UsagePage, "usage": d.Usage, "transport": d.Transport, "error": macos.ButtonDeviceError(d)})
			}
			options := macos.ConnectedButtonDevices()
			captureError := ""
			captureFound := false
			input := make(chan macos.DeviceButtonEvent, 32)
			received := []macos.DeviceButtonEvent{}
			if *capture != "" {
				stop()
				var bindings []config.DeviceButtonBinding
				captureID := *capture
				if *bindButtons != "" || *bindKeys != "" {
					for _, d := range devices {
						if d.ID != *capture {
							continue
						}
						pairs := strings.Split(*bindButtons, ",")
						if *bindKeys != "" {
							pairs = strings.Split(*bindKeys, ",")
						}
						for _, text := range pairs {
							page := uint64(9)
							value := text
							if *bindKeys != "" {
								head, tail, ok := strings.Cut(text, ":")
								if !ok {
									panic("invalid page:usage")
								}
								var err error
								page, err = strconv.ParseUint(head, 0, 32)
								if err != nil {
									panic(err)
								}
								value = tail
							}
							usage, err := strconv.ParseUint(value, 0, 32)
							if err != nil || usage == 0 {
								panic("invalid mouse button usage")
							}
							action := "dictation"
							if usage == 5 {
								action = "translation"
							}
							bindings = append(bindings, config.DeviceButtonBinding{ID: "acceptance-" + text, Device: d, Enabled: true, Page: uint32(page), Usage: uint32(usage), Action: action})
						}
					}
					captureID = ""
				}
				if *legacy {
					for _, d := range devices {
						if d.ID == *capture {
							d.ID, _, _ = strings.Cut(d.ID, ":")
							bindings = []config.DeviceButtonBinding{{ID: "acceptance-only", Device: d, Enabled: true, Page: 7, Usage: 4, Action: "dictation"}}
						}
					}
					captureID = ""
				}
				stop, err = macos.StartDeviceButtonMonitor(bindings, captureID, func(event macos.DeviceButtonEvent) {
					select {
					case input <- event:
					default:
					}
				})
				if err != nil {
					captureError = err.Error()
				} else {
					for _, d := range macos.ButtonDevices() {
						if d.ID == *capture {
							captureFound = true
							captureError = macos.ButtonDeviceError(d)
						}
					}
				}
				failed = captureError != "" || !captureFound
			}
			if *listen > 0 && !failed {
				json.NewEncoder(os.Stdout).Encode(map[string]any{"listening": true, "capture": *capture, "expected_presses": *wanted})
				timer := time.NewTimer(*listen)
				stopPoll := time.NewTicker(50 * time.Millisecond)
			waitForInput:
				for len(received) < *wanted || *stopFile != "" {
					select {
					case event := <-input:
						if !event.Unsupported {
							received = append(received, event)
						}
					case <-stopPoll.C:
						if *stopFile != "" {
							if _, err := os.Stat(*stopFile); err == nil {
								break waitForInput
							}
						}
					case <-timer.C:
						if len(received) < *wanted {
							failed = true
							captureError = "timed out waiting for real button presses"
						}
						break waitForInput
					}
				}
				timer.Stop()
				stopPoll.Stop()
				if len(received) < *wanted {
					failed = true
					captureError = "not enough real button presses"
				}
				// A browser can act on mouse-up. Keep the real tap alive until all
				// captured presses have released; do not exit on the final down.
				releaseDeadline := time.Now().Add(3 * time.Second)
				for macos.MouseButtonInterceptionEvidence().Pending > 0 && time.Now().Before(releaseDeadline) {
					time.Sleep(10 * time.Millisecond)
				}
				if macos.MouseButtonInterceptionEvidence().Pending > 0 {
					failed = true
					captureError = "mouse button has not been released"
				}
			}
			for _, d := range macos.ButtonDevices() {
				if d.ID == *capture {
					if problem := macos.ButtonDeviceError(d); problem != "" {
						captureError = problem
						failed = true
					}
				}
			}
			interception := macos.MouseButtonInterceptionEvidence()
			if stop != nil {
				stop()
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": !failed, "capture": *capture, "bound_buttons": *bindButtons, "bound_keys": *bindKeys, "legacy_binding": *legacy, "capture_found": captureFound, "capture_error": captureError, "passive_discovery": *capture == "", "devices": out, "physical_button_tested": len(received) > 0, "events": received, "mouse_interception": interception, "selection_options": options})
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
	if failed {
		os.Exit(1)
	}
}
