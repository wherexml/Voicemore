// Command cleanup-acceptance exercises production cleanup.Run over real HTTP sockets.
// It does not use credentials or modify user data; JSON on stdout is the evidence.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"voiceinput/internal/cleanup"
)

func main() {
	cases := []string{"body-timeout", "partial-timeout", "cancelled", "truncated", "malformed", "success", "status"}
	rows := []map[string]any{}
	passed := true
	for _, name := range cases {
		l, e := net.Listen("tcp", "127.0.0.1:58187")
		if e != nil {
			panic(e)
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch name {
			case "body-timeout", "partial-timeout", "cancelled":
				w.WriteHeader(200)
				if name == "partial-timeout" {
					fmt.Fprint(w, `{"choices":[`)
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			case "truncated":
				w.Header().Set("Content-Length", "1000")
				fmt.Fprint(w, `{"choices":[`)
			case "malformed":
				fmt.Fprint(w, `invalid`)
			case "success":
				fmt.Fprint(w, `{"choices":[{"message":{"content":"明天下午开会。"}}]}`)
			case "status":
				w.WriteHeader(503)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}
		})}
		go srv.Serve(l)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		if name == "cancelled" {
			time.AfterFunc(80*time.Millisecond, cancel)
		}
		t := time.Now()
		out, err := cleanup.Run(ctx, cleanup.Options{BaseURL: "http://127.0.0.1:58187/v1", Model: "fixture"}, "明天下午开会。")
		elapsed := time.Since(t).Milliseconds()
		var status *cleanup.StatusError
		ok := false
		switch name {
		case "body-timeout", "partial-timeout":
			ok = errors.Is(err, context.DeadlineExceeded)
		case "cancelled":
			ok = errors.Is(err, context.Canceled)
		case "truncated":
			ok = errors.Is(err, cleanup.ErrUnreachable)
		case "malformed":
			ok = errors.Is(err, cleanup.ErrNoText)
		case "success":
			ok = err == nil && out == "明天下午开会。"
		case "status":
			ok = errors.As(err, &status) && status.Code == 503 && elapsed < 150
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		rows = append(rows, map[string]any{"case": name, "pass": ok, "milliseconds": elapsed, "error": msg, "output": out})
		passed = passed && ok
		cancel()
		http.DefaultClient.CloseIdleConnections()
		srv.Close()
	}
	json.NewEncoder(os.Stdout).Encode(rows)
	if !passed {
		os.Exit(1)
	}
}
