//go:build darwin && integrityacceptance

// Real Cocoa event loop and quit callbacks, isolated data and synthetic input.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"voiceinput/internal/session"

	"github.com/egoist/mygo"
)

func main() {
	output := flag.String("output", "", "fresh isolated evidence directory")
	fault := flag.String("fault", "index", "index, audio, both, success or private")
	flag.Parse()
	root, err := filepath.Abs(*output)
	if err != nil || *output == "" {
		panic("output must be specified")
	}
	if err = os.Mkdir(root, 0700); err != nil {
		panic(err)
	}
	write := func(name string, value any) {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
			panic(err)
		}
	}
	finished := make(chan struct{})
	go func() {
		select {
		case <-finished:
			return
		case <-time.After(8 * time.Second):
			stack := make([]byte, 1<<20)
			n := runtime.Stack(stack, true)
			_ = os.WriteFile(filepath.Join(root, "timeout-stacks.txt"), stack[:n], 0600)
			write("results.json", map[string]any{"passed": false, "timeout": true, "fault": *fault})
			os.Exit(2)
		}
	}()
	mygo.App.SetName("Voicemore Dev")
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(root, "ui-data"))
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	var fixture *session.PersistenceExitAcceptance
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { fixture.WillQuit() })
	mygo.App.WhenReady(func() {
		fixture, err = session.StartPersistenceExitAcceptance(root, *fault, mygo.RunOnMain, mygo.App.Quit)
		if err != nil {
			panic(err)
		}
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
	rows := fixture.Results()
	passed := true
	for _, row := range rows {
		passed = passed && row["passed"] == true
	}
	write("results.json", map[string]any{"passed": passed, "fault": *fault, "nativeEventLoop": true, "checks": rows})
	close(finished)
	fmt.Printf("%s: %d checks; passed=%v; evidence=%s\n", *fault, len(rows), passed, root)
	if !passed {
		os.Exit(1)
	}
}
