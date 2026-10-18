//go:build updateacceptance

// Native update E2E harness. Never launches or replaces the user's app.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"voiceinput/internal/distribution"
	"voiceinput/internal/updateclient"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func run(name string, args ...string) {
	b, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("%s: %v: %s", name, err, b))
	}
}
func hashFile(p string) string {
	f, err := os.Open(p)
	must(err)
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	must(err)
	return hex.EncodeToString(h.Sum(nil))
}
func archiveApp(app, dest string) {
	run("/usr/bin/tar", "-czf", dest, "-C", filepath.Dir(app), filepath.Base(app))
}
func malicious(kind string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	if kind == "chained-symlink" {
		for _, h := range []*tar.Header{
			{Name: "Voicemore.app/d", Typeflag: tar.TypeDir, Mode: 0755},
			{Name: "Voicemore.app/b/c", Typeflag: tar.TypeSymlink, Linkname: "../d"},
			{Name: "Voicemore.app/a", Typeflag: tar.TypeSymlink, Linkname: "b/c/../../outside"},
		} {
			must(tr.WriteHeader(h))
		}
		must(tr.Close())
		must(gz.Close())
		return b.Bytes()
	}
	h := &tar.Header{Name: "Voicemore.app/Contents/evil", Typeflag: tar.TypeReg, Mode: 0644}
	switch kind {
	case "traversal":
		h.Name = "Voicemore.app/../../escaped"
	case "symlink":
		h.Typeflag = tar.TypeSymlink
		h.Linkname = "../../../escaped"
	case "hardlink":
		h.Typeflag = tar.TypeLink
		h.Linkname = "/tmp/escaped"
	case "oversized":
		h.Size = (1 << 30) + 1
	}
	must(tr.WriteHeader(h))
	if kind == "duplicate" {
		must(tr.WriteHeader(h))
	}
	if kind != "oversized" {
		must(tr.Close())
		must(gz.Close())
	} else {
		_ = gz.Close()
	}
	return b.Bytes()
}

func main() {
	must(os.Setenv("COPYFILE_DISABLE", "1"))
	old := flag.String("old", "", "real signed older app")
	archive := flag.String("archive", "", "real signed 0.3.8 archive")
	development := flag.String("development", "", "development-signed app with production ID")
	nextVersion := flag.String("next-version", "0.3.8", "archive bundle version")
	out := flag.String("out", "", "evidence output directory")
	flag.Parse()
	if *old == "" || *archive == "" || *development == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	must(os.MkdirAll(*out, 0700))
	work, err := os.MkdirTemp(*out, "e2e-")
	must(err)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	must(err)
	listener, err := net.Listen("tcp", "127.0.0.1:48149")
	must(err)
	var mode string
	var payload []byte
	var m map[string]any
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "http-error" {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/feed" {
			if mode == "malformed" {
				_, _ = w.Write([]byte("not JSON"))
				return
			}
			_ = json.NewEncoder(w).Encode(m)
			return
		}
		if mode == "truncated" {
			_, _ = w.Write(payload[:len(payload)/2])
			return
		}
		_, _ = w.Write(payload)
	})}
	go server.Serve(listener)
	defer server.Close()
	realArchive, err := os.ReadFile(*archive)
	must(err)
	devStage := filepath.Join(work, "development")
	must(os.Mkdir(devStage, 0700))
	devApp := filepath.Join(devStage, "Voicemore.app")
	run("/usr/bin/ditto", *development, devApp)
	devArchive := filepath.Join(work, "development.tar.gz")
	archiveApp(devApp, devArchive)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	must(distribution.VerifyApp(ctx, *old))
	cancel()
	extraPayloads := map[string][]byte{}
	for _, kind := range []string{"wrong-bundle", "invalid-code", "not-notarized"} {
		fixtureDir := filepath.Join(work, kind+"-fixture")
		must(os.Mkdir(fixtureDir, 0700))
		fixture := filepath.Join(fixtureDir, "Voicemore.app")
		run("/usr/bin/ditto", devApp, fixture)
		switch kind {
		case "wrong-bundle":
			run("/usr/bin/plutil", "-replace", "CFBundleIdentifier", "-string", "dev.steve.voice-input.fixture", filepath.Join(fixture, "Contents/Info.plist"))
		case "invalid-code":
			f, err := os.OpenFile(filepath.Join(fixture, "Contents/MacOS/Voicemore"), os.O_WRONLY|os.O_APPEND, 0)
			must(err)
			_, err = f.Write([]byte("tampered"))
			must(err)
			must(f.Close())
		}
		if kind != "invalid-code" {
			run("/usr/bin/codesign", "--force", "--options", "runtime", "--timestamp", "--sign", distribution.Policy.Identity, fixture)
		}
		file := filepath.Join(work, kind+".tar.gz")
		archiveApp(fixture, file)
		b, err := os.ReadFile(file)
		must(err)
		extraPayloads[kind] = b
	}
	devPayload, err := os.ReadFile(devArchive)
	must(err)
	cases := []string{"http-error", "malformed", "bad-signature", "wrong-size", "truncated", "cancelled", "traversal", "symlink", "chained-symlink", "hardlink", "duplicate", "oversized", "wrong-version", "development-candidate", "development-installed", "wrong-bundle", "invalid-code", "not-notarized", "no-downgrade", "prepare-discard", "commit-cancelled", "changed-target", "valid-upgrade"}
	results := []map[string]any{}
	for _, scenario := range cases {
		mode = scenario
		dir := filepath.Join(work, scenario)
		must(os.Mkdir(dir, 0700))
		target := filepath.Join(dir, "Voicemore.app")
		from := *old
		if scenario == "development-installed" {
			from = *development
		}
		run("/usr/bin/ditto", from, target)
		before := hashFile(filepath.Join(target, "Contents/MacOS/Voicemore"))
		payload = realArchive
		switch scenario {
		case "traversal", "symlink", "chained-symlink", "hardlink", "duplicate", "oversized":
			payload = malicious(scenario)
		case "development-candidate":
			payload = devPayload
		case "wrong-bundle", "invalid-code", "not-notarized":
			payload = extraPayloads[scenario]
		}
		h := sha256.Sum256(payload)
		signature := ed25519.Sign(private, h[:])
		if scenario == "bad-signature" {
			signature[0] ^= 1
		}
		m = map[string]any{"version": *nextVersion, "url": "http://127.0.0.1:48149/archive", "size": len(payload), "signature": base64.StdEncoding.EncodeToString(signature)}
		if scenario == "wrong-size" {
			m["size"] = len(payload) + 1
		}
		if scenario == "wrong-version" {
			m["version"] = "99.0.0"
		}
		if scenario == "no-downgrade" {
			m["version"] = "0.0.0"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		client := updateclient.Client{Target: target, Feed: "http://127.0.0.1:48149/feed", PublicKey: base64.StdEncoding.EncodeToString(pub)}
		up, actualErr := client.Check(ctx)
		if actualErr == nil && up != nil {
			if scenario == "cancelled" {
				cancel()
			}
			var prepared *updateclient.Prepared
			prepared, actualErr = up.Prepare(ctx, nil)
			if actualErr == nil {
				if hashFile(filepath.Join(target, "Contents/MacOS/Voicemore")) != before {
					panic("Prepare replaced the installed app")
				}
				switch scenario {
				case "prepare-discard":
				case "commit-cancelled":
					cancel()
					actualErr = prepared.Commit(ctx)
				case "changed-target":
					moved := target + ".moved"
					must(os.Rename(target, moved))
					run("/usr/bin/ditto", moved, target)
					actualErr = prepared.Commit(ctx)
				default:
					actualErr = prepared.Commit(ctx)
				}
				prepared.Close()
			}
		}
		cancel()
		after := hashFile(filepath.Join(target, "Contents/MacOS/Voicemore"))
		expected := map[string]string{
			"commit-cancelled": "context canceled", "changed-target": "installed app changed", "http-error": "HTTP 503", "malformed": "invalid character", "bad-signature": "archive signature rejected", "wrong-size": "size mismatch", "truncated": "size mismatch", "cancelled": "context canceled", "traversal": "unsafe or duplicate", "symlink": "unsafe update symlink", "chained-symlink": "unsafe update symlink", "hardlink": "unsupported update entry", "duplicate": "unsafe or duplicate", "oversized": "expansion limit", "wrong-version": "version does not match", "development-candidate": "candidate identity", "development-installed": "stable signing identity", "wrong-bundle": "unexpected bundle identifier", "invalid-code": "candidate identity", "not-notarized": "spctl",
		}
		passed := actualErr != nil && strings.Contains(actualErr.Error(), expected[scenario]) && before == after
		detail := ""
		if actualErr != nil {
			detail = actualErr.Error()
		}
		if scenario == "prepare-discard" {
			passed = actualErr == nil && before == after
		}
		if scenario == "no-downgrade" {
			passed = actualErr == nil && up == nil && before == after
		}
		if scenario == "valid-upgrade" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			v, vErr := distribution.BundleValue(ctx, target, "CFBundleShortVersionString")
			verifyErr := distribution.VerifyUpgrade(ctx, *old, target)
			backups, _ := filepath.Glob(filepath.Join(dir, ".Voicemore.app.update-*.previous.app"))
			backupOK := len(backups) == 1 && hashFile(filepath.Join(backups[0], "Contents/MacOS/Voicemore")) == before
			passed = actualErr == nil && vErr == nil && v == *nextVersion && verifyErr == nil && backupOK && after != before
			if verifyErr != nil {
				detail += verifyErr.Error()
			}
			cancel()
		}
		result := map[string]any{"case": scenario, "passed": passed, "error": detail, "before_sha256": before, "after_sha256": after}
		results = append(results, result)
		fmt.Printf("%s: %v %s\n", scenario, passed, strings.Split(detail, "\n")[0])
		b, err := json.MarshalIndent(results, "", "  ")
		must(err)
		must(os.WriteFile(filepath.Join(*out, "results.json"), b, 0600))
		// Preserve the successful installed/backup pair for read-only inspection.
		if passed && scenario != "valid-upgrade" {
			must(os.RemoveAll(dir))
		}
	}
	failed := false
	for _, r := range results {
		if !r["passed"].(bool) {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	// Keep the upgraded/backup pair and JSON evidence; discard generated inputs.
	for _, name := range []string{"development", "development.tar.gz", "wrong-bundle-fixture", "wrong-bundle.tar.gz", "invalid-code-fixture", "invalid-code.tar.gz", "not-notarized-fixture", "not-notarized.tar.gz"} {
		must(os.RemoveAll(filepath.Join(work, name)))
	}
}
