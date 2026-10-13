//go:build darwin

// Native E2E: real focused controls, production insertion, real clipboard.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"voiceinput/internal/macos"
)

func main() {
	runtime.LockOSThread()
	if len(os.Args) > 1 && os.Args[1] == "--probe" {
		target := macos.CaptureTarget()
		defer target.Release()
		json.NewEncoder(os.Stdout).Encode(map[string]any{"app": target.AppName, "hasElement": target.HasElement(), "pasteAvailable": target.PasteAvailable()})
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--wechat-draft" {
		target := macos.CaptureTarget()
		defer target.Release()
		if target.BundleID != "com.tencent.xinWeChat" {
			panic("WeChat must be frontmost")
		}
		result, err := macos.Insert("语音输入兼容性验证（未发送）", macos.InsertOptions{RestoreDelayMs: 600})
		if result.Target != nil {
			defer result.Target.Release()
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"result": result, "error": fmt.Sprint(err)})
		return
	}
	dir, _ := filepath.Abs("artifacts/current-input")
	os.Remove(filepath.Join(dir, "command"))
	os.Remove(filepath.Join(dir, "state"))
	fixture := exec.Command(filepath.Join(dir, "fixture"), dir)
	if err := fixture.Start(); err != nil {
		panic(err)
	}
	defer fixture.Process.Kill()
	time.Sleep(time.Second)
	command := func(cmd string) map[string]string {
		os.WriteFile(filepath.Join(dir, "command"), []byte(cmd), 0600)
		for i := 0; i < 100; i++ {
			b, _ := os.ReadFile(filepath.Join(dir, "state"))
			var state map[string]string
			json.Unmarshal(b, &state)
			if state["command"] == cmd {
				return state
			}
			time.Sleep(50 * time.Millisecond)
		}
		panic("fixture timeout: " + cmd)
	}
	clipboard := func() string { b, _ := exec.Command("pbpaste").Output(); return string(b) }
	setClipboard := func(s string) {
		c := exec.Command("pbcopy")
		c.Stdin = strings.NewReader(s)
		if err := c.Run(); err != nil {
			panic(err)
		}
	}
	original := clipboard()
	defer setClipboard(original)
	results := []map[string]any{}
	command("first")
	old := macos.CaptureTarget()
	defer old.Release()
	command("second")
	setClipboard("clipboard sentinel")
	res, err := macos.Insert("当前输入位置验证", macos.InsertOptions{RestoreDelayMs: 600})
	if res.Target != nil {
		res.Target.Release()
	}
	state := command("read1")
	results = append(results, map[string]any{"case": "changed focus writes current field", "passed": err == nil && !res.ClipboardOnly && state["first"] == "" && state["second"] == "当前输入位置验证" && clipboard() == "clipboard sentinel", "state": state, "result": res, "error": fmt.Sprint(err)})
	for _, mode := range []string{"none", "readonly"} {
		command(mode)
		text := "剪贴板保留 " + mode
		res, err = macos.Insert(text, macos.InsertOptions{RestoreDelayMs: 200})
		if res.Target != nil {
			res.Target.Release()
		}
		state = command("read-" + mode)
		results = append(results, map[string]any{"case": mode, "passed": err == nil && res.ClipboardOnly && clipboard() == text && state["first"] == "" && state["second"] == "当前输入位置验证", "state": state, "result": res, "error": fmt.Sprint(err)})
	}
	command("opaque")
	res, err = macos.Insert("自绘输入框验证", macos.InsertOptions{RestoreDelayMs: 600})
	if res.Target != nil {
		res.Target.Release()
	}
	state = command("read-opaque")
	results = append(results, map[string]any{"case": "custom editor without AX writable input", "passed": err == nil && !res.ClipboardOnly && !res.FocusVerified && state["opaque"] == "自绘输入框验证", "state": state, "result": res})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := clipboard()
	_, err = macos.Insert("must not appear", macos.InsertOptions{Context: ctx})
	results = append(results, map[string]any{"case": "cancelled", "passed": err == context.Canceled && clipboard() == before})
	b, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile(filepath.Join(dir, "results.json"), b, 0600)
	fmt.Println(string(b))
	os.WriteFile(filepath.Join(dir, "command"), []byte("quit"), 0600)
	for _, r := range results {
		if r["passed"] != true {
			os.Exit(1)
		}
	}
}
