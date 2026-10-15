// Package localasr owns Voice Input's offline speech model and worker process.
// No oMLX or external Python installation is used at runtime.
package localasr

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const URL = "http://127.0.0.1:48132"
const Model = "Qwen3-ASR-1.7B-8bit"
const DefaultSource = ""

//go:embed assets/* assets/vad/*
var assets embed.FS

type fileEntry struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Repo     string      `json:"repo"`
	Revision string      `json:"revision"`
	Files    []fileEntry `json:"files"`
}

var modelManifest manifest

func init() {
	b, _ := assets.ReadFile("assets/model-manifest.json")
	if err := json.Unmarshal(b, &modelManifest); err != nil {
		panic(err)
	}
}
func ModelBytes() int64 {
	var n int64
	for _, f := range modelManifest.Files {
		n += f.Bytes
	}
	return n
}

type State struct {
	Phase       string // absent, installing, starting, ready, stopping, stopped, error
	Detail      string
	Done, Total int64
	Installed   bool
	PID         int
}

func (s State) Busy() bool {
	return s.Phase == "installing" || s.Phase == "starting" || s.Phase == "stopping"
}

type Manager struct {
	Root, Archive    string
	Port             int
	Changed          func()
	mu               sync.Mutex
	op               sync.Mutex
	state            State
	cancel           context.CancelFunc
	cmd              *exec.Cmd
	exited           chan struct{}
	closed, stopping bool
	idleTimeout      time.Duration
	idleTimer        *time.Timer
	idleGeneration   uint64
	users            int
}

func New(root, archive string, port ...int) *Manager {
	m := &Manager{Root: root, Archive: archive, Port: 48132, idleTimeout: 5 * time.Minute}
	if len(port) > 0 {
		if port[0] < 10000 || port[0] > 65535 {
			panic("invalid ASR port")
		}
		m.Port = port[0]
	}
	m.state = State{Phase: "absent", Total: ModelBytes()}
	if b, err := os.ReadFile(filepath.Join(root, "installed.json")); err == nil && strings.TrimSpace(string(b)) == modelManifest.Revision {
		m.state.Installed = true
		m.state.Phase = "stopped"
	}
	return m
}
func (m *Manager) Address() string { return fmt.Sprintf("http://127.0.0.1:%d", m.Port) }
func (m *Manager) Snapshot() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) update(fn func(*State)) {
	m.mu.Lock()
	fn(&m.state)
	cb := m.Changed
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}
func (m *Manager) phase(p, d string) { m.update(func(s *State) { s.Phase = p; s.Detail = d }) }
func (m *Manager) begin(phase string, work func(context.Context) error) {
	m.mu.Lock()
	if m.closed || m.stopping || m.cancel != nil || m.cmd != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state.Phase, m.state.Detail = phase, ""
	cb := m.Changed
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
	go func() {
		m.op.Lock()
		err := work(ctx)
		m.mu.Lock()
		m.cancel = nil
		stopping := m.stopping
		m.mu.Unlock()
		cancel()
		if err != nil && !stopping {
			if errors.Is(err, context.Canceled) {
				m.phase("stopped", "操作已取消，可重试")
			} else {
				m.phase("error", err.Error())
			}
		}
		m.op.Unlock()
	}()
}

// Deploy copies a local model when source is nonempty, otherwise downloads the
// pinned files. Both paths reuse only checksum-verified destination files.
func (m *Manager) Deploy(source string) {
	m.begin("installing", func(ctx context.Context) error {
		m.phase("installing", "正在准备独立推理环境")
		if err := m.installRuntime(ctx); err != nil {
			return err
		}
		if err := m.installModel(ctx, source); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(m.Root, "installed.json"), []byte(modelManifest.Revision), 0600); err != nil {
			return err
		}
		m.update(func(s *State) { s.Installed = true })
		return m.start(ctx)
	})
}
func (m *Manager) Start() {
	m.begin("starting", func(ctx context.Context) error {
		if !m.Snapshot().Installed {
			return errors.New("请先复制或下载 ASR 模型")
		}
		if err := m.installRuntime(ctx); err != nil {
			return err
		}
		return m.start(ctx)
	})
}
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return
	}
	m.stopping = true
	m.disarmIdleLocked()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.finishStop()
}
func (m *Manager) finishStop() {
	m.phase("stopping", "正在释放模型内存")
	go func() {
		m.op.Lock()
		m.stopProcess()
		m.mu.Lock()
		m.stopping = false
		m.state.Phase, m.state.Detail = "stopped", "模型已停止"
		cb := m.Changed
		m.mu.Unlock()
		if cb != nil {
			cb()
		}
		m.op.Unlock()
	}()
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.stopping = true
	m.disarmIdleLocked()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.op.Lock()
	defer m.op.Unlock()
	m.stopProcess()
	m.phase("stopped", "模型已停止")
}
func (m *Manager) stopProcess() {
	m.mu.Lock()
	cmd, done := m.cmd, m.exited
	m.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}
func (m *Manager) start(ctx context.Context) error {
	m.phase("starting", "正在校验并加载模型，首次启动需要一些时间")
	if err := m.verifyModel(ctx); err != nil {
		return err
	}
	// Never reuse or terminate another process on the app's private port.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", m.Port))
	if err != nil {
		return fmt.Errorf("本地语音端口 %d 被占用：%w", m.Port, err)
	}
	ln.Close()
	if err = m.writeWorker(); err != nil {
		return err
	}
	tokenBytes := make([]byte, 24)
	if _, err = rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	logPath := filepath.Join(m.Root, "worker.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(m.Root, "runtime/bin/python3.11"), "-I", filepath.Join(m.Root, "worker/run.py"))
	cmd.Dir = m.Root
	// Explicit environment prevents Python/user-site/loader overrides from leaking
	// in. All models and inference packages are owned by the application.
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "HF_HUB_OFFLINE=1", "TOKENIZERS_PARALLELISM=false", "PYTHONUNBUFFERED=1", "VOICE_INPUT_ASR_DATA=" + m.Root, "VOICE_INPUT_PORT=" + fmt.Sprint(m.Port), "VOICE_INPUT_PARENT=" + fmt.Sprint(os.Getpid()), "VOICE_INPUT_INSTANCE=" + token}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = ctx.Err(); err != nil {
		logFile.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	done := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.exited = done
	m.state.PID = cmd.Process.Pid
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		logFile.Close()
		m.mu.Lock()
		m.cmd = nil
		m.state.PID = 0
		stopping := m.stopping || m.closed
		m.mu.Unlock()
		close(done)
		if !stopping {
			detail := "识别进程已退出，请重新启动"
			if err != nil {
				detail += "：" + err.Error()
			}
			m.phase("error", detail+"。日志："+logPath)
		}
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopProcess()
			return ctx.Err()
		case <-done:
			return fmt.Errorf("模型加载失败，请查看 %s", logPath)
		case <-timer.C:
			m.stopProcess()
			return fmt.Errorf("模型加载超时，请查看 %s", logPath)
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.Address()+"/health", nil)
			res, e := client.Do(req)
			if e != nil {
				continue
			}
			var h struct {
				Ready    bool   `json:"ready"`
				Instance string `json:"instance"`
			}
			e = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&h)
			res.Body.Close()
			if e == nil && res.StatusCode == 200 && h.Ready && h.Instance == token {
				m.mu.Lock()
				if m.cmd != cmd || m.stopping || m.closed || ctx.Err() != nil {
					m.mu.Unlock()
					m.stopProcess()
					return errors.New("模型启动已取消或进程已退出")
				}
				m.state.Phase, m.state.Detail = "ready", "模型已就绪 · 完全本地运行"
				m.armIdleLocked()
				cb := m.Changed
				m.mu.Unlock()
				if cb != nil {
					cb()
				}
				return nil
			}
		}
	}
}
func (m *Manager) writeWorker() error {
	return fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		dest := filepath.Join(m.Root, "worker", strings.TrimPrefix(path, "assets/"))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		return os.WriteFile(dest, b, 0600)
	})
}
func (m *Manager) installRuntime(ctx context.Context) error {
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return err
	}
	hashBytes, _ := assets.ReadFile("assets/runtime.sha256")
	expected := strings.TrimSpace(string(hashBytes))
	stamp := filepath.Join(m.Root, "runtime/version.sha256")
	if b, e := os.ReadFile(stamp); e == nil && strings.TrimSpace(string(b)) == expected {
		if _, e = os.Stat(filepath.Join(m.Root, "runtime/bin/python3.11")); e == nil {
			return nil
		}
	}
	f, err := os.Open(m.Archive)
	if err != nil {
		return fmt.Errorf("应用缺少独立 ASR 运行环境，请重新安装完整应用：%w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err = copyContext(ctx, h, f, nil); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("独立推理环境校验失败，请重新安装应用")
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	stage, err := os.MkdirTemp(m.Root, "runtime-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if !strings.HasPrefix(name, "runtime/") && name != "runtime" {
			return errors.New("运行环境包含非法路径")
		}
		target := filepath.Join(stage, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0700)
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			var out *os.File
			out, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode)&0777)
			if err == nil {
				_, err = copyContext(ctx, out, tr, nil)
				ce := out.Close()
				if err == nil {
					err = ce
				}
			}
		case tar.TypeSymlink:
			link := filepath.Clean(filepath.Join(filepath.Dir(name), hdr.Linkname))
			if filepath.IsAbs(hdr.Linkname) || !strings.HasPrefix(link, "runtime/") {
				return errors.New("运行环境包含外部链接")
			}
			if err = os.MkdirAll(filepath.Dir(target), 0700); err == nil {
				err = os.Symlink(hdr.Linkname, target)
			}
		default:
			return fmt.Errorf("不支持的运行环境文件 %s", name)
		}
		if err != nil {
			return err
		}
	}
	final := filepath.Join(m.Root, "runtime")
	if err = os.RemoveAll(final); err != nil {
		return err
	}
	if err = os.Rename(filepath.Join(stage, "runtime"), final); err != nil {
		return err
	}
	return os.WriteFile(stamp, []byte(expected), 0600)
}
func (m *Manager) verifyModel(ctx context.Context) error {
	for _, f := range modelManifest.Files {
		if err := verify(ctx, filepath.Join(m.Root, "model", f.File), f); err != nil {
			return fmt.Errorf("模型文件 %s 不完整，请重新部署：%w", f.File, err)
		}
	}
	return nil
}
func verify(ctx context.Context, path string, entry fileEntry) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() != entry.Bytes {
		return errors.New("文件大小不符")
	}
	h := sha256.New()
	if _, err = copyContext(ctx, h, f, nil); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return errors.New("SHA256 不符")
	}
	return nil
}
func (m *Manager) installModel(ctx context.Context, source string) error {
	dest := filepath.Join(m.Root, "model")
	if err := os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	var completed int64
	m.update(func(s *State) { s.Done = 0; s.Total = ModelBytes() })
	for _, f := range modelManifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(dest, f.File)
		if verify(ctx, path, f) == nil {
			completed += f.Bytes
			m.update(func(s *State) { s.Done = completed })
			continue
		}
		verb := "下载"
		if source != "" {
			verb = "复制"
		}
		m.phase("installing", "正在"+verb+" "+f.File)
		var input io.ReadCloser
		if source != "" {
			var err error
			input, err = os.Open(filepath.Join(source, f.File))
			if err != nil {
				return fmt.Errorf("本地模型不可用：%w", err)
			}
		} else {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://huggingface.co/"+modelManifest.Repo+"/resolve/"+modelManifest.Revision+"/"+f.File, nil)
			client := &http.Client{Timeout: 60 * time.Minute}
			res, err := client.Do(req)
			if err != nil {
				return err
			}
			if res.StatusCode != http.StatusOK {
				res.Body.Close()
				return fmt.Errorf("下载 %s 失败：HTTP %d", f.File, res.StatusCode)
			}
			input = res.Body
		}
		temp := path + ".partial"
		out, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return err
		}
		hash := sha256.New()
		var n int64
		last := time.Now()
		n, err = copyContext(ctx, io.MultiWriter(out, hash), io.LimitReader(input, f.Bytes+1), func(count int64) {
			if time.Since(last) > 200*time.Millisecond {
				m.update(func(s *State) { s.Done = completed + count })
				last = time.Now()
			}
		})
		input.Close()
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil && (n != f.Bytes || hex.EncodeToString(hash.Sum(nil)) != f.SHA256) {
			err = errors.New("文件大小或 SHA256 不符")
		}
		if err != nil {
			os.Remove(temp)
			return fmt.Errorf("%s %s 失败：%w", verb, f.File, err)
		}
		if err = os.Rename(temp, path); err != nil {
			return err
		}
		completed += f.Bytes
		m.update(func(s *State) { s.Done = completed })
	}
	return nil
}
func copyContext(ctx context.Context, dst io.Writer, src io.Reader, progress func(int64)) (int64, error) {
	b := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, e := src.Read(b)
		if n > 0 {
			written, err := dst.Write(b[:n])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
			if progress != nil {
				progress(total)
			}
		}
		if e == io.EOF {
			return total, nil
		}
		if e != nil {
			return total, e
		}
	}
}
