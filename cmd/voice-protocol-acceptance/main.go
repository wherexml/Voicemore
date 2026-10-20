// Command voice-protocol-acceptance runs real loopback HTTP/WebSocket journeys
// for frozen dictionary options, failure recovery and digital silence.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"voiceinput/internal/asr"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/textstyle"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: voice-protocol-acceptance NEW_RESULT_JSON")
	}
	l, e := net.Listen("tcp", "127.0.0.1:58198")
	if e != nil {
		panic(e)
	}
	var mu sync.Mutex
	starts, bytesSent, gets := map[string]int{}, map[string]int{}, map[string]int{}
	received := map[string][]string{}
	receivedRules := map[string][]textstyle.Correction{}
	receipt := func(id string) map[string]any {
		return map[string]any{"id": id, "model": cloudasr.Sub2APIModel, "raw_text": "测试文字", "text": "测试文字", "usage_log_id": 1}
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/voice/tasks/") {
			id := strings.TrimPrefix(r.URL.Path, "/v1/voice/tasks/")
			mu.Lock()
			gets[id]++
			mu.Unlock()
			json.NewEncoder(w).Encode(receipt(id))
			return
		}
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		id := r.Header.Get("Idempotency-Key")
		mu.Lock()
		starts[id]++
		mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, b, e := c.Read(ctx)
		if e != nil {
			return
		}
		var start struct {
			Options json.RawMessage `json:"text_options"`
		}
		json.Unmarshal(b, &start)
		var opt cloudasr.VoiceTextOptions
		json.Unmarshal(start.Options, &opt)
		mu.Lock()
		received[id] = opt.Hotwords
		receivedRules[id] = opt.Corrections
		mu.Unlock()
		h := sha256.Sum256(start.Options)
		digest := hex.EncodeToString(h[:])
		if id == "unsupported" {
			digest = "legacy-server-ignored-hotwords"
		}
		send := func(v any) { b, _ := json.Marshal(v); c.Write(ctx, websocket.MessageText, b) }
		send(map[string]any{"type": "accepted", "text_options_digest": digest})
		for {
			typ, b, e := c.Read(ctx)
			if e != nil {
				return
			}
			if typ == websocket.MessageBinary {
				mu.Lock()
				bytesSent[id] += len(b)
				mu.Unlock()
				continue
			}
			if id == "recover" {
				return
			} // lose response after the simulated commit
			send(map[string]any{"type": "completed", "result": receipt(id)})
			return
		}
	})}
	go srv.Serve(l)
	defer srv.Close()
	var rows []map[string]any
	words := "mock,json"
	options := &cloudasr.VoiceTextOptions{Version: 1}
	for _, id := range []string{"words", "legacy", "unsupported", "recover", "zero", "quiet"} {
		api := cloudasr.Sub2API{Base: "http://127.0.0.1:58198", APIKey: "fixture-only", TaskID: id, TextOptions: options}
		if id == "words" {
			rules := []textstyle.Correction{{Wrong: "Helios", Term: "HeliosCode", Context: []string{"editor"}}}
			api = api.WithCorrections(rules).(cloudasr.Sub2API)
			rules[0].Term = "mutated"
			rules[0].Context[0] = "mutated"
		}
		var terms *string
		if id != "legacy" {
			terms = &words
		}
		pcm := make([]byte, 640)
		if id != "zero" {
			pcm[0] = 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, result, err := asr.TranscribeResult(ctx, api, terms, pcm)
		cancel()
		mu.Lock()
		ok := err == nil && result != nil && bytesSent[id] == len(pcm) && starts[id] == 1
		switch id {
		case "zero":
			ok = errors.Is(err, asr.ErrNoAudioSignal) && starts[id] == 0
		case "unsupported":
			ok = err != nil && bytesSent[id] == 0 && gets[id] == 0
		case "recover":
			ok = ok && gets[id] == 1
		case "words":
			ok = ok && strings.Join(received[id], ",") == words && len(options.Hotwords) == 0 && len(options.Corrections) == 0 && len(receivedRules[id]) == 1 && receivedRules[id][0].Term == "HeliosCode" && receivedRules[id][0].Context[0] == "editor"
		case "legacy":
			ok = ok && len(received[id]) == 0
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		rows = append(rows, map[string]any{"case": id, "passed": ok, "connections": starts[id], "audio_bytes": bytesSent[id], "result_gets": gets[id], "hotwords": received[id], "corrections": receivedRules[id], "error": msg})
		mu.Unlock()
	}
	b, _ := json.MarshalIndent(rows, "", "  ")
	f, e := os.OpenFile(os.Args[1], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		panic(e)
	}
	_, e = f.Write(b)
	if e != nil {
		panic(e)
	}
	f.Close()
	for _, r := range rows {
		if r["passed"] != true {
			os.Exit(1)
		}
	}
}
