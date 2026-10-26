package subtitles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// The short-segment variant measured in tools/system-audio-prototype: an
// 8 s target and a 9 s hard limit instead of dictation's 30/32 s. Only these
// two lines of a private copy change; the installed worker is never edited.
const (
	qwenShortTarget    = 8
	qwenShortExtension = 1
	productionASRPort  = 48132
	qwenReadyTimeout   = 180 * time.Second
)

var (
	maxSegmentLine   = regexp.MustCompile(`(?m)^MAX_SEGMENT=30\b`)
	maxExtensionLine = regexp.MustCompile(`(?m)^MAX_SEGMENT_EXTENSION=2\b`)
)

type qwenEngine struct {
	p    *proc
	conn atomic.Pointer[websocket.Conn] // set once connected; read by close from any goroutine
}

// prepareShortWorker copies the installed worker into dir and patches only
// its segment bounds, refusing a worker whose lines differ from the
// verified version.
func prepareShortWorker(root, dir string) error {
	src := filepath.Join(root, "worker")
	if err := os.RemoveAll(dir); err != nil {
		return fail(ErrQwenWorker, "%v", err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dir, rel), 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o600)
	})
	if err != nil {
		return fail(ErrQwenWorker, "copy worker: %v", err)
	}
	server := filepath.Join(dir, "server.py")
	b, err := os.ReadFile(server)
	if err != nil {
		return fail(ErrQwenWorker, "%v", err)
	}
	s := string(b)
	if len(maxSegmentLine.FindAllStringIndex(s, -1)) != 1 || len(maxExtensionLine.FindAllStringIndex(s, -1)) != 1 {
		return fail(ErrQwenWorker, "installed server.py lacks MAX_SEGMENT=30 / MAX_SEGMENT_EXTENSION=2")
	}
	s = maxSegmentLine.ReplaceAllString(s, fmt.Sprintf("MAX_SEGMENT=%d", qwenShortTarget))
	s = maxExtensionLine.ReplaceAllString(s, fmt.Sprintf("MAX_SEGMENT_EXTENSION=%d", qwenShortExtension))
	return os.WriteFile(server, []byte(s), 0o600)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func startQwen(ctx context.Context, cfg Config, text func(Kind, int, string, timing)) (engine, <-chan error, error) {
	root := cfg.QwenRoot
	py := filepath.Join(root, "runtime/bin/python3.11")
	for _, p := range []string{py, filepath.Join(root, "worker/run.py"), filepath.Join(root, "worker/server.py"), filepath.Join(root, "model")} {
		if _, err := os.Stat(p); err != nil {
			return nil, nil, fail(ErrQwenMissing, "%s", p)
		}
	}
	dir := filepath.Join(cfg.WorkDir, "qwen-short-worker")
	if err := prepareShortWorker(root, dir); err != nil {
		return nil, nil, err
	}
	port, err := freePort()
	if err != nil || port == productionASRPort {
		return nil, nil, fail(ErrEngineStart, "no isolated port: %v", err)
	}
	tok := make([]byte, 16)
	rand.Read(tok)
	token := hex.EncodeToString(tok)
	logOut, _ := os.Create(filepath.Join(cfg.WorkDir, "qwen-worker.log"))
	cmd := exec.Command(py, "-I", filepath.Join(dir, "run.py"))
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1",
		"TOKENIZERS_PARALLELISM=false", "PYTHONUNBUFFERED=1", "VOICE_INPUT_ASR_DATA=" + root, "VOICE_INPUT_PORT=" + fmt.Sprint(port),
		"VOICE_INPUT_PARENT=" + fmt.Sprint(os.Getpid()), "VOICE_INPUT_INSTANCE=" + token}
	cmd.Stdout, cmd.Stderr = logOut, logOut
	p := newProc(cmd)
	if err := cmd.Start(); err != nil {
		return nil, nil, fail(ErrEngineStart, "%v", err)
	}
	e := &qwenEngine{p: p}
	go func() {
		p.wait()
		if logOut != nil {
			logOut.Close()
		}
	}()
	// A hung worker must not outlive the session: cancelling closes it,
	// whatever Run is blocked on.
	go func() {
		select {
		case <-ctx.Done():
			e.close()
		case <-p.waited:
		}
	}()
	cfg.log("qwen_worker_start", map[string]any{"pid": cmd.Process.Pid, "port": port, "segment_s": qwenShortTarget, "hard_s": qwenShortTarget + qwenShortExtension})

	if err := waitHealthy(ctx, port, token, p); err != nil {
		e.close()
		return nil, nil, err
	}
	q := url.Values{"language": {cfg.Language}}
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws?%s", port, q.Encode()), nil)
	if err != nil {
		e.close()
		return nil, nil, fail(ErrEngineStart, "connect: %v", err)
	}
	conn.SetReadLimit(1 << 20)
	e.conn.Store(conn)
	_, first, err := conn.Read(ctx)
	var ready struct{ Type string }
	if err != nil || json.Unmarshal(first, &ready) != nil || ready.Type != "ready" {
		e.close()
		return nil, nil, fail(ErrEngineStart, "no ready: %v %s", err, first)
	}

	errs := make(chan error, 2)
	go func() {
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				errs <- fail(ErrEngineFailed, "read: %v", err)
				return
			}
			var m struct {
				Type    string   `json:"type"`
				Segment int      `json:"segment"`
				Text    string   `json:"text"`
				Message string   `json:"message"`
				AudioS  *float64 `json:"audio_s"` // the worker's audio_s, as it defines it
			}
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			t := noTiming
			if m.AudioS != nil {
				t.audioS = *m.AudioS
			}
			switch m.Type {
			case "partial":
				text(Partial, m.Segment, strings.TrimSpace(m.Text), t)
			case "final":
				text(Final, m.Segment, strings.TrimSpace(m.Text), t)
			case "error":
				errs <- fail(ErrEngineFailed, "worker: %s", m.Message)
				return
			}
		}
	}()
	go func() {
		<-p.waited
		if ctx.Err() == nil {
			errs <- fail(ErrEngineFailed, "worker exited: %v", p.err)
		}
	}()
	return e, errs, nil
}

func waitHealthy(ctx context.Context, port int, token string, p *proc) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(qwenReadyTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.waited:
			return fail(ErrEngineStart, "worker exited: %v", p.err)
		case <-time.After(250 * time.Millisecond):
		}
		if r, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port)); err == nil {
			var h struct {
				Instance string `json:"instance"`
				Ready    bool   `json:"ready"`
			}
			json.NewDecoder(r.Body).Decode(&h)
			r.Body.Close()
			if h.Instance == token && h.Ready {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fail(ErrEngineStart, "worker not ready within %s", qwenReadyTimeout)
		}
	}
}

func (e *qwenEngine) feed(ctx context.Context, pcm []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.conn.Load().Write(wctx, websocket.MessageBinary, pcm); err != nil {
		return fail(ErrEngineFailed, "send: %v", err)
	}
	return nil
}

// close ends the connection at once (no close handshake a hung worker
// would stall) and the worker's whole process group, and returns once it
// is gone or the bound passed. It may be called more than once.
func (e *qwenEngine) close() {
	if c := e.conn.Load(); c != nil {
		c.CloseNow()
	}
	e.p.stop(5 * time.Second)
}
