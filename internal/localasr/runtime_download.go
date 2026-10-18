package localasr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type runtimeSpec struct {
	CompatibleInstalledSHA256 []string `json:"compatible_installed_sha256,omitempty"`
	URL                       string   `json:"url"`
	Bytes                     int64    `json:"bytes"`
	SHA256                    string   `json:"sha256"`
	Format                    string   `json:"format"`
}

var runtimeManifest runtimeSpec

func init() {
	b, err := assets.ReadFile("assets/runtime-manifest.json")
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(b, &runtimeManifest); err != nil {
		panic(err)
	}
}

// Runtime acquisition belongs exclusively to local model deployment/start.
// New() and cloud dictation never call this path.
func (m *Manager) downloadRuntime(ctx context.Context, spec runtimeSpec) (string, error) {
	if spec.Bytes <= 0 || spec.Bytes > 2<<30 || len(spec.SHA256) != 64 || (spec.Format != "gzip" && spec.Format != "bzip2") {
		return "", errors.New("invalid runtime manifest")
	}
	if _, err := hex.DecodeString(spec.SHA256); err != nil {
		return "", err
	}
	checkURL := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("runtime download requires HTTPS")
		}
		return nil
	}
	if err := checkURL(spec.URL); err != nil {
		return "", err
	}
	dir := filepath.Join(m.Root, "downloads")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	suffix := ".tar.gz"
	if spec.Format == "bzip2" {
		suffix = ".tar.bz2"
	}
	dest := filepath.Join(dir, spec.SHA256+suffix)
	entry := fileEntry{Bytes: spec.Bytes, SHA256: spec.SHA256}
	if verify(ctx, dest, entry) == nil {
		return dest, nil
	}
	m.phase("installing", "正在下载本地识别环境")
	m.update(func(s *State) { s.Done = 0; s.Total = spec.Bytes })
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many runtime redirects")
		}
		return checkURL(req.URL.String())
	}}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载本地识别环境失败：HTTP %d", res.StatusCode)
	}
	f, err := os.CreateTemp(dir, ".runtime-partial-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	last := time.Time{}
	n, copyErr := copyContext(ctx, io.MultiWriter(f, hash), io.LimitReader(res.Body, spec.Bytes+1), func(done int64) {
		if time.Since(last) >= 200*time.Millisecond {
			last = time.Now()
			m.update(func(s *State) { s.Done = done })
		}
	})
	if err := errors.Join(copyErr, f.Close()); err != nil {
		return "", err
	}
	if n != spec.Bytes || hex.EncodeToString(hash.Sum(nil)) != spec.SHA256 {
		return "", errors.New("本地识别环境大小或 SHA256 不符，请重试")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.Rename(f.Name(), dest); err != nil {
		return "", err
	}
	m.update(func(s *State) { s.Done = spec.Bytes })
	return dest, nil
}

// DownloadBytes is the complete optional local recognition download.
func DownloadBytes() int64 { return ModelBytes() + runtimeManifest.Bytes }
