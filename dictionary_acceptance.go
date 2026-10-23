package main

// An opt-in native UI journey, isolated from normal and development data.
import (
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
	"time"
)

func (a *app) startDictionaryAcceptance() {
	dir := os.Getenv("VOICE_INPUT_DICTIONARY_ACCEPTANCE")
	if !filepath.IsAbs(dir) || os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") == "" || mygo.App.Name() != "Voicemore Dev" {
		return
	}
	go func() {
		time.Sleep(2 * time.Second)
		mygo.RunOnMain(func() {
			a.onboard.win.Hide()
			a.main.goTo("dict")
			a.main.show()
		})
	}()
}
