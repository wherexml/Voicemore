// Process-level acceptance of the production credential store. Synthetic keys only.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"voiceinput/internal/secrets"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func main() {
	if len(os.Args) > 1 {
		worker(os.Args[1], os.Args[2])
		return
	}
	dir, err := os.MkdirTemp("", "voice-input-secrets-acceptance-")
	must(err)
	defer os.RemoveAll(dir)
	exe, err := os.Executable()
	must(err)
	run := func(mode string) {
		out, err := exec.Command(exe, mode, dir).CombinedOutput()
		if err != nil {
			panic(fmt.Sprintf("%s: %s %v", mode, out, err))
		}
		fmt.Printf("PASS %s\n", mode)
	}
	run("save")
	run("restart-read")
	run("update-delete")
	run("restart-updated")
	keyPath := filepath.Join(dir, "credentials", "master.key")
	encPath := filepath.Join(dir, "credentials", "secrets.enc")
	for _, p := range []string{keyPath, encPath} {
		info, err := os.Stat(p)
		must(err)
		check(info.Mode().Perm() == 0600, "unsafe file permissions")
	}
	info, err := os.Stat(filepath.Dir(keyPath))
	must(err)
	check(info.Mode().Perm() == 0700, "unsafe directory permissions")
	encrypted, err := os.ReadFile(encPath)
	must(err)
	check(!bytes.Contains(encrypted, []byte("synthetic-")), "plaintext persisted")
	run("same-value-save")
	fresh, err := os.ReadFile(encPath)
	must(err)
	check(!bytes.Equal(fresh, encrypted), "nonce reused")
	fmt.Println("PASS permissions, ciphertext, fresh nonce")
	key, err := os.ReadFile(keyPath)
	must(err)
	must(os.Remove(keyPath))
	run("reject")
	after, err := os.ReadFile(encPath)
	must(err)
	check(bytes.Equal(after, fresh), "missing-key failure overwrote ciphertext")
	must(os.WriteFile(keyPath, []byte("short"), 0600))
	run("reject")
	wrong := bytes.Repeat([]byte{7}, 32)
	must(os.WriteFile(keyPath, wrong, 0600))
	run("reject")
	must(os.WriteFile(keyPath, key, 0600))
	damaged := append([]byte{}, fresh...)
	damaged[len(damaged)-1] ^= 1
	must(os.WriteFile(encPath, damaged, 0600))
	run("reject")
	after, err = os.ReadFile(encPath)
	must(err)
	check(bytes.Equal(after, damaged), "corruption failure overwrote ciphertext")
	must(os.WriteFile(encPath, []byte("broken"), 0600))
	run("reject")
	must(os.WriteFile(encPath, fresh, 0600))
	run("failed-write")
	run("restart-updated")
	run("concurrent")
	run("restart-concurrent")
	fmt.Println("PASS all process-level credential acceptance checks")
}
func worker(mode, dir string) {
	s, err := secrets.Open(dir)
	if mode == "reject" {
		check(err != nil, "damaged store accepted")
		check(s.Set("x", "y") != nil, "damaged store allowed write")
		_, err = s.Get("x")
		check(err != nil, "damaged store allowed read")
		return
	}
	must(err)
	get := func(k, want string) { v, err := s.Get(k); must(err); check(v == want, "credential mismatch: "+k) }
	switch mode {
	case "save":
		must(s.Set("local", "synthetic-local"))
		must(s.Set("provider", "synthetic-provider"))
	case "restart-read":
		get("local", "synthetic-local")
		get("provider", "synthetic-provider")
		get("absent", "")
	case "update-delete":
		must(s.Set("local", "synthetic-updated"))
		must(s.Delete("provider"))
	case "restart-updated":
		get("local", "synthetic-updated")
		get("provider", "")
	case "same-value-save":
		must(s.Set("local", "synthetic-updated"))
	case "failed-write":
		// A directory at the destination deterministically rejects rename, even as root.
		path := filepath.Join(dir, "credentials", "secrets.enc")
		must(os.Rename(path, path+".backup"))
		must(os.Mkdir(path, 0700))
		check(s.Set("local", "should-not-save") != nil, "failed save succeeded")
		get("local", "synthetic-updated")
		must(os.Remove(path))
		must(os.Rename(path+".backup", path))
	case "concurrent":
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); must(s.Set(fmt.Sprint(i), "synthetic-parallel")) }(i)
		}
		wg.Wait()
	case "restart-concurrent":
		for i := 0; i < 20; i++ {
			get(fmt.Sprint(i), "synthetic-parallel")
		}
	default:
		panic("unknown mode")
	}
}
