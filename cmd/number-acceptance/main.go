// Command number-acceptance exercises the production formatting/config/history path.
// Failure cases are frozen before implementation; no user history is changed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	fixtures := flag.String("cases", "artifacts/numeric-style/failure-cases.json", "prewritten cases")
	output := flag.String("out", "artifacts/numeric-style/report.json", "JSON evidence")
	endpoint := flag.String("asr", "", "optional real local ASR endpoint")
	historyIDs := flag.String("history-ids", "", "optional comma-separated IDs of local year recordings; copies only")
	flag.Parse()
	var cases []struct{ Name, Input, Expected string }
	b, e := os.ReadFile(*fixtures)
	must(e)
	must(json.Unmarshal(b, &cases))
	var results []map[string]any
	passed := true
	check := func(name string, ok bool, details map[string]any) {
		passed = passed && ok
		details["name"] = name
		details["passed"] = ok
		results = append(results, details)
	}
	for _, c := range cases {
		got, rules := textstyle.Apply(c.Input, textstyle.Defaults(), nil)
		again, _ := textstyle.Apply(got, textstyle.Defaults(), nil)
		opts := textstyle.Defaults()
		opts.ArabicNumbers = false
		off, _ := textstyle.Apply(c.Input, opts, nil)
		check(c.Name, got == c.Expected && again == got && off == c.Input, map[string]any{"input": c.Input, "expected": c.Expected, "actual": got, "off": off, "rules": rules})
	}
	temp, e := os.MkdirTemp("", "voicemore-numbers-")
	must(e)
	defer os.RemoveAll(temp)
	must(os.WriteFile(filepath.Join(temp, "settings.json"), []byte(`{"textStyle":{"trimPeriod":false,"joinAcronyms":false}}`), 0600))
	cfg, e := config.Open(temp)
	must(e)
	s := cfg.Get()
	check("old-settings-default", s.TextStyle.ArabicNumbers && !s.TextStyle.TrimPeriod && !s.TextStyle.JoinAcronyms, map[string]any{"style": s.TextStyle})
	s.TextStyle.ArabicNumbers = false
	must(cfg.Set(s))
	reopened, e := config.Open(temp)
	must(e)
	check("explicit-off-persists", !reopened.Get().TextStyle.ArabicNumbers, map[string]any{})
	if *endpoint != "" {
		h, e := store.Open(temp)
		must(e)
		d, e := dict.Open(temp)
		must(e)
		s = config.Defaults()
		s.ASRURL = *endpoint
		must(cfg.Set(s))
		c := session.Controller{Settings: cfg, History: h, Dict: d}
		var samples []struct {
			Index         int
			ID, AudioPath string
			Audio_path    string `json:"audio_path"`
		}
		b, e := os.ReadFile("dataset/typeless/benchmark-100/sample.json")
		must(e)
		must(json.Unmarshal(b, &samples))
		for _, x := range samples {
			if x.Index != 3 && x.Index != 15 && x.Index != 81 {
				continue
			}
			pcm, e := exec.Command("ffmpeg", "-v", "error", "-i", x.Audio_path, "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1").Output()
			must(e)
			name := x.ID + ".wav"
			must(h.WriteWAV(name, pcm))
			must(h.Add(store.Record{ID: x.ID, AudioFile: name, RawText: "original preserved"}))
			err := c.Retranscribe(x.ID)
			fresh, e := store.Open(temp)
			must(e)
			r, found := fresh.Get(x.ID)
			ok := err == nil && found && len(r.Versions) == 1 && r.RawText == "original preserved"
			var detail any = r.Versions
			if ok {
				v := r.Versions[0]
				expected, _ := textstyle.Apply(v.BasedOn, s.TextStyle, d.Hotwords())
				ok = v.Text == expected && v.BasedOn != ""
			}
			check(fmt.Sprintf("real-ASR-history-%03d", x.Index), ok, map[string]any{"versions": detail, "error": fmt.Sprint(err)})
		}
		home, e := os.UserHomeDir()
		must(e)
		original, e := store.Open(filepath.Join(home, "Library/Application Support/Voice Input"))
		must(e)
		for _, id := range strings.Split(*historyIDs, ",") {
			if id == "" {
				continue
			}
			rec, found := original.Get(id)
			if !found {
				panic("missing real year audio: " + id)
			}
			pcm, e := original.ReadPCM(rec.AudioFile)
			must(e)
			rec.Versions = nil
			must(h.WriteWAV(rec.AudioFile, pcm))
			must(h.Add(rec))
			err := c.Retranscribe(id)
			fresh, e := store.Open(temp)
			must(e)
			r, _ := fresh.Get(id)
			ok := err == nil && len(r.Versions) == 1 && r.RawText == rec.RawText
			if ok {
				ok = strings.Contains(r.Versions[0].Text, "2026") && !strings.Contains(r.Versions[0].Text, "二零二六")
			}
			check("real-year-audio-"+id, ok, map[string]any{"versions": r.Versions, "error": fmt.Sprint(err)})
		}
	}
	b, e = json.MarshalIndent(map[string]any{"passed": passed, "results": results, "scope": "Production formatting, config persistence, optional real ASR -> Controller.Retranscribe -> disk history; excludes native microphone and paste."}, "", "  ")
	must(e)
	must(os.MkdirAll(filepath.Dir(*output), 0700))
	must(os.WriteFile(*output, b, 0600))
	fmt.Println("passed", passed, "checks", len(results))
	if !passed {
		os.Exit(1)
	}
}
