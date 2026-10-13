//go:build darwin

// Runs real microphone capture and observes macOS system output. No ASR needed.
package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"voiceinput/internal/macos"
)

func script(source string) string {
	b, err := exec.Command("/usr/bin/osascript", "-e", source).CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("%s: %v", b, err))
	}
	return strings.TrimSpace(string(b))
}

func main() {
	original := script("output muted of (get volume settings)")
	defer script("set volume output muted " + original)
	volume := script("output volume of (get volume settings)")
	results := []map[string]any{}
	for _, initial := range []string{"false", "true", "false"} {
		script("set volume output muted " + initial)
		var frames atomic.Int64
		capture, err := macos.StartCapture(func(pcm []byte) { frames.Add(1) })
		if err != nil {
			panic(err)
		}
		func() {
			defer capture.Stop()
			during := script("output muted of (get volume settings)")
			deadline := time.Now().Add(3 * time.Second)
			for frames.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			_, overlapErr := macos.StartCapture(func([]byte) {})
			capture.Stop()
			capture.Stop()
			after := script("output muted of (get volume settings)")
			afterVolume := script("output volume of (get volume settings)")
			passed := during == "true" && after == initial && volume == afterVolume && frames.Load() > 0 && overlapErr != nil
			results = append(results, map[string]any{"initialMuted": initial, "duringMuted": during, "afterMuted": after, "volumeBefore": volume, "volumeAfter": afterVolume, "pcmFrames": frames.Load(), "overlapRejected": overlapErr != nil, "passed": passed})
		}()
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(b))
	for _, result := range results {
		if result["passed"] != true {
			panic("acceptance failed")
		}
	}
}
