// Command cleanup-thinking-acceptance checks provider-specific payloads over HTTP.
package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/providers"
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:58189")
	if err != nil {
		panic(err)
	}
	requests := make(chan map[string]any, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "整理结果。"}}}})
	})}
	go server.Serve(listener)
	defer server.Close()
	passed := true
	for _, provider := range []string{providers.DeepSeek, providers.Local, "fixture-custom"} {
		s := config.Defaults()
		s.CleanupURL = "http://127.0.0.1:58189/v1"
		s.CleanupModel = "fixture"
		s.Providers.DeepSeekEndpoint = s.CleanupURL
		s.Providers.Enabled = map[string]bool{providers.DeepSeek: true}
		s.Providers.Custom = []config.CustomProvider{{ID: "fixture-custom", Kind: providers.KindCleanup, URL: s.CleanupURL, Models: []string{"fixture"}, Enabled: true}}
		model := "fixture"
		if provider == providers.DeepSeek {
			model = "deepseek-flash"
		}
		s.CleanupAssign = config.Assignment{Provider: provider, Model: model}
		opts, e := providers.CleanupOptions(s, func(string) string { return "fixture-key" })
		if e != nil {
			panic(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, e := cleanup.Run(ctx, opts, "原始口述")
		cancel()
		if e != nil {
			panic(e)
		}
		body := <-requests
		_, hasLocal := body["enable_thinking"]
		_, hasCloud := body["thinking"]
		ok := out == "整理结果。"
		switch provider {
		case providers.DeepSeek:
			t, _ := body["thinking"].(map[string]any)
			ok = ok && t["type"] == "disabled" && !hasLocal
		case providers.Local:
			ok = ok && hasLocal && body["enable_thinking"] == false && !hasCloud
		default:
			ok = ok && !hasLocal && !hasCloud
		}
		passed = passed && ok
		json.NewEncoder(os.Stdout).Encode(map[string]any{"provider": provider, "passed": ok, "request": body})
	}
	if !passed {
		os.Exit(1)
	}
}
