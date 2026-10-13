// Real filesystem/process/HTTP acceptance; no mocked inference or unit tests.
// Uses its own data directory and five-digit port. Model weights are hardlinked
// read-only into the fixture, never redownloaded or modified in the source.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"voiceinput/internal/localasr"
)

type result struct {
	Name   string         `json:"name"`
	Passed bool           `json:"passed"`
	State  localasr.State `json:"state"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func wait(m *localasr.Manager, phase string) localasr.State {
	end := time.Now().Add(150 * time.Second)
	for time.Now().Before(end) {
		s := m.Snapshot()
		if s.Phase == phase {
			return s
		}
		if s.Phase == "error" && phase != "error" {
			panic(s.Detail)
		}
		time.Sleep(100 * time.Millisecond)
	}
	panic("timeout waiting for " + phase)
}
func settle(m *localasr.Manager) { time.Sleep(300 * time.Millisecond) }
func main() {
	source := flag.String("source", "", "already deployed model directory")
	archive := flag.String("runtime", "resources/local-asr/runtime.tar.gz", "bundled runtime")
	output := flag.String("out", "artifacts/managed-asr-20261013/lifecycle", "evidence directory")
	flag.Parse()
	must(os.MkdirAll(*output, 0700))
	root, err := os.MkdirTemp(*output, "fixture-")
	must(err)
	root, err = filepath.Abs(root)
	must(err)
	var results []result
	record := func(name string, ok bool, m *localasr.Manager) {
		s := m.Snapshot()
		results = append(results, result{name, ok, s})
		fmt.Println(name, ok, s.Phase)
		b, _ := json.MarshalIndent(results, "", "  ")
		must(os.WriteFile(filepath.Join(*output, "report.json"), b, 0600))
		if !ok {
			panic(name)
		}
	}
	m := localasr.New(root, *archive, 48133)
	defer m.Close()
	// Invalid source must not produce an installed model or a process.
	m.Deploy(filepath.Join(root, "missing"))
	s := wait(m, "error")
	record("missing_source_rejected", !s.Installed && s.PID == 0, m)
	settle(m)
	// Create a fixture using hardlinks for immutable, verified weights.
	must(os.MkdirAll(filepath.Join(root, "model"), 0700))
	files, err := os.ReadDir(*source)
	must(err)
	for _, f := range files {
		if !f.IsDir() {
			must(os.Link(filepath.Join(*source, f.Name()), filepath.Join(root, "model", f.Name())))
		}
	}
	// Copy path skips already verified files; cancellation must terminate cleanly.
	m.Deploy(*source)
	time.Sleep(20 * time.Millisecond)
	m.Stop()
	wait(m, "stopped")
	record("deployment_cancelled", m.Snapshot().PID == 0, m)
	settle(m)
	// Occupied port is not mistaken for our worker, and is never terminated.
	listener, err := net.Listen("tcp", "127.0.0.1:48133")
	must(err)
	m.Deploy(*source)
	s = wait(m, "error")
	record("foreign_port_rejected", s.PID == 0 && strings.Contains(s.Detail, "占用"), m)
	must(listener.Close())
	settle(m)
	// Corrupt only a fixture metadata file; app-owned source remains unchanged.
	config := filepath.Join(root, "model/config.json")
	must(os.Remove(config))
	must(os.WriteFile(config, []byte("{}"), 0600))
	m.Start()
	s = wait(m, "error")
	record("corrupt_model_rejected", s.PID == 0 && strings.Contains(s.Detail, "config.json"), m)
	settle(m)
	m.Deploy(*source)
	s = wait(m, "ready")
	record("local_repair_and_start", s.PID > 0, m)
	settle(m)
	pid := s.PID
	m.Start()
	time.Sleep(200 * time.Millisecond)
	record("duplicate_start_no_second_worker", m.Snapshot().PID == pid, m)
	m.Stop()
	s = wait(m, "stopped")
	record("stop_releases_worker", s.PID == 0, m)
	settle(m)
	// Exercise the real pinned HTTP download path using only a 1 KB README.
	// The 2.46 GB weights are verified and reused, never downloaded.
	must(os.Remove(filepath.Join(root, "model/README.md")))
	m.Deploy("")
	s = wait(m, "ready")
	record("download_missing_metadata_reuse_weights", s.PID > 0, m)
	settle(m)
	m.Close()
	record("close_releases_worker", m.Snapshot().PID == 0, m)
	next := localasr.New(root, *archive, 48133)
	defer next.Close()
	next.Start()
	s = wait(next, "ready")
	record("reopen_existing_deployment", s.PID > 0, next)
	next.Close()
	// Keep evidence and the small logs, remove only this command's large fixture.
	must(os.WriteFile(filepath.Join(*output, "worker.log"), read(filepath.Join(root, "worker.log")), 0600))
	must(os.RemoveAll(root))
}
func read(p string) []byte { b, e := os.ReadFile(p); must(e); return b }
