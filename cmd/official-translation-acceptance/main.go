//go:build officialtranslationacceptance

package main

import (
	"context"
	"encoding/json"
	"os"
	"time"
	"voiceinput/internal/asr"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/session"
)

func main() {
	if len(os.Args) < 3 {
		panic("usage: command base [task-id language]")
	}
	if os.Args[1] == "controller" {
		rows := session.AcceptOfficialTranslation(os.Args[2])
		json.NewEncoder(os.Stdout).Encode(rows)
		for _, r := range rows {
			if r["passed"] != true {
				os.Exit(1)
			}
		}
		return
	}
	api := cloudasr.Sub2API{Base: os.Args[2], APIKey: "local-fixture-key-only", TaskID: os.Args[3], TextOptions: &cloudasr.VoiceTextOptions{Version: 1, TranslationLanguage: os.Args[4]}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pcm := make([]byte, 640)
	pcm[0] = 1
	raw, result, err := asr.TranscribeResult(ctx, api, nil, pcm)
	row := map[string]any{"raw": raw, "result": result, "passed": err == nil}
	if err != nil {
		row["error"] = err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(row)
}
