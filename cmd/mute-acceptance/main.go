//go:build darwin

// Runs real microphone capture and observes macOS system output. No ASR needed.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

func snapshot() map[string]any {
	path := os.Getenv("VOICEMORE_AUDIO_PROBE")
	if path == "" {
		return nil
	}
	data, err := exec.Command(path).Output()
	if err != nil {
		panic(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func main() {
	original := script("output muted of (get volume settings)")
	defer script("set volume output muted " + original)

	results := []map[string]any{}
	for _, initial := range []string{"false", "true", "false"} {
		script("set volume output muted " + initial)
		volume := script("output volume of (get volume settings)")
		beforeAudio := snapshot()
		var frames atomic.Int64
		var nonzero atomic.Int64
		capture, err := macos.StartCapture(func(pcm []byte) {
			frames.Add(1)
			for i := 0; i+1 < len(pcm); i += 2 {
				if pcm[i] != 0 || pcm[i+1] != 0 {
					nonzero.Add(1)
				}
			}
		})
		if err != nil {
			panic(err)
		}
		func() {
			defer capture.Stop()
			time.Sleep(1500 * time.Millisecond)
			beforeMuteSamples := nonzero.Load()
			if err := capture.SilenceOutput(); err != nil {
				panic(err)
			}
			time.Sleep(1500 * time.Millisecond)
			during := script("output muted of (get volume settings)")
			deadline := time.Now().Add(3 * time.Second)
			for frames.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			_, overlapErr := macos.StartCapture(func([]byte) {})
			stopAt := time.Now()
			capture.Stop()
			stopMs := time.Since(stopAt).Milliseconds()
			capture.Stop()
			afterAudio := snapshot()
			after := script("output muted of (get volume settings)")
			afterVolume := script("output volume of (get volume settings)")
			beforeLevel, _ := strconv.Atoi(volume)
			afterLevel, _ := strconv.Atoi(afterVolume)
			passed := during == "true" && after == initial && afterLevel <= beforeLevel && frames.Load() > 0 && nonzero.Load() > beforeMuteSamples && overlapErr != nil
			if beforeAudio != nil {
				beforeOutput := beforeAudio["output"].(map[string]any)
				afterOutput := afterAudio["output"].(map[string]any)
				afterInput := afterAudio["input"].(map[string]any)
				passed = passed && beforeOutput["device"] == afterOutput["device"] && beforeOutput["rate"] == afterOutput["rate"] && afterInput["running"] == float64(0)
			}
			results = append(results, map[string]any{"beforeAudio": beforeAudio, "afterAudio": afterAudio, "stopMs": stopMs, "initialMuted": initial, "duringMuted": during, "afterMuted": after, "volumeBefore": volume, "volumeAfter": afterVolume, "beforeMuteSamples": beforeMuteSamples, "nonzeroSamples": nonzero.Load(), "pcmFrames": frames.Load(), "overlapRejected": overlapErr != nil, "passed": passed})
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
