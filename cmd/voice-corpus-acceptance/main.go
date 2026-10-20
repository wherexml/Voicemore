// Command voice-corpus-acceptance replays a frozen private audio corpus through
// the production streaming adapter. It never writes settings or user history.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"voiceinput/internal/asr"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/textstyle"
)

type sample struct {
	ID        string `json:"id"`
	Split     string `json:"split"`
	AudioPath string `json:"audio_path"`
	AudioSHA  string `json:"audio_sha256"`
	PCMSHA    string `json:"pcm_sha256"`
	Raw       string `json:"raw"`
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func readPCM(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid WAV: %s", path)
	}
	valid := false
	var pcm []byte
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if n > len(b)-i-8 {
			return nil, fmt.Errorf("truncated WAV: %s", path)
		}
		chunk := b[i+8 : i+8+n]
		switch string(b[i : i+4]) {
		case "fmt ":
			valid = n >= 16 && binary.LittleEndian.Uint16(chunk) == 1 && binary.LittleEndian.Uint16(chunk[2:]) == 1 && binary.LittleEndian.Uint32(chunk[4:]) == 16000 && binary.LittleEndian.Uint16(chunk[14:]) == 16
		case "data":
			pcm = chunk
		}
		i += 8 + n + n%2
	}
	if !valid || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, fmt.Errorf("requires nonempty mono 16kHz PCM16 WAV: %s", path)
	}
	return pcm, nil
}
func main() {
	corpus := flag.String("corpus", "", "frozen corpus JSON")
	output := flag.String("output", "", "new JSONL file (exclusive creation)")
	variant := flag.String("variant", "", "recorded experiment variant")
	base := flag.String("base", "", "optional isolated service origin")
	split := flag.String("split", "", "optional development, holdout or failure")
	terms := flag.Bool("hotwords", false, "send frozen confirmed dictionary through supported adapter")
	promptFile := flag.String("cleanup-prompt", "", "isolated acceptance only: replay frozen text with this prompt")
	flag.Parse()
	if *corpus == "" || *output == "" || *variant == "" {
		panic("corpus, output and variant required")
	}
	var prompt []byte
	if *promptFile != "" {
		u, e := url.Parse(*base)
		if e != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			panic("cleanup replay requires a loopback HTTP -base")
		}
		var err error
		prompt, err = os.ReadFile(*promptFile)
		must(err)
	}
	b, err := os.ReadFile(*corpus)
	must(err)
	var rows []sample
	must(json.Unmarshal(b, &rows))
	if len(rows) == 0 {
		panic("empty corpus")
	}
	root := filepath.Dir(*corpus)
	b, err = os.ReadFile(filepath.Join(root, "options.json"))
	must(err)
	var options struct {
		TextStyle   textstyle.Options `json:"textStyle"`
		PolishRules string            `json:"polishRules"`
	}
	must(json.Unmarshal(b, &options))
	// Validate every selected input before making the first paid request.
	seen := map[string]bool{}
	for _, r := range rows {
		if *split != "" && r.Split != *split {
			continue
		}
		if seen[r.ID] || r.ID == "" {
			panic("duplicate/empty ID")
		}
		seen[r.ID] = true
		b, err := os.ReadFile(r.AudioPath)
		must(err)
		if digest(b) != r.AudioSHA {
			panic("audio hash mismatch: " + r.ID)
		}
		pcm, err := readPCM(r.AudioPath)
		must(err)
		if digest(pcm) != r.PCMSHA {
			panic("PCM hash mismatch: " + r.ID)
		}
	}
	if len(seen) == 0 {
		panic("no selected samples")
	}
	home, err := os.UserHomeDir()
	must(err)
	dataDir := filepath.Join(home, "Library/Application Support/Voice Input")
	cfg, err := config.Open(dataDir)
	must(err)
	settings := cfg.Get()
	vault, err := secrets.Open(dataDir)
	must(err)
	key, err := vault.Get(providers.Account(providers.Sub2API, settings, "apiKey"))
	must(err)
	if key == "" {
		panic("configured voice credential is missing")
	}
	endpoint := providers.Sub2APIBase(settings)
	if *base != "" {
		endpoint = *base
	}
	var hotwords *string
	var corrections []textstyle.Correction
	if *terms {
		d, err := dict.Open(root)
		must(err)
		p, _, err := d.HotwordParam()
		must(err)
		hotwords = &p
		corrections = d.ConfirmedCorrections()
	}
	f, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	must(err)
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if *split != "" && r.Split != *split {
			continue
		}
		pcm, err := readPCM(r.AudioPath)
		must(err)
		if digest(pcm) != r.PCMSHA {
			panic("PCM hash mismatch: " + r.ID)
		}
		task := fmt.Sprintf("quality-%d-%s", time.Now().UnixNano(), strings.ReplaceAll(r.ID, ".", "-"))
		api := cloudasr.Sub2API{Base: endpoint, APIKey: key, TaskID: task, TextOptions: &cloudasr.VoiceTextOptions{Version: 1, Format: options.TextStyle, PolishRules: options.PolishRules, Corrections: corrections}}
		if *terms && !api.Hotwords() {
			panic("adapter does not support requested hotwords")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(pcm)/32000+90)*time.Second)
		start := time.Now()
		if *promptFile != "" {
			if r.Raw == "" {
				must(enc.Encode(map[string]any{"id": r.ID, "variant": *variant, "status": "NOT_RUN", "reason": "no frozen ASR text"}))
				must(f.Sync())
				cancel()
				continue
			}
			o := *api.TextOptions
			if hotwords != nil {
				o.Hotwords = strings.Split(*hotwords, ",")
			}
			body, err := json.Marshal(map[string]any{"raw": r.Raw, "prompt": string(prompt), "options": o})
			must(err)
			req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/acceptance/cleanup", bytes.NewReader(body))
			must(err)
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			var response map[string]any
			msg := ""
			if err != nil {
				msg = err.Error()
			} else {
				b, readErr := io.ReadAll(io.LimitReader(res.Body, 2<<20))
				res.Body.Close()
				if readErr != nil {
					msg = readErr.Error()
				} else if res.StatusCode != 200 {
					msg = fmt.Sprintf("HTTP %d: %s", res.StatusCode, string(b))
				} else if err = json.Unmarshal(b, &response); err != nil {
					msg = err.Error()
				}
			}
			final := ""
			if text, ok := response["text"].(string); ok {
				final, _ = textstyle.Apply(text, options.TextStyle, o.Hotwords)
			}
			must(enc.Encode(map[string]any{"id": r.ID, "split": r.Split, "variant": *variant, "input": r.Raw, "input_sha256": digest([]byte(r.Raw)), "prompt_sha256": digest(prompt), "response": response, "final": final, "error": msg, "elapsed_ms": time.Since(start).Milliseconds(), "semantic_review_required": true}))
			must(f.Sync())
			cancel()
			fmt.Printf("%s %s cleanup complete error=%t\n", *variant, r.ID, msg != "")
			continue
		}
		raw, result, runErr := asr.TranscribeResult(ctx, api, hotwords, pcm)
		cancel()
		msg := ""
		if runErr != nil {
			msg = runErr.Error()
		}
		row := map[string]any{"id": r.ID, "split": r.Split, "variant": *variant, "audio_sha256": r.AudioSHA, "pcm_sha256": r.PCMSHA, "task_id": task, "raw": raw, "result": result, "error": msg, "elapsed_ms": time.Since(start).Milliseconds(), "audio_seconds": float64(len(pcm)) / 32000, "hotwords": *terms, "semantic_review_required": true}
		must(enc.Encode(row))
		must(f.Sync())
		fmt.Printf("%s %s complete error=%t ms=%d\n", *variant, r.ID, runErr != nil, time.Since(start).Milliseconds())
	}
}
