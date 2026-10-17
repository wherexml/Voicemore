//go:build darwin && devicebuttonsacceptance

package main

import (
	"encoding/json"
	"github.com/egoist/mygo"
	"os"
	"time"
	"voiceinput/internal/macos"
)

func main() {
	mygo.App.SetName("Voicemore Device Capability Acceptance")
	mygo.App.WhenReady(func() {
		macos.OnMain = mygo.RunOnMain
		stop, err := macos.StartDeviceButtonMonitor(nil, "", nil)
		if err != nil {
			panic(err)
		}
		go func() {
			time.Sleep(350 * time.Millisecond)
			rows := macos.DeviceDescriptorsEvidence()
			cases := macos.DeviceCapabilityPolicyAcceptance()
			passed := true
			for _, c := range cases {
				passed = passed && c.Passed
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"passed": passed, "cases": cases, "devices": rows, "options": macos.ConnectedButtonDevices()})
			if !passed {
				panic("capability policy acceptance failed")
			}
			stop()
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}
