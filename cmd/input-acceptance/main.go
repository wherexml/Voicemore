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
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func main() {
	runtime.LockOSThread()
	if len(os.Args) > 1 && os.Args[1] == "--remote" {
		remoteAcceptance(os.Args[2:])
		return
	}
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
	fixture := exec.Command(filepath.Join(dir, "Fixture.app/Contents/MacOS/fixture"), dir)
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
				if state["active"] != "true" || state["keyWindow"] != "true" {
					panic("fixture lost foreground focus; aborting keyboard acceptance: " + cmd)
				}
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
	defer func() {
		b, _ := json.MarshalIndent(results, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, "results.json"), b, 0600)
	}()
	if os.Getenv("VOICE_INPUT_ACCEPTANCE_AUTO_SEND_ONLY") != "1" {
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

		// Exercise remote profiles using real keyboard delivery to an opaque native
		// canvas, including Ctrl+V; this proves dispatch, not network clipboard sync.
		for _, shortcut := range []string{"cmd-v", "ctrl-v"} {
			command("remote-" + shortcut)
			target := macos.CaptureTarget()
			rules := []config.RemoteInput{{BundleID: target.BundleID, Shortcut: shortcut, SyncDelayMs: 300}}
			target.Release()
			text := "远程快捷键 " + shortcut
			res, err = macos.Insert(text, macos.InsertOptions{RemoteInputs: rules})
			if res.Target != nil {
				res.Target.Release()
			}
			// CGEventPost is asynchronous; allow the native event loop to consume it.
			time.Sleep(200 * time.Millisecond)
			state = command("read-remote-" + shortcut)
			results = append(results, map[string]any{"case": "remote " + shortcut, "passed": err == nil && res.Remote && !res.ClipboardOnly && !res.ClipboardRestored && state["opaque"] == text && clipboard() == text, "state": state, "result": res})
		}
		for _, interruptAfter := range []time.Duration{200 * time.Millisecond, 700 * time.Millisecond} {
			for _, scenario := range []string{"cancel", "clipboard", "focus"} {
				command("remote-protection-" + scenario)
				target := macos.CaptureTarget()
				rules := []config.RemoteInput{{BundleID: target.BundleID, Shortcut: "cmd-v", SyncDelayMs: 1000}}
				target.Release()
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() {
					defer close(done)
					time.Sleep(interruptAfter)
					switch scenario {
					case "cancel":
						cancel()
					case "clipboard":
						setClipboard("new clipboard must survive")
					case "focus":
						command("second")
					}
				}()
				res, err = macos.Insert("must not paste", macos.InsertOptions{Context: ctx, RemoteInputs: rules})
				<-done
				cancel()
				if res.Target != nil {
					res.Target.Release()
				}
				state = command("read-protection-" + scenario)
				passed := !res.Remote && state["opaque"] == "" && state["second"] == "当前输入位置验证"
				if scenario == "cancel" {
					passed = passed && err == context.Canceled
				} else {
					passed = passed && err == nil && res.ClipboardOnly
				}
				if scenario == "clipboard" {
					passed = passed && clipboard() == "new clipboard must survive"
				}
				results = append(results, map[string]any{"case": fmt.Sprintf("remote guard %s at %s", scenario, interruptAfter), "passed": passed, "state": state, "result": res, "error": fmt.Sprint(err)})
			}

		}
	}
	// Real native send events: assert payload existed before Enter and exactly one
	// key reached the target. This also catches modifier mistakes and duplicates.
	bundle := "dev.steve.voice-input.acceptance-fixture"
	for n, scenario := range []string{"enter", "cmd-enter", "ctrl-enter", "inherit", "override", "off", "disabled", "cancel-send", "focus-send", "clipboard-send", "fallback-send", "remote-no-send", "remote-disabled", "empty-send", "unknown-key"} {
		command(fmt.Sprintf("send-reset-%d", n))
		auto := config.AutoSend{Enabled: true, Default: "enter"}
		want := "enter"
		switch scenario {
		case "cmd-enter", "ctrl-enter":
			auto.Default = scenario
			want = scenario
		case "inherit":
			auto.Default = "ctrl-enter"
			auto.Apps = []config.AutoSendApp{{BundleID: bundle, Shortcut: "inherit"}}
			want = "ctrl-enter"
		case "override":
			auto.Default = "ctrl-enter"
			auto.Apps = []config.AutoSendApp{{BundleID: bundle, Shortcut: "cmd-enter"}}
			want = "cmd-enter"
		case "off":
			auto.Apps = []config.AutoSendApp{{BundleID: bundle, Shortcut: "off"}}
			want = ""
		case "disabled":
			auto.Enabled = false
			want = ""
		case "cancel-send", "focus-send", "clipboard-send", "fallback-send", "remote-no-send", "remote-disabled", "empty-send", "unknown-key":
			want = ""
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			switch scenario {
			case "cancel-send", "focus-send", "clipboard-send":
				time.Sleep(150 * time.Millisecond)
				switch scenario {
				case "cancel-send":
					cancel()
				case "focus-send":
					command("first")
				case "clipboard-send":
					setClipboard("new user copy")
				}
			}
		}()
		if scenario == "fallback-send" {
			command("none")
		}
		options := macos.InsertOptions{Context: ctx, RestoreDelayMs: 600, AutoSend: auto}
		if scenario == "remote-no-send" {
			options.RemoteInputs = []config.RemoteInput{{BundleID: bundle, Shortcut: "cmd-v", SyncDelayMs: 300}}
		}
		if scenario == "remote-disabled" {
			options.RemoteInputs = []config.RemoteInput{{BundleID: bundle}}
		}
		if scenario == "unknown-key" {
			options.AutoSend.Default = "unknown"
		}
		text := "自动发送验收 " + scenario
		if scenario == "empty-send" {
			text = ""
		}
		res, err := macos.Insert(text, options)
		<-done
		cancel()
		if res.Target != nil {
			res.Target.Release()
		}
		time.Sleep(150 * time.Millisecond)
		state := command(fmt.Sprintf("send-read-%d", n))
		passed := err == nil && res.SendShortcut == want
		if want != "" {
			passed = passed && state["sendEvents"] == "1" && state["sentText"] == text && state["sendFlags"] == want && state["second"] == ""
		} else {
			passed = passed && state["sendEvents"] == "0"
		}
		results = append(results, map[string]any{"case": "auto-send " + scenario, "passed": passed, "state": state, "result": res, "error": fmt.Sprint(err)})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := clipboard()
	_, err := macos.Insert("must not appear", macos.InsertOptions{Context: ctx})
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
