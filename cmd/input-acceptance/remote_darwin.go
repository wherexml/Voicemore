package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

// This runs the production insertion path against a manually prepared remote
// field. A dispatched event is NOT a remote readback assertion.
func remoteAcceptance(args []string) {
	f := flag.NewFlagSet("remote", flag.ExitOnError)
	bundle := f.String("bundle", "", "required foreground client bundle ID")
	shortcut := f.String("shortcut", "cmd-v", "cmd-v or ctrl-v")
	text := f.String("text", "远程输入验收 42", "test text (never submitted)")
	delay := f.Int("delay", 1500, "clipboard synchronization wait in ms")
	cancelMs := f.Int("cancel-after", 0, "cancel during synchronization")
	output := f.String("out", "artifacts/remote-input/dispatch.json", "evidence file")
	sample := f.String("sample", "", "PCM16 mono 16kHz WAV to recognize before automatic paste")
	base := f.String("asr", "http://127.0.0.1:48132", "ASR service")
	wait := f.Duration("wait-foreground", 0, "wait for the requested client to become frontmost")
	f.Parse(args)
	if *bundle != "" && *wait > 0 {
		deadline := time.Now().Add(*wait)
		for {
			current := macos.CaptureTarget()
			ready := current.BundleID == *bundle
			current.Release()
			if ready {
				break
			}
			if time.Now().After(deadline) {
				panic("timed out waiting for foreground client; no insertion")
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	t := macos.CaptureTarget()
	defer t.Release()
	if *bundle == "" || t.BundleID != *bundle {
		panic("requested client must be frontmost")
	}
	if !macos.AccessibilityTrusted(false) {
		panic("acceptance process lacks Accessibility permission")
	}
	if *shortcut != "cmd-v" && *shortcut != "ctrl-v" {
		panic("invalid shortcut")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *cancelMs > 0 {
		timer := time.AfterFunc(time.Duration(*cancelMs)*time.Millisecond, cancel)
		defer timer.Stop()
	}
	start := time.Now()
	sampleHash := ""
	if *sample != "" {
		audio, err := os.ReadFile(*sample)
		if err != nil {
			panic(err)
		}
		sampleHash = fmt.Sprintf("%x", sha256.Sum256(audio))
		if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
			panic("invalid WAV")
		}
		var pcm []byte
		valid := false
		for p := 12; p+8 <= len(audio); {
			n := int(binary.LittleEndian.Uint32(audio[p+4:]))
			start := p + 8
			if n > len(audio)-start {
				panic("truncated WAV")
			}
			chunk := audio[start : start+n]
			switch string(audio[p : p+4]) {
			case "fmt ":
				valid = n >= 16 && binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 && binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
			case "data":
				pcm = chunk
			}
			p = start + n + n%2
		}
		if !valid || len(pcm) == 0 {
			panic("requires PCM16 mono 16kHz WAV")
		}
		asrCtx, stop := context.WithTimeout(ctx, 90*time.Second)
		defer stop()
		stream, err := asr.Dial(asrCtx, *base, nil)
		if err != nil {
			panic(err)
		}
		defer stream.Close()
		for p := 0; p < len(pcm); p += asr.ChunkBytes {
			end := min(p+asr.ChunkBytes, len(pcm))
			if err = stream.Send(asrCtx, pcm[p:end]); err != nil {
				panic(err)
			}
			time.Sleep(time.Duration(end-p) * time.Second / 32000)
		}
		if err = stream.Finish(asrCtx); err != nil {
			panic(err)
		}
		*text = stream.Transcript.Final()
		if *text == "" {
			panic("empty ASR transcript; no paste")
		}
		now := macos.CaptureTarget()
		same := now.BundleID == *bundle
		now.Release()
		if !same {
			panic("foreground changed during recognition; no paste")
		}
	}

	res, err := macos.Insert(*text, macos.InsertOptions{Context: ctx, RemoteInputs: []config.RemoteInput{{BundleID: *bundle, Shortcut: *shortcut, SyncDelayMs: *delay}}})
	defer res.Target.Release()
	b, _ := json.MarshalIndent(map[string]any{"sample": *sample, "sampleSHA256": sampleHash, "asr": *base, "client": *bundle, "shortcut": *shortcut, "text": *text, "delayMs": *delay, "elapsedMs": time.Since(start).Milliseconds(), "result": res, "error": fmt.Sprint(err), "remoteReadback": "must be verified on remote screen separately"}, "", "  ")
	if err := os.WriteFile(*output, b, 0600); err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
