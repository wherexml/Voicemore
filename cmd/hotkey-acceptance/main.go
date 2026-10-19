//go:build darwin && hotkeyacceptance

// Native tap lifecycle acceptance; never posts keys to other applications.
package main

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"os"
	"voiceinput/internal/macos"
)

func main() {
	failed := make(chan bool, 1)
	mygo.App.SetName("Voicemore Hotkey Acceptance")
	mygo.App.WhenReady(func() {
		macos.OnMain = mygo.RunOnMain
		go func() {
			result := macos.AcceptHotkeyRecovery()
			result = append(result, macos.AcceptFnInterception()...)
			json.NewEncoder(os.Stdout).Encode(result)
			bad := false
			for _, item := range result {
				bad = bad || item["passed"] != true
			}
			failed <- bad
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
	if <-failed {
		os.Exit(1)
	}
}
