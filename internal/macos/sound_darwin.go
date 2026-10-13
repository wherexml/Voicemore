//go:build darwin

package macos

import (
	_ "embed"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// Short, original soft fixed-pitch cues embedded in the binary.
//
//go:embed sounds/start.wav
var startCue []byte

//go:embed sounds/stop.wav
var stopCue []byte

// Accessed only on the AppKit main thread; retained for the app's lifetime.
var recordingSounds [2]objc.ID

// PlayRecordingCue plays without activating a window or changing system volume.
func PlayRecordingCue(start bool) {
	if load() != nil {
		return
	}
	OnMain(func() {
		i, wav := 1, stopCue
		if start {
			i, wav = 0, startCue
		}
		if recordingSounds[i] == 0 {
			data := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&wav[0]), uintptr(len(wav)))
			recordingSounds[i] = class("NSSound").Send(sel("alloc")).Send(sel("initWithData:"), data)
			runtime.KeepAlive(wav)
		}
		// A very fast toggle should never overlap two cues.
		for _, sound := range recordingSounds {
			if sound != 0 {
				sound.Send(sel("stop"))
			}
		}
		if sound := recordingSounds[i]; sound != 0 {
			objc.Send[bool](sound, sel("play"))
		}
	})
}
