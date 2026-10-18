// Package updateclient authenticates and stages updates before replacing an app.
package updateclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"voiceinput/internal/distribution"
)

const maxArchive = 2 << 30

type manifest struct {
	Version, Notes, Date, URL, Signature string
	Size                                 int64
}
type Update struct {
	Version, Notes, Date string
	manifest             manifest
	client               Client
}

// Client's explicit inputs also allow repeatable E2E runs against disposable app
// copies and a loopback feed. Stable() is the only constructor used by the app.
type Client struct{ Target, Feed, PublicKey string }

func Stable() (Client, error) {
	exe, err := os.Executable()
	if err != nil {
		return Client{}, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return Client{}, err
	}
	return Client{Target: filepath.Dir(filepath.Dir(filepath.Dir(exe))), Feed: distribution.Policy.Feed, PublicKey: distribution.Policy.PublicKey}, nil
}

func version(s string) ([3]uint64, error) {
	var v [3]uint64
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("invalid stable version: %q", s)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil || p == "" {
			return v, fmt.Errorf("invalid stable version: %q", s)
		}
		v[i] = n
	}
	return v, nil
}

func newer(next, current string) (bool, error) {
	n, err := version(next)
	if err != nil {
		return false, err
	}
	c, err := version(current)
	if err != nil {
		return false, err
	}
	for i := range n {
		if n[i] != c[i] {
			return n[i] > c[i], nil
		}
	}
	return false, nil
}

func validURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" {
		return fmt.Errorf("invalid update URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
		return nil
	}
	return fmt.Errorf("update URL must use HTTPS")
}

func get(ctx context.Context, raw string) (*http.Response, error) {
	if err := validURL(raw); err != nil {
		return nil, err
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many update redirects")
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("insecure update redirect")
		}
		return validURL(req.URL.String())
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c Client) Check(ctx context.Context) (*Update, error) {
	if err := distribution.VerifyApp(ctx, c.Target); err != nil {
		return nil, fmt.Errorf("automatic updates require the stable signing identity: %w", err)
	}
	current, err := distribution.BundleValue(ctx, c.Target, "CFBundleShortVersionString")
	if err != nil {
		return nil, err
	}
	resp, err := get(ctx, c.Feed)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("update manifest too large")
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if m.Size <= 0 || m.Size > maxArchive {
		return nil, fmt.Errorf("invalid update size")
	}
	if err := validURL(m.URL); err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid update signature")
	}
	isNew, err := newer(m.Version, current)
	if err != nil {
		return nil, err
	}
	if !isNew {
		return nil, nil
	}
	return &Update{Version: m.Version, Notes: m.Notes, Date: m.Date, manifest: m, client: c}, nil
}

type progressWriter struct {
	file        io.Writer
	hash        io.Writer
	done, total int64
	progress    func(int64, int64)
}

func (w *progressWriter) Write(b []byte) (int, error) {
	n, err := w.file.Write(b)
	_, _ = w.hash.Write(b[:n])
	w.done += int64(n)
	if w.progress != nil {
		w.progress(w.done, w.total)
	}
	return n, err
}

func (u *Update) Install(ctx context.Context, progress func(int64, int64)) error {
	p, err := u.Prepare(ctx, progress)
	if err != nil {
		return err
	}
	defer p.Close()
	return p.Commit(ctx)
}

func (u *Update) Prepare(ctx context.Context, progress func(int64, int64)) (*Prepared, error) {
	c, m := u.client, u.manifest
	before, err := os.Lstat(c.Target)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("update target must be a real app directory")
	}
	if err := distribution.VerifyApp(ctx, c.Target); err != nil {
		return nil, err
	}
	current, err := distribution.BundleValue(ctx, c.Target, "CFBundleShortVersionString")
	if err != nil {
		return nil, err
	}
	if ok, err := newer(m.Version, current); err != nil || !ok {
		return nil, fmt.Errorf("update no longer newer than installed app")
	}
	work, err := os.MkdirTemp(filepath.Dir(c.Target), ".Voicemore.app.update-")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(work)
		}
	}()
	archive, err := os.Create(filepath.Join(work, "archive.tar.gz"))
	if err != nil {
		return nil, err
	}
	resp, err := get(ctx, m.URL)
	if err != nil {
		archive.Close()
		return nil, err
	}
	hash := sha256.New()
	w := &progressWriter{file: archive, hash: hash, total: m.Size, progress: progress}
	_, copyErr := io.Copy(w, io.LimitReader(resp.Body, m.Size+1))
	resp.Body.Close()
	closeErr := archive.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return nil, err
	}
	if w.done != m.Size {
		return nil, fmt.Errorf("update size mismatch")
	}
	key, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid update public key")
	}
	sig, _ := base64.StdEncoding.DecodeString(m.Signature)
	if !ed25519.Verify(key, hash.Sum(nil), sig) {
		return nil, fmt.Errorf("update archive signature rejected")
	}
	extracted := filepath.Join(work, "extracted")
	if err := extract(ctx, filepath.Join(work, "archive.tar.gz"), extracted); err != nil {
		return nil, err
	}
	candidate := filepath.Join(extracted, "Voicemore.app")
	actual, err := distribution.BundleValue(ctx, candidate, "CFBundleShortVersionString")
	if err != nil {
		return nil, err
	}
	if actual != m.Version {
		return nil, fmt.Errorf("update bundle version does not match manifest")
	}
	if err := distribution.VerifyUpgrade(ctx, c.Target, candidate); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true
	return &Prepared{target: c.Target, candidate: candidate, work: work, before: before}, nil
}

// Prepared owns a verified staging directory. Preparing never replaces the running app.
// Call Commit only after the user requests restart and active work has finished.
type Prepared struct {
	target, candidate, work string
	before                  os.FileInfo
}

func (p *Prepared) Close() { _ = os.RemoveAll(p.work) }
func (p *Prepared) Commit(ctx context.Context) error {
	if err := distribution.VerifyUpgrade(ctx, p.target, p.candidate); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := os.Lstat(p.target)
	if err != nil {
		return err
	}
	if !os.SameFile(p.before, after) {
		return fmt.Errorf("installed app changed during update")
	}
	// Retain the previous sealed bundle for recovery. The backup is outside the
	// disposable staging directory and never replaced by a later failed update.
	backup := p.work + ".previous.app"
	if err := os.Rename(p.target, backup); err != nil {
		return err
	}
	if err := os.Rename(p.candidate, p.target); err != nil {
		if rollback := os.Rename(backup, p.target); rollback != nil {
			return fmt.Errorf("install failed: %v; restore %s: %w", err, backup, rollback)
		}
		return err
	}
	return nil
}

func extract(ctx context.Context, archive, dest string) error {
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	links := map[string]string{}
	var expanded int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name != path.Clean(name) || (name != "Voicemore.app" && !strings.HasPrefix(name, "Voicemore.app/")) || seen[name] {
			return fmt.Errorf("unsafe or duplicate update path: %s", name)
		}
		seen[name] = true
		if err := root.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			expanded += h.Size
			if h.Size < 0 || h.Size > 1<<30 || expanded > 4<<30 {
				return fmt.Errorf("update expansion limit")
			}
			out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(h.Mode)&0755)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			closeErr := out.Close()
			if err = errors.Join(err, closeErr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			target := path.Clean(path.Join(path.Dir(name), h.Linkname))
			if path.IsAbs(h.Linkname) || !strings.HasPrefix(target, "Voicemore.app/") {
				return fmt.Errorf("unsafe update symlink: %s", name)
			}
			links[name] = h.Linkname
		default:
			return fmt.Errorf("unsupported update entry: %s", name)
		}
	}
	// Create links last; archive content can never be written through a symlink.
	for name, target := range links {
		if err := root.Symlink(target, name); err != nil {
			return err
		}
	}
	// Lexical checks alone do not catch ".." traversing another symlink.
	// Resolve the completed graph and reject dangling/cyclic/external links.
	for name := range links {
		resolved, err := filepath.EvalSymlinks(filepath.Join(dest, name))
		if err != nil {
			return fmt.Errorf("unsafe update symlink: %s: %w", name, err)
		}
		rel, err := filepath.Rel(filepath.Join(dest, "Voicemore.app"), resolved)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("unsafe update symlink: %s", name)
		}
	}

	return nil
}
