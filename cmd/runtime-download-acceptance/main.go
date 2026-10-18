//go:build runtimeacceptance

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"voiceinput/internal/localasr"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	if len(os.Args) < 2 {
		panic("usage: output-directory [real-runtime.tar.bz2]")
	}
	out := os.Args[1]
	must(os.MkdirAll(out, 0700))
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"runtime/", "runtime/bin/"} {
		must(tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0755}))
	}
	body := []byte("fixture-python")
	must(tw.WriteHeader(&tar.Header{Name: "runtime/bin/python3.11", Mode: 0755, Size: int64(len(body))}))
	_, err := tw.Write(body)
	must(err)
	must(tw.Close())
	must(gz.Close())
	payload := buf.Bytes()
	sum := sha256.Sum256(payload)
	pin := hex.EncodeToString(sum[:])
	var hits atomic.Int64
	mode := "ok"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch mode {
		case "http":
			w.WriteHeader(503)
			return
		case "redirect":
			http.Redirect(w, r, "http://127.0.0.1:48133/payload", 302)
			return
		case "truncated":
			w.Write(payload[:len(payload)/2])
			return
		case "corrupt":
			w.Write(bytes.Repeat([]byte("x"), len(payload)))
			return
		}
		w.Write(payload)
	}))
	defer server.Close()
	http.DefaultTransport = server.Client().Transport
	results := []map[string]any{}
	record := func(name string, pass bool) {
		results = append(results, map[string]any{"case": name, "passed": pass})
		fmt.Println(name, pass)
	}
	_ = localasr.New(filepath.Join(out, "cloud-only"), "")
	record("cloud-only-construction-no-download", hits.Load() == 0)
	for _, scenario := range []string{"http", "redirect", "truncated", "corrupt", "cancel", "ok"} {
		mode = scenario
		root := filepath.Join(out, scenario)
		must(os.MkdirAll(filepath.Join(root, "runtime"), 0700))
		must(os.WriteFile(filepath.Join(root, "runtime/keep"), []byte("old"), 0600))
		m := localasr.New(root, "")
		ctx, cancel := context.WithCancel(context.Background())
		if scenario == "cancel" {
			cancel()
		}
		err := m.AcceptRuntimeDownload(ctx, server.URL, pin, int64(len(payload)), "gzip")
		cancel()
		partial, _ := filepath.Glob(filepath.Join(root, "downloads/.runtime-partial-*"))
		if scenario != "ok" {
			b, _ := os.ReadFile(filepath.Join(root, "runtime/keep"))
			record(scenario, err != nil && string(b) == "old" && len(partial) == 0)
			continue
		}
		b, _ := os.ReadFile(filepath.Join(root, "runtime/bin/python3.11"))
		record("download-extract", err == nil && bytes.Equal(b, body))
		previous := hits.Load()
		mode = "http"
		err = m.AcceptRuntimeDownload(context.Background(), server.URL, pin, int64(len(payload)), "gzip")
		record("installed-runtime-offline-reuse", err == nil && hits.Load() == previous)
		must(os.RemoveAll(filepath.Join(root, "runtime")))
		err = m.AcceptRuntimeDownload(context.Background(), server.URL, pin, int64(len(payload)), "gzip")
		record("verified-download-cache-offline-reuse", err == nil && hits.Load() == previous)
	}
	root := filepath.Join(out, "legacy-runtime")
	must(os.MkdirAll(filepath.Join(root, "runtime/bin"), 0700))
	must(os.WriteFile(filepath.Join(root, "runtime/bin/python3.11"), body, 0700))
	legacy := "1234567890123456789012345678901234567890123456789012345678901234"
	must(os.WriteFile(filepath.Join(root, "runtime/version.sha256"), []byte(legacy), 0600))
	m := localasr.New(root, "")
	before := hits.Load()
	mode = "http"
	err = m.AcceptRuntimeDownload(context.Background(), server.URL, pin, int64(len(payload)), "gzip", legacy)
	record("compatible-legacy-runtime-no-redownload", err == nil && hits.Load() == before)
	if len(os.Args) > 2 {
		mode = "ok"
		payload, err = os.ReadFile(os.Args[2])
		must(err)
		sum = sha256.Sum256(payload)
		pin = hex.EncodeToString(sum[:])
		root := filepath.Join(out, "real-runtime")
		m := localasr.New(root, "")
		must(m.AcceptRuntimeDownload(context.Background(), server.URL, pin, int64(len(payload)), "bzip2"))
		log, err := exec.Command(filepath.Join(root, "runtime/bin/python3.11"), "-I", "-B", "-c", "import mlx.core, mlx_audio, onnxruntime, fastapi, uvicorn; print('ASR imports passed')").CombinedOutput()
		must(os.WriteFile(filepath.Join(out, "imports.log"), log, 0600))
		record("real-signed-runtime-download-extract-import", err == nil)
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	must(os.WriteFile(filepath.Join(out, "results.json"), data, 0600))
	for _, r := range results {
		if r["passed"] != true {
			os.Exit(1)
		}
	}
}
