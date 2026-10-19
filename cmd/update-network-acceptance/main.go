//go:build updateacceptance

// Exercises update checks over real local HTTPS against an unchanged signed app.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"time"
	"voiceinput/internal/updateclient"
)

func main() {
	target := os.Args[1]
	var results []map[string]any
	record := func(name string, ok bool, detail any) {
		results = append(results, map[string]any{"case": name, "pass": ok, "detail": detail})
	}
	for _, mode := range []string{"tls-recovery", "503-recovery", "404-no-retry", "cancel-backoff", "untrusted-certificate", "503-exhausted", "https-downgrade"} {
		var hits, handshakes atomic.Int32
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			if mode == "https-downgrade" {
				http.Redirect(w, r, "http://127.0.0.1:38129/?sig=SECRET", http.StatusFound)
				return
			}
			if mode == "404-no-retry" {
				w.WriteHeader(404)
				return
			}
			if mode == "503-exhausted" || mode == "cancel-backoff" || (mode == "503-recovery" && n == 1) {
				w.WriteHeader(503)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Version": "0.3.11", "URL": "https://example.com/app.tar.gz", "Size": 1, "Signature": base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))})
		}))
		if mode == "tls-recovery" {
			server.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
				if handshakes.Add(1) == 1 {
					time.Sleep(11 * time.Second)
				}
				return nil, nil
			}}
		}
		server.StartTLS()
		old := http.DefaultTransport
		transport := server.Client().Transport.(*http.Transport).Clone()
		transport.TLSHandshakeTimeout = 10 * time.Second
		if mode == "untrusted-certificate" {
			transport.TLSClientConfig = &tls.Config{}
		}
		http.DefaultTransport = transport
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		if mode == "cancel-backoff" {
			go func() {
				for hits.Load() == 0 {
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
			}()
		}
		started := time.Now()
		_, err := (updateclient.Client{Target: target, Feed: server.URL}).Check(ctx)
		cancel()
		ok := err == nil
		switch mode {
		case "tls-recovery":
			ok = ok && handshakes.Load() == 2
		case "503-recovery":
			ok = ok && hits.Load() == 2
		case "404-no-retry":
			ok = err != nil && hits.Load() == 1
		case "cancel-backoff":
			ok = err != nil && hits.Load() == 1 && time.Since(started) < 10*time.Second
		case "503-exhausted":
			ok = err != nil && hits.Load() == 3
		case "https-downgrade":
			ok = err != nil && hits.Load() == 1
		case "untrusted-certificate":
			ok = err != nil && hits.Load() == 0
		}
		record(mode, ok, map[string]any{"requests": hits.Load(), "seconds": time.Since(started).Seconds(), "error": updateclient.ErrorDetail(err)})
		transport.CloseIdleConnections()
		http.DefaultTransport = old
		server.Close()
	}
	detail := updateclient.ErrorDetail(&url.Error{Op: "Get", URL: "https://release-assets.githubusercontent.com/private?sig=SECRET", Err: fmt.Errorf("net/http: TLS handshake timeout")})
	record("signed-url-redacted", detail == "release-assets.githubusercontent.com: net/http: TLS handshake timeout", detail)
	data, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(data))
	for _, r := range results {
		if r["pass"] != true {
			os.Exit(1)
		}
	}
}
