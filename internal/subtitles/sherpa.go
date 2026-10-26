package subtitles

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed sherpa_stream.py
var sherpaScript []byte

type sherpaEngine struct {
	p     *proc
	stdin io.WriteCloser
}

// SherpaPython is the interpreter of a sherpa root.
func SherpaPython(root string) string { return filepath.Join(root, "venv/bin/python3") }

func startSherpa(ctx context.Context, cfg Config, text func(Kind, int, string, timing)) (engine, <-chan error, error) {
	py, model := SherpaPython(cfg.SherpaRoot), filepath.Join(cfg.SherpaRoot, "model")
	for _, p := range []string{py, filepath.Join(model, "tokens.txt")} {
		if _, err := os.Stat(p); err != nil {
			return nil, nil, fail(ErrSherpaMissing, "%s", p)
		}
	}
	script := filepath.Join(cfg.WorkDir, "sherpa_stream.py")
	if err := os.WriteFile(script, sherpaScript, 0o600); err != nil {
		return nil, nil, fail(ErrEngineStart, "%v", err)
	}
	logOut, _ := os.Create(filepath.Join(cfg.WorkDir, "sherpa.log"))
	cmd := exec.Command(py, "-I", script, "--model-dir", model)
	cmd.Dir = cfg.WorkDir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "PYTHONUNBUFFERED=1"}
	cmd.Stderr = logOut
	p := newProc(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fail(ErrEngineStart, "%v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fail(ErrEngineStart, "%v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fail(ErrEngineStart, "%v", err)
	}
	e := &sherpaEngine{p: p, stdin: stdin}
	cfg.log("sherpa_start", map[string]any{"pid": cmd.Process.Pid})
	// feed blocks in a pipe write while the engine is behind or hung:
	// cancelling closes the pipe and the process group from here instead.
	go func() {
		select {
		case <-ctx.Done():
			e.close()
		case <-p.waited:
		}
	}()

	ready := make(chan error, 1)
	errs := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		isReady := false
		for sc.Scan() {
			var m struct {
				Type    string  `json:"type"`
				Segment int     `json:"segment"`
				Text    string  `json:"text"`
				Message string  `json:"message"`
				LoadS   float64 `json:"load_s"`
				AudioS  float64 `json:"audio_s"`     // stream audio consumed when decoded
				TokenS  float64 `json:"token_end_s"` // end of the last token, -1 when unknown
				// A final's range on the recognizer's decoded clock, see sherpa_stream.py.
				Start     *int64  `json:"start_sample"`
				End       *int64  `json:"end_sample"`
				PrevToken int64   `json:"prev_token_sample"`
				FirstTok  float64 `json:"first_token_s"`
				LastTok   float64 `json:"last_token_s"`
				Clock     string  `json:"clock"`
				Tokens    []string `json:"tokens"`
				TokenAt   []int64  `json:"token_samples"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			switch m.Type {
			case "ready":
				isReady = true
				trySend(ready, nil)
			case "partial":
				text(Partial, m.Segment, strings.TrimSpace(m.Text), timing{audioS: m.AudioS, tokenEndS: m.TokenS})
			case "final":
				t := timing{audioS: m.AudioS, tokenEndS: m.TokenS}
				if m.Start != nil && m.End != nil {
					t.rng = &segRange{start: *m.Start, end: *m.End, prevToken: m.PrevToken, firstTokS: m.FirstTok, lastTokS: m.LastTok, clock: m.Clock}
				}
				if len(m.Tokens) == len(m.TokenAt) && m.Clock == "decoded" {
					for k, tok := range m.Tokens {
						t.tokens = append(t.tokens, Token{Text: tok, Sample: m.TokenAt[k]})
					}
				}
				text(Final, m.Segment, strings.TrimSpace(m.Text), t)
			case "error":
				err := fail(ErrEngineFailed, "sherpa: %s", m.Message)
				if !isReady {
					trySend(ready, fail(ErrEngineStart, "sherpa: %s", m.Message))
				}
				trySend(errs, err)
			}
		}
		werr := p.wait()
		if logOut != nil {
			logOut.Close()
		}
		if !isReady {
			trySend(ready, fail(ErrEngineStart, "sherpa exited: %v", werr))
		} else if ctx.Err() == nil {
			trySend(errs, fail(ErrEngineFailed, "sherpa exited: %v", werr))
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			e.close()
			return nil, nil, err
		}
	case <-ctx.Done():
		e.close()
		return nil, nil, ctx.Err()
	case <-time.After(60 * time.Second):
		e.close()
		return nil, nil, fail(ErrEngineStart, "sherpa not ready within 60 s")
	}
	return e, errs, nil
}

// trySend reports the first failure; later ones of the same end are dropped.
func trySend(ch chan error, err error) {
	select {
	case ch <- err:
	default:
	}
}

func (e *sherpaEngine) feed(ctx context.Context, pcm []byte) error {
	if _, err := e.stdin.Write(pcm); err != nil {
		return fail(ErrEngineFailed, "send: %v", err)
	}
	return nil
}

// close ends input, which unblocks a pending feed, and the process group,
// and returns once it is gone or the bound passed. It may be called more
// than once.
func (e *sherpaEngine) close() {
	e.stdin.Close()
	e.p.stop(3 * time.Second)
}
