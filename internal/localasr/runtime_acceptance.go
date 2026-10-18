//go:build runtimeacceptance

package localasr

import "context"

// AcceptRuntime exercises the production extractor with an explicit fixture pin.
// It is excluded from application builds.
func (m *Manager) AcceptRuntime(ctx context.Context, sha256 string) error {
	return m.installRuntimeArchive(ctx, sha256)
}

// AcceptRuntimeDownload drives the production HTTPS acquisition and extraction.
// This override exists only in disposable acceptance executables.
func (m *Manager) AcceptRuntimeDownload(ctx context.Context, url, hash string, size int64, format string, compatible ...string) error {
	old := runtimeManifest
	defer func() { runtimeManifest = old }()
	runtimeManifest = runtimeSpec{URL: url, SHA256: hash, Bytes: size, Format: format, CompatibleInstalledSHA256: compatible}
	return m.installRuntimeArchive(ctx, hash)
}
