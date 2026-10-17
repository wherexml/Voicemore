// Command custom-thinking-acceptance verifies persisted custom-provider requests.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/providers"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	l, e := net.Listen("tcp", "127.0.0.1:58190")
	must(e)
	bodies := make(chan map[string]any, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		bodies <- b
		fmt.Fprint(w, `{"choices":[{"message":{"content":"整理正文。"}}]}`)
	})}
	go srv.Serve(l)
	defer srv.Close()
	cases := []struct {
		name, mode, params string
		bad                bool
	}{
		{"legacy", "", "", false}, {"server", "server", "", false}, {"deepseek", "deepseek", "", false}, {"omlx", "omlx", "", false}, {"custom", "custom", `{"reasoning_effort":"none","chat_template_kwargs":{"enable_thinking":false}}`, false},
		{"inactive-draft", "server", `{"thinking":{"type":"disabled"}}`, false}, {"bad-json", "custom", "{", true}, {"array", "custom", "[]", true}, {"null", "custom", "null", true}, {"empty", "custom", "{}", true}, {"model-override", "custom", `{"model":"evil"}`, true}, {"messages-override", "custom", `{"messages":[]}`, true}, {"credentials", "custom", `{"api_key":"fixture"}`, true}, {"nested-credentials", "custom", `{"thinking":{"token":"fixture"}}`, true}, {"unknown", "wrong", "", true},
	}
	all := true
	for _, c := range cases {
		raw, _ := json.Marshal(map[string]any{"id": "fixture", "kind": providers.KindCleanup, "name": "Fixture", "url": "http://127.0.0.1:58190/v1", "models": []string{"fixture-model"}, "enabled": true, "thinkingMode": c.mode, "thinkingParams": c.params})
		var cp config.CustomProvider
		must(json.Unmarshal(raw, &cp))
		s := config.Defaults()
		s.Providers.Custom = []config.CustomProvider{cp}
		s.CleanupAssign = config.Assignment{Provider: "fixture", Model: "fixture-model"}
		dir, e := os.MkdirTemp("", "custom-thinking-")
		must(e)
		st, e := config.Open(dir)
		must(e)
		must(st.Set(s))
		st, e = config.Open(dir)
		must(e)
		loaded := st.Get()
		os.RemoveAll(dir)
		o, e := providers.CleanupOptions(loaded, func(string) string { return "" })
		var body map[string]any
		out := ""
		if e == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			out, e = cleanup.Run(ctx, o, "待整理原文")
			cancel()
			if e == nil {
				body = <-bodies
			}
		}
		ok := false
		if c.bad {
			ok = e != nil
		} else if e == nil {
			ok = out == "整理正文。" && body["model"] == "fixture-model"
			_, dt := body["thinking"]
			_, lt := body["enable_thinking"]
			switch c.mode {
			case "deepseek":
				v, _ := body["thinking"].(map[string]any)
				ok = ok && v["type"] == "disabled" && !lt
			case "omlx":
				ok = ok && lt && body["enable_thinking"] == false && !dt
			case "custom":
				ok = ok && body["reasoning_effort"] == "none" && !dt && !lt
			default:
				ok = ok && !dt && !lt
			}
		}
		all = all && ok
		msg := ""
		if e != nil {
			msg = e.Error()
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"case": c.name, "pass": ok, "error": msg, "request": body})
	}
	if !all {
		os.Exit(1)
	}
}
