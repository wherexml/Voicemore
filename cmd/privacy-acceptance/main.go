package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"voiceinput/internal/config"
	"voiceinput/internal/secrets"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	output := flag.String("output", "", "new isolated evidence directory (required)")
	flag.Parse()
	if *output == "" {
		panic("-output is required")
	}
	root, e := filepath.Abs(*output)
	must(e)
	must(os.Mkdir(root, 0700))
	rows := []map[string]any{}
	check := func(n string, ok bool) { rows = append(rows, map[string]any{"case": n, "passed": ok}) }
	dir := func(n string) string { p := filepath.Join(root, n); must(os.Mkdir(p, 0700)); return p }
	read := func(p string) string { b, e := os.ReadFile(p); must(e); return string(b) }
	exists := func(p string) bool { _, e := os.Lstat(p); return e == nil }
	for _, missing := range []bool{false, true} {
		d := dir(fmt.Sprint("settings-", missing))
		s, e := config.Open(d)
		must(e)
		must(s.Set(s.Get()))
		v := s.Get()
		v.PrivateApps = []config.PrivateApp{{BundleID: "private.fixture"}}
		must(s.Set(v))
		p := filepath.Join(d, "settings.json")
		if missing {
			must(os.Remove(p))
		} else {
			must(os.WriteFile(p, []byte("{broken"), 0600))
		}
		for i := 0; i < 2; i++ {
			r, e := config.Open(d)
			check(fmt.Sprint("recovered settings startup and write blocked ", missing, "/", i), e != nil && r.LoadError() != nil && r.Set(r.Get()) != nil)
			c := &session.Controller{Settings: r}
			c.StartPractice(func(string) { panic("unsafe recovered settings started dictation") })
			check("controller rejects unsafe recovered settings before capture", c.View().Phase == session.Failed)
		}
		check(fmt.Sprint("unsafe primary not silently restored ", missing), missing && !exists(p) || !missing && read(p) == "{broken")
	}
	d := dir("history-clear")
	h, e := store.Open(d)
	must(e)
	must(h.WriteWAV("older.wav", []byte{0, 1}))
	must(h.Add(store.Record{ID: "older", RawText: "SYNTHETIC_PRIVATE", AudioFile: "older.wav"}))
	must(h.WriteWAV("newer.wav", []byte{0, 2}))
	must(h.Add(store.Record{ID: "newer", RawText: "SYNTHETIC_PRIVATE", AudioFile: "newer.wav"}))
	p := filepath.Join(d, "history.json")
	must(os.WriteFile(p, []byte(read(p)+" trailing"), 0600))
	h, e = store.Open(d)
	check("history backup still recovers", config.JSONRecovered(e))
	must(os.WriteFile(filepath.Join(d, "unrelated.txt"), []byte("KEEP"), 0600))
	must(h.Clear())
	a, _ := filepath.Glob(p + ".corrupt-*")
	files, e := os.ReadDir(filepath.Join(d, "audio"))
	must(e)
	check("clear removes recovery archives and orphan audio", len(a) == 0 && len(files) == 0 && len(h.List("")) == 0)
	check("clear preserves unrelated data", read(filepath.Join(d, "unrelated.txt")) == "KEEP")
	r, e := store.Open(d)
	check("empty history survives reopen", e == nil && len(r.List("")) == 0)
	d = dir("clear-retry")
	h, e = store.Open(d)
	must(e)
	must(h.WriteWAV("orphan.wav", []byte{0, 1}))
	must(os.Chmod(filepath.Join(d, "audio"), 0500))
	e = h.Clear()
	check("unlink failure reported", e != nil)
	must(os.Chmod(filepath.Join(d, "audio"), 0700))
	e = h.Clear()
	check("clear retry removes orphan", e == nil && !exists(h.AudioPath("orphan.wav")))
	d = dir("clear-commit-failure")
	h, e = store.Open(d)
	must(e)
	must(h.WriteWAV("keep.wav", []byte{0, 1}))
	must(h.Add(store.Record{ID: "keep", AudioFile: "keep.wav"}))
	must(os.Mkdir(filepath.Join(d, "history.json.bak"), 0700))
	e = h.Clear()
	check("failed index commit preserves audio and state", e != nil && exists(h.AudioPath("keep.wav")) && len(h.List("")) == 1)
	d = dir("clear-symlink")
	h, e = store.Open(d)
	must(e)
	external := filepath.Join(root, "outside.wav")
	must(os.WriteFile(external, []byte("KEEP"), 0600))
	must(os.Symlink(external, h.AudioPath("link.wav")))
	e = h.Clear()
	check("clear never follows audio symlinks", e == nil && read(external) == "KEEP" && !exists(h.AudioPath("link.wav")))
	d = dir("clear-archive-retry")
	h, e = store.Open(d)
	must(e)
	archive := filepath.Join(d, "history.json.corrupt-fixture")
	must(os.Mkdir(archive, 0700))
	must(os.WriteFile(filepath.Join(archive, "block"), []byte("SYNTHETIC"), 0600))
	e = h.Clear()
	check("recovery snapshot removal failure reported", e != nil)
	must(os.Remove(filepath.Join(archive, "block")))
	e = h.Clear()
	check("recovery snapshot removal retries safely", e == nil && !exists(archive))
	for _, legacy := range []bool{true, false} {
		d = dir(fmt.Sprint("key-", legacy))
		s := config.Defaults()
		key := "SYNTHETIC_KEY_NEVER_REAL"
		s.CleanupAPIKey = key
		b, e := json.Marshal(s)
		must(e)
		p = filepath.Join(d, "settings.json")
		must(os.WriteFile(p+".bak", b, 0600))
		must(os.WriteFile(p+".corrupt-old", append(b, []byte(" trailing")...), 0600))
		if !legacy {
			s.CleanupAPIKey = ""
			b, e = json.Marshal(s)
			must(e)
		}
		must(os.WriteFile(p, b, 0600))
		cfg, e := config.Open(d)
		must(e)
		vault, e := secrets.Open(d)
		must(e)
		must(vault.Set("fixture", key))
		back, e := vault.Get("fixture")
		must(e)
		cleaner, ok := any(cfg).(interface{ ClearLegacyCleanupKey(string) error })
		check(fmt.Sprint("verified migration API available ", legacy), ok)
		if ok {
			e = cleaner.ClearLegacyCleanupKey(back)
			check("verified migration succeeds", e == nil)
		} else {
			v := cfg.Get()
			v.CleanupAPIKey = ""
			must(cfg.Set(v))
		}
		paths, _ := filepath.Glob(p + "*")
		clean := true
		for _, f := range paths {
			clean = clean && !strings.Contains(read(f), key)
		}
		check(fmt.Sprint("all settings snapshots free of plaintext ", legacy), clean)
		back, e = vault.Get("fixture")
		check("encrypted key retained", e == nil && back == key)
		cfg, e = config.Open(d)
		check("sanitized settings reopen without key", e == nil && cfg.Get().CleanupAPIKey == "")
	}
	d = dir("key-mismatch")
	s, e := config.Open(d)
	must(e)
	v := s.Get()
	v.CleanupAPIKey = "SYNTHETIC_UNMIGRATED"
	must(s.Set(v))
	if cleaner, ok := any(s).(interface{ ClearLegacyCleanupKey(string) error }); ok {
		e = cleaner.ClearLegacyCleanupKey("DIFFERENT")
		check("unverified key not erased", e != nil && s.Get().CleanupAPIKey == v.CleanupAPIKey)
	} else {
		check("unverified key not erased", false)
	}
	d = dir("key-write-failure")
	s, e = config.Open(d)
	must(e)
	v = s.Get()
	v.CleanupAPIKey = "SYNTHETIC_RETRY_KEY"
	must(s.Set(v))
	must(os.Mkdir(filepath.Join(d, "settings.json.bak"), 0700))
	e = s.ClearLegacyCleanupKey(v.CleanupAPIKey)
	check("snapshot failure retains original and reports error", e != nil && s.Get().CleanupAPIKey == v.CleanupAPIKey && strings.Contains(read(filepath.Join(d, "settings.json")), v.CleanupAPIKey))
	must(os.Remove(filepath.Join(d, "settings.json.bak")))
	e = s.ClearLegacyCleanupKey(v.CleanupAPIKey)
	check("credential cleanup retries after failure", e == nil && s.Get().CleanupAPIKey == "" && !strings.Contains(read(filepath.Join(d, "settings.json.bak")), v.CleanupAPIKey))
	b, e := json.MarshalIndent(rows, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(root, "results.json"), b, 0600))
	failed := 0
	for _, r := range rows {
		if r["passed"] != true {
			failed++
			fmt.Println("FAIL", r["case"])
		}
	}
	fmt.Printf("checks=%d failed=%d\n", len(rows), failed)
	if failed > 0 {
		os.Exit(1)
	}
}
