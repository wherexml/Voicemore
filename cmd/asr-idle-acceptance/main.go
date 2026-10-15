// Real worker/HTTP/WebSocket acceptance. Uses a disposable fixture and a private port.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/localasr"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	source := flag.String("source", "", "installed local-asr directory")
	sample := flag.String("sample", "", "16 kHz mono PCM16 WAV")
	out := flag.String("out", "artifacts/asr-idle", "evidence directory")
	flag.Parse()
	must(os.MkdirAll(*out, 0700))
	root, err := os.MkdirTemp(*out, "fixture-")
	must(err)
	root, err = filepath.Abs(root)
	must(err)
	defer os.RemoveAll(root)
	// Only reuse immutable runtime/weights; worker files and logs stay isolated.
	for _, name := range []string{"runtime", "model"} {
		must(os.Symlink(filepath.Join(*source, name), filepath.Join(root, name)))
	}
	b, err := os.ReadFile(filepath.Join(*source, "installed.json"))
	must(err)
	must(os.WriteFile(filepath.Join(root, "installed.json"), b, 0600))
	m := localasr.New(root, "", 48134)
	defer m.Close()
	m.SetIdleTimeout(2 * time.Second)
	rows := []map[string]any{}
	record := func(name string, ok bool, details any) {
		rows = append(rows, map[string]any{"name": name, "passed": ok, "details": details, "state": m.Snapshot()})
		b, _ := json.MarshalIndent(rows, "", "  ")
		must(os.WriteFile(filepath.Join(*out, "report.json"), b, 0600))
		fmt.Println(name, ok, details)
		if !ok {
			panic(name)
		}
	}
	waitStopped := func() {
		end := time.Now().Add(12 * time.Second)
		for time.Now().Before(end) {
			if m.Snapshot().Phase == "stopped" {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		panic("idle worker did not stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := time.Now()
	release, err := m.Acquire(ctx)
	must(err)
	record("cold_load", true, time.Since(started).Seconds())
	pid := m.Snapshot().PID
	time.Sleep(2500 * time.Millisecond)
	record("active_lease_survives_idle_deadline", m.Snapshot().Phase == "ready" && m.Snapshot().PID == pid, pid)
	rss, _ := exec.Command("ps", "-o", "rss=", "-p", fmt.Sprint(pid)).Output()
	record("ready_rss_kib", true, strings.TrimSpace(string(rss)))
	release()
	time.Sleep(time.Second)
	started = time.Now()
	release, err = m.Acquire(ctx)
	must(err)
	record("warm_acquire", m.Snapshot().PID == pid, time.Since(started).Seconds())
	release()
	release() // release must be idempotent
	waitStopped()
	record("idle_releases_process", m.Snapshot().PID == 0 && syscall.Kill(pid, 0) != nil, pid)
	started = time.Now()
	release, err = m.Acquire(ctx)
	must(err)
	record("reload_after_idle", m.Snapshot().PID != pid, time.Since(started).Seconds())
	b, err = os.ReadFile(*sample)
	must(err)
	var pcm []byte
	formatOK := false
	if len(b) < 12 || string(b[:4]) != "RIFF" {
		panic("WAV required")
	}
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if i+8+n > len(b) {
			panic("invalid WAV")
		}
		if string(b[i:i+4]) == "fmt " && n >= 16 {
			chunk := b[i+8 : i+8+n]
			formatOK = binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 && binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		}
		if string(b[i:i+4]) == "data" {
			pcm = b[i+8 : i+8+n]
		}
		i += 8 + n + n%2
	}
	if !formatOK || len(pcm) == 0 {
		panic("empty WAV")
	}
	text, err := asr.Transcribe(ctx, asr.Local(m.Address()), nil, pcm)
	must(err)
	record("real_transcription_after_reload", strings.TrimSpace(text) != "", text)
	release()
	waitStopped()
	// The actual history controller must also acquire/release the sleeping model.
	data := filepath.Join(root, "history")
	must(os.MkdirAll(data, 0700))
	cfg, err := config.Open(data)
	must(err)
	settings := cfg.Get()
	settings.ASRURL = m.Address()
	must(cfg.Set(settings))
	history, err := store.Open(data)
	must(err)
	dictionary, err := dict.Open(data)
	must(err)
	id := store.NewID()
	must(history.WriteWAV(id+".wav", pcm))
	must(history.Add(store.Record{ID: id, StartedAt: time.Now(), AudioFile: id + ".wav", RawText: text, Status: store.StatusNotInserted}))
	controller := &session.Controller{Settings: cfg, History: history, Dict: dictionary,
		PrepareASR: func(ctx context.Context, _ config.Settings) (func(), error) { return m.Acquire(ctx) }}
	must(controller.Retranscribe(id))
	saved, ok := history.Get(id)
	record("controller_retranscribes_from_standby", ok && len(saved.Versions) == 1 && strings.TrimSpace(saved.Versions[0].Text) != "", saved.Versions)
	waitStopped()
	release, err = m.Acquire(ctx)
	must(err)
	pid = m.Snapshot().PID
	release()
	m.Stop()
	release, err = m.Acquire(ctx)
	must(err)
	record("acquire_during_shutdown_restarts", m.Snapshot().Phase == "ready" && m.Snapshot().PID != pid, m.Snapshot().PID)
	release()
	waitStopped()
	// Cancellation during cold loading returns promptly and eventually frees the worker.
	short, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	started = time.Now()
	_, err = m.Acquire(short)
	stop()
	record("cancel_load_returns_promptly", err != nil && time.Since(started) < time.Second, fmt.Sprint(err))
	end := time.Now().Add(60 * time.Second)
	for time.Now().Before(end) {
		s := m.Snapshot()
		if s.Phase == "stopped" {
			break
		}
		if s.Phase == "error" {
			panic(s.Detail)
		}
		time.Sleep(50 * time.Millisecond)
	}
	record("cancelled_load_eventually_unloads", m.Snapshot().Phase == "stopped" && m.Snapshot().PID == 0, nil)
	m.Close()
	_, err = m.Acquire(ctx)
	record("closed_manager_cannot_restart", err != nil && m.Snapshot().PID == 0, fmt.Sprint(err))
	b, err = os.ReadFile(filepath.Join(root, "worker.log"))
	must(err)
	must(os.WriteFile(filepath.Join(*out, "worker.log"), b, 0600))
}
