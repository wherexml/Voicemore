//go:build runtimeacceptance

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"voiceinput/internal/localasr"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: runtime-acceptance archive output-directory")
	}
	archive, out := os.Args[1], os.Args[2]
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		panic("output must be new")
	}
	b, err := os.ReadFile(archive)
	if err != nil {
		panic(err)
	}
	h := sha256.Sum256(b)
	m := localasr.New(out, archive)
	if err := m.AcceptRuntime(context.Background(), hex.EncodeToString(h[:])); err != nil {
		panic(err)
	}
	python := filepath.Join(out, "runtime/bin/python3.11")
	output, err := exec.Command(python, "-I", "-B", "-c", "import mlx.core, mlx_audio, onnxruntime, fastapi, uvicorn; print('ASR imports passed')").CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("imports: %v: %s", err, output))
	}
	report, _ := json.MarshalIndent(map[string]any{"passed": true, "archive": archive, "sha256": hex.EncodeToString(h[:]), "imports": string(output)}, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "result.json"), report, 0600); err != nil {
		panic(err)
	}
	fmt.Println(string(report))
}
