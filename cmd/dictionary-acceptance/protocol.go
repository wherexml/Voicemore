//go:build darwin

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"voiceinput/internal/asr"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/dict"
)

type loopbackTransport struct{ base http.RoundTripper }

func (t loopbackTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Only the acceptance process is redirected; no configured credentials used.
	if r.URL.Host != "openspeech.bytedance.com" {
		return nil, fmt.Errorf("unexpected host")
	}
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = "http"
	u.Host = "127.0.0.1:54837"
	copy.URL = &u
	copy.Host = u.Host
	return t.base.RoundTrip(copy)
}
func protocol(out string) {
	listener, err := net.Listen("tcp", "127.0.0.1:54837")
	must(err)
	requests := make(chan map[string]any, 4)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if len(data) < 8 || data[1]>>4 != 1 {
			return
		}
		reader, err := gzip.NewReader(bytes.NewReader(data[8:]))
		if err != nil {
			return
		}
		payload, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return
		}
		var req map[string]any
		if json.Unmarshal(payload, &req) != nil {
			return
		}
		requests <- req
		response := func(last bool) error {
			payload := []byte(`{"result":{"text":"fixture transcript"}}`)
			flags := byte(0)
			if last {
				flags = 2
			}
			frame := []byte{0x11, 0x90 | flags, 0x10, 0}
			frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
			frame = append(frame, payload...)
			return conn.Write(ctx, websocket.MessageBinary, frame)
		}
		if response(false) != nil {
			return
		}
		for {
			_, data, err = conn.Read(ctx)
			if err != nil {
				return
			}
			if len(data) >= 8 && data[1]>>4 == 2 && data[1]&2 != 0 {
				response(true)
				return
			}
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = loopbackTransport{base: http.DefaultTransport}
	defer func() { http.DefaultClient.Transport = old }()
	tmp, err := os.MkdirTemp(out, "protocol-dict-")
	must(err)
	d, err := dict.Open(tmp)
	must(err)
	for _, word := range []string{"词汇", "quoted\"word", "mock"} {
		must(d.Add(dict.Entry{Kind: dict.KindTerm, Term: word, Source: "acceptance"}))
	}
	terms, _, err := d.HotwordParam()
	must(err)
	empty := ""
	results := []map[string]any{}
	for _, tc := range []struct {
		name  string
		terms *string
	}{{"nil", nil}, {"empty", &empty}, {"dictionary", &terms}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		text, err := asr.Transcribe(ctx, cloudasr.Volc{APIKey: "acceptance-only"}, tc.terms, make([]byte, 6400))
		cancel()
		must(err)
		req := <-requests
		options := req["request"].(map[string]any)
		passed := text == "fixture transcript"
		if tc.name == "dictionary" {
			raw, ok := options["corpus"].(map[string]any)["context"].(string)
			passed = passed && ok && options["enable_nonstream"] == true
			var context struct {
				Hotwords []struct {
					Word string `json:"word"`
				} `json:"hotwords"`
			}
			must(json.Unmarshal([]byte(raw), &context))
			var words []string
			for _, w := range context.Hotwords {
				words = append(words, w.Word)
			}
			passed = passed && strings.Join(words, ",") == "mock,quoted\"word,词汇"
		} else {
			_, hasCorpus := options["corpus"]
			_, hasSecondPass := options["enable_nonstream"]
			passed = passed && !hasCorpus && !hasSecondPass
		}
		results = append(results, map[string]any{"case": tc.name, "passed": passed, "request": req})
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(out, "protocol-results.json"), b, 0600))
	fmt.Println(string(b))
	for _, r := range results {
		if r["passed"] != true {
			panic("protocol acceptance failed")
		}
	}
}
