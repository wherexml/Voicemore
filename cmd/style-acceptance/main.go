package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
	"voiceinput/internal/textstyle"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func main() {
	out := flag.String("out", "artifacts/style-installed", "evidence")
	audio := flag.Bool("audio", false, "exercise installed ASR")
	flag.Parse()
	must(os.MkdirAll(*out, 0700))
	home, _ := os.UserHomeDir()
	source := filepath.Join(home, "Library/Application Support/Voice Input")
	d, e := dict.Open(source)
	must(e)
	var cases []struct{ Name, Input, Expected string }
	b, e := os.ReadFile("artifacts/transcript-style/failure-cases.json")
	must(e)
	must(json.Unmarshal(b, &cases))
	var results []map[string]any
	pass := true
	for _, c := range cases {
		got, _ := textstyle.Apply(c.Input, textstyle.Defaults(), d.Hotwords())
		again, _ := textstyle.Apply(got, textstyle.Defaults(), d.Hotwords())
		off, _ := textstyle.Apply(c.Input, textstyle.Options{}, d.Hotwords())
		ok := got == c.Expected && again == got && off == c.Input
		pass = pass && ok
		results = append(results, map[string]any{"name": c.Name, "passed": ok, "actual": got, "expected": c.Expected})
	}
	var replay struct {
		Rows []struct{ ID, Before, After string }
	}
	b, e = os.ReadFile("artifacts/transcript-style/report.json")
	must(e)
	must(json.Unmarshal(b, &replay))
	for _, r := range replay.Rows {
		got, _ := textstyle.Apply(r.Before, textstyle.Defaults(), d.Hotwords())
		ok := got == r.After
		pass = pass && ok
		results = append(results, map[string]any{"id": r.ID, "passed": ok, "before": r.Before, "actual": got, "expected": r.After})
	}
	temp, e := os.MkdirTemp("", "voicemore-style-")
	must(e)
	defer os.RemoveAll(temp)
	h, e := store.Open(temp)
	must(e)
	must(h.Add(store.Record{ID: "empty-style", RawText: "嗯。", Corrected: true, CorrectedText: ""}))
	h, e = store.Open(temp)
	must(e)
	r, _ := h.Get("empty-style")
	ok := r.Transcript() == ""
	pass = pass && ok
	results = append(results, map[string]any{"name": "empty-result-persistence", "passed": ok})
	if *audio {
		original, e := store.Open(source)
		must(e)
		cfg, e := config.Open(temp)
		must(e)
		s := cfg.Get()
		s.ASRURL = "http://127.0.0.1:48132"
		must(cfg.Set(s))
		c := session.Controller{Settings: cfg, History: h, Dict: d}
		for _, id := range []string{"20261014-100844.635655", "20261014-100413.648693"} {
			rec, found := original.Get(id)
			if !found {
				panic(id)
			}
			pcm, e := original.ReadPCM(rec.AudioFile)
			must(e)
			rec.Versions = nil
			must(h.WriteWAV(rec.AudioFile, pcm))
			must(h.Add(rec))
			e = c.Retranscribe(id)
			fresh, _ := h.Get(id)
			ok := e == nil && len(fresh.Versions) == 1
			if ok {
				v := fresh.Versions[0]
				want := "按照当前的 mock 的效果，帮我完整的设计一下，到我们的这个 APP 里面"
				if id == "20261014-100413.648693" {
					want = "按照当前的 mock 的效果，帮我完整的设计一下到我们的 APP 里面"
				}
				ok = v.Text == want && v.BasedOn != ""
			}
			pass = pass && ok
			results = append(results, map[string]any{"name": "installed-audio-controller", "id": id, "passed": ok, "versions": fresh.Versions, "error": fmt.Sprint(e)})
		}
	}
	b, e = json.MarshalIndent(map[string]any{"passed": pass, "results": results}, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(*out, "report.json"), b, 0600))
	fmt.Println("passed:", pass, "checks:", len(results))
	if !pass {
		os.Exit(1)
	}
}
