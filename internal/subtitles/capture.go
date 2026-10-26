package subtitles

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"time"
)

// capture is a running VoicemoreAudioTap: PCM in 100 ms chunks, a signal
// while it hears only digital silence, and done when it ends.
type capture struct {
	p      *proc
	stdin  io.WriteCloser
	pcm    chan []byte
	silent chan struct{}
	done   chan error
}

func startCapture(ctx context.Context, cfg Config, id uint64) (*capture, error) {
	if st, err := os.Stat(cfg.TapPath); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return nil, fail(ErrTapMissing, "%s", cfg.TapPath)
	}
	cmd := exec.Command(cfg.TapPath)
	p := newProc(cmd)
	stdin, err := cmd.StdinPipe() // held open: its EOF stops the helper if this app goes away
	if err != nil {
		return nil, fail(ErrCapture, "%v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fail(ErrCapture, "%v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fail(ErrCapture, "%v", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fail(ErrCapture, "start %s: %v", cfg.TapPath, err)
	}
	c := &capture{p: p, stdin: stdin, pcm: make(chan []byte, queueChunks), silent: make(chan struct{}, 1), done: make(chan error, 1)}
	cfg.log("capture_start", map[string]any{"session": id, "pid": cmd.Process.Pid})

	var lastErr atomic.Value // the helper's last reported error, for a failed exit
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			var e map[string]any
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			e["session"] = id
			ev, _ := e["event"].(string)
			delete(e, "event")
			cfg.log("capture_"+ev, e)
			switch ev {
			case "zero_audio_warning":
				select {
				case c.silent <- struct{}{}:
				default:
				}
			case "error":
				if m, ok := e["message"].(string); ok {
					lastErr.Store(m)
				}
			case "no_callbacks":
				lastErr.Store(ErrNoCallbacks)
			}
		}
	}()
	go func() {
		var frames, dropped int64
		r := bufio.NewReaderSize(stdout, chunkBytes*4)
		for {
			buf := make([]byte, chunkBytes)
			n, err := io.ReadFull(r, buf)
			if n > 0 {
				frames += int64(n / 2)
				select {
				case c.pcm <- buf[:n]:
				default:
					// The engine is behind: keep the newest audio, count what is lost.
					select {
					case <-c.pcm:
						dropped++
					default:
					}
					select {
					case c.pcm <- buf[:n]:
					default:
					}
					if dropped%10 == 1 {
						cfg.log("capture_dropped", map[string]any{"session": id, "dropped_chunks": dropped})
					}
				}
			}
			if err != nil {
				break
			}
		}
		<-stderrDone // Wait closes the pipes: read the last status lines first
		werr := p.wait()
		cfg.log("capture_exit", map[string]any{"session": id, "pcm_frames": frames, "dropped_chunks": dropped, "exit": exitText(werr)})
		var out error
		switch m, _ := lastErr.Load().(string); {
		case m == ErrNoCallbacks:
			out = fail(ErrNoCallbacks, "no audio buffers within 3 s")
		case m != "":
			out = fail(ErrCapture, "%s", m)
		default:
			out = fail(ErrCapture, "helper ended: %s", exitText(werr))
		}
		c.done <- out
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.close()
		case <-p.waited:
		}
	}()
	return c, nil
}

// close stops the helper, which tears its tap down on SIGTERM, and returns
// once it is gone or the bound passed. It may be called more than once.
func (c *capture) close() {
	c.stdin.Close()
	c.p.stop(2 * time.Second)
}

func exitText(err error) string {
	if err == nil {
		return "0"
	}
	return err.Error()
}
