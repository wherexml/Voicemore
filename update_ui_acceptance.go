package main

// Opt-in native updater interaction fixture. An empty Prepared cannot pass
// identity verification, so a real click exercises commit failure, never install.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"voiceinput/internal/updateclient"
)

func (a *app) startUpdateUIAcceptance() {
	dir := os.Getenv("VOICE_INPUT_UPDATE_UI_ACCEPTANCE")
	if dir == "" || os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") == "" || mygo.App.Name() != "Voicemore Dev" {
		return
	}
	go func() {
		time.Sleep(2 * time.Second)
		mygo.RunOnMain(func() {
			a.onboard.win.Hide()
			a.updates.prepared = &updateclient.Prepared{}
			a.updates.status = "updateReadyRestart"
			a.main.starting = os.Getenv("VOICE_INPUT_UPDATE_UI_BUSY") == "1"
			a.main.updateOpen = true
			a.main.show()
		})
		for range time.Tick(300 * time.Millisecond) {
			command, _ := os.ReadFile(filepath.Join(dir, "command"))
			mygo.RunOnMain(func() {
				switch string(command) {
				case "idle":
					a.main.starting = false
				case "busy":
					a.main.starting = true
				}
				data, _ := json.MarshalIndent(map[string]any{
					"status": a.updates.status, "busy": a.updateBusy(),
					"committing": a.updates.committing, "prepared": a.updates.prepared != nil,
					"restartRequested": a.updates.restartRequested,
					"detail":           a.updates.detail, "panelOpen": a.main.updateOpen,
				}, "", "  ")
				_ = os.WriteFile(filepath.Join(dir, "state.json"), data, 0600)
			})
		}
	}()
}
