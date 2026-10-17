// Package distribution defines the stable app identity shared by builds and updates.
package distribution

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed policy.json
var policyJSON []byte

var Policy = func() struct {
	BundleID, Identity, TeamID, Requirement, Feed, PublicKey string
	DebugBundleID, DebugName, DebugIdentity                  string
} {
	var p struct {
		BundleID, Identity, TeamID, Requirement, Feed, PublicKey string
		DebugBundleID, DebugName, DebugIdentity                  string
	}
	if err := json.Unmarshal(policyJSON, &p); err != nil {
		panic(err)
	}
	return p
}()

func command(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func BundleValue(ctx context.Context, app, key string) (string, error) {
	return command(ctx, "/usr/bin/plutil", "-extract", key, "raw", filepath.Join(app, "Contents/Info.plist"))
}

// VerifyApp rejects other teams and development builds, including valid signatures
// sharing our bundle ID. A team requirement allows certificate renewal in that team.
func VerifyApp(ctx context.Context, app string) error {
	id, err := BundleValue(ctx, app, "CFBundleIdentifier")
	if err != nil {
		return err
	}
	if id != Policy.BundleID {
		return fmt.Errorf("unexpected bundle identifier: %s", id)
	}
	_, err = command(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", "-R="+Policy.Requirement, app)
	return err
}

func Requirement(ctx context.Context, app string) (string, error) {
	out, err := command(ctx, "/usr/bin/codesign", "-d", "-r-", app)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if r, ok := strings.CutPrefix(line, "designated => "); ok {
			return r, nil
		}
	}
	return "", fmt.Errorf("missing designated requirement")
}

// VerifyUpgrade runs before any bundle replacement. The installed app's actual
// requirement must accept the candidate, in addition to the pinned stable policy.
func VerifyUpgrade(ctx context.Context, installed, candidate string) error {
	if err := VerifyApp(ctx, installed); err != nil {
		return fmt.Errorf("installed app identity is not the stable identity; explicit migration required: %w", err)
	}
	if err := VerifyApp(ctx, candidate); err != nil {
		return fmt.Errorf("candidate identity: %w", err)
	}
	requirement, err := Requirement(ctx, installed)
	if err != nil {
		return err
	}
	if _, err = command(ctx, "/usr/bin/codesign", "--verify", "--strict", "-R="+requirement, candidate); err != nil {
		return fmt.Errorf("update would change permission identity: %w", err)
	}
	// Gatekeeper validates Developer ID notarization using the stapled ticket.
	_, err = command(ctx, "/usr/sbin/spctl", "--assess", "--type", "execute", candidate)
	return err
}
