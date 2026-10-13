// Command acceptance exercises the real local ASR, history and dictionary path.
// It writes repeatable evidence and never records the microphone or pastes text.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"voiceinput/internal/asr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

type event struct {
	AtMS    int64  `json:"atMs"`
	Final   string `json:"final"`
	Pending string `json:"pending"`
}
type result struct {
	Name       string  `json:"name"`
	Passed     bool    `json:"passed"`
	Text       string  `json:"text,omitempty"`
	Error      string  `json:"error,omitempty"`
	DurationMS int64   `json:"durationMs"`
	Events     []event `json:"events,omitempty"`
}

func normalized(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	sample := flag.String("sample", "", "public 16kHz mono PCM16 WAV")
	reference := flag.String("reference", "甚至出现交易几乎停滞的情况", "expected sample transcript")
	output := flag.String("out", "artifacts/acceptance-20261013/pipeline", "evidence directory (new or existing)")
	base := flag.String("asr", "http://127.0.0.1:48131", "local ASR base URL")
	flag.Parse()
	must(os.MkdirAll(*output, 0700))
	b, err := os.ReadFile(*sample)
	must(err)
	// Accept only a real PCM16/16kHz/mono WAV, walking RIFF chunks.
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		panic("invalid WAV")
	}
	var pcm []byte
	formatOK := false
	for p := 12; p+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[p+4:]))
		start := p + 8
		if n < 0 || start+n > len(b) {
			panic("truncated WAV")
		}
		chunk := b[start : start+n]
		switch string(b[p : p+4]) {
		case "fmt ":
			formatOK = n >= 16 && binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 && binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		case "data":
			pcm = chunk
		}
		p = start + n + n%2
	}
	if !formatOK || len(pcm) == 0 || len(pcm)%2 != 0 {
		panic("expected PCM16 mono 16000 Hz")
	}
	must(os.WriteFile(filepath.Join(*output, "sample.wav"), b, 0600))
	var results []result
	run := func(name string, audio []byte, want string) string {
		start := time.Now()
		r := result{Name: name}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		s, e := asr.Dial(ctx, *base, nil)
		if e == nil {
			for p := 0; p < len(audio) && e == nil; p += asr.ChunkBytes {
				end := min(p+asr.ChunkBytes, len(audio))
				e = s.Send(ctx, audio[p:end])
				time.Sleep(time.Duration(end-p) * time.Second / 32000)
				r.Events = append(r.Events, event{time.Since(start).Milliseconds(), s.Transcript.Final(), s.Transcript.Pending()})
			}
			if e == nil {
				e = s.Finish(ctx)
			}
			r.Text = s.Transcript.Final()
			s.Close()
		}
		r.DurationMS = time.Since(start).Milliseconds()
		r.Passed = e == nil && normalized(r.Text) == normalized(want)
		if e != nil {
			r.Error = e.Error()
		}
		results = append(results, r)
		return r.Text
	}
	raw := run("public_speech_realtime", pcm, *reference)
	run("silence_realtime", make([]byte, 32000*3), "")
	dir := filepath.Join(*output, "data")
	must(os.MkdirAll(dir, 0700))
	h, err := store.Open(dir)
	must(err)
	cfg, err := config.Open(dir)
	must(err)
	settings := cfg.Get()
	settings.ASRURL = *base
	must(cfg.Set(settings))
	d, err := dict.Open(dir)
	must(err)
	id := store.NewID()
	must(h.WriteWAV(id+".wav", pcm))
	must(h.Add(store.Record{ID: id, StartedAt: time.Now(), DurationMs: int64(len(pcm)) * 1000 / 32000, Status: store.StatusNotInserted, StatusDetail: "Acceptance fixture; no insertion attempted", RawText: raw, AudioFile: id + ".wav"}))
	c := &session.Controller{Settings: cfg, History: h, Dict: d}
	start := time.Now()
	err = c.Retranscribe(id)
	rr, _ := h.Get(id)
	r := result{Name: "history_retranscription", Passed: err == nil && len(rr.Versions) == 1 && normalized(rr.Versions[0].Text) == normalized(raw), DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		r.Error = err.Error()
	}
	results = append(results, r)
	reopened, err := store.Open(dir)
	must(err)
	reread, ok := reopened.Get(id)
	saved, err := reopened.ReadPCM(reread.AudioFile)
	results = append(results, result{Name: "history_audio_roundtrip", Passed: ok && err == nil && reread.RawText == raw && sha256.Sum256(saved) == sha256.Sum256(pcm)})
	n, _ := d.Propose("请使用 Cloud 帮我检查这个函数的实现。", "请使用 Claude 帮我检查这个函数的实现。", "history-revision", id)
	pending := d.Pending()
	pass := n == 1 && len(pending) == 1
	if pass {
		x := pending[0]
		before, _ := d.Apply("Cloud 帮我检查函数")
		e := d.Accept(x.ID, dict.KindRule, x.Wrong, x.Right, []string{"函数"})
		after, _ := d.Apply("Cloud 帮我检查函数")
		negative, _ := d.Apply("The cloud is white")
		pass = e == nil && before == "Cloud 帮我检查函数" && after == "Claude 帮我检查函数" && negative == "The cloud is white"
	}
	results = append(results, result{Name: "confirmed_dictionary_positive_negative", Passed: pass})
	// Exercise automatic dictionary hints through the real service and controller,
	// without configuring any separate hotword setting.
	start = time.Now()
	err = c.Retranscribe(id)
	withHints, _ := h.Get(id)
	hintPass := err == nil && len(withHints.Versions) == 2
	if hintPass {
		v := withHints.Versions[1]
		hintPass = normalized(v.Text) == normalized(raw) && strings.Contains(v.Note, "1 dictionary terms")
	}
	hr := result{Name: "automatic_dictionary_retranscription", Passed: hintPass, DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		hr.Error = err.Error()
	}
	results = append(results, hr)

	all := true
	for _, r := range results {
		all = all && r.Passed
		fmt.Printf("%s: %t %s\n", r.Name, r.Passed, r.Text)
	}
	report := map[string]any{"passed": all, "asr": *base, "sample": *sample, "sampleSHA256": fmt.Sprintf("%x", sha256.Sum256(b)), "reference": *reference, "results": results, "scope": "Real ASR over WebSocket, production client/controller, history/audio persistence and dictionary; not microphone/Fn/paste/UI acceptance"}
	out, err := json.MarshalIndent(report, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*output, "report.json"), out, 0600))
	if !all {
		os.Exit(1)
	}
}
