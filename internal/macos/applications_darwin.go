//go:build darwin

package macos

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ebitengine/purego/objc"
)

// SelectableApplications includes installed GUI applications even when stopped,
// plus running apps outside the usual install locations. Bundle IDs are stable
// identities; never descend into app packages to expose their helper bundles.
func SelectableApplications() []Application {
	apps := map[string]Application{}
	add := func(a Application) {
		if a.BundleID == "" || a.BundleID == "dev.steve.voice-input" || a.BundleID == "dev.steve.voice-input.debug" {
			return
		}
		if _, exists := apps[a.BundleID]; !exists {
			apps[a.BundleID] = a
		}
	}
	for _, a := range RunningApplications() {
		add(a)
	}
	if load() != nil {
		return nil
	}
	OnMain(func() {
		pool := class("NSAutoreleasePool").Send(sel("new"))
		defer pool.Send(sel("drain"))
		home, _ := os.UserHomeDir()
		for _, root := range []string{"/Applications", filepath.Join(home, "Applications"), "/System/Applications", "/System/Library/CoreServices/Applications"} {
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || !d.IsDir() {
					return nil
				}
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				if !strings.EqualFold(filepath.Ext(path), ".app") {
					return nil
				}
				bundle := class("NSBundle").Send(sel("bundleWithPath:"), nsString(path))
				info := func(key string) objc.ID { return bundle.Send(sel("objectForInfoDictionaryKey:"), nsString(key)) }
				for _, key := range []string{"LSUIElement", "LSBackgroundOnly"} {
					if v := info(key); v != 0 && objc.Send[bool](v, sel("boolValue")) {
						return filepath.SkipDir
					}
				}
				name := goString(info("CFBundleDisplayName"))
				if name == "" {
					name = goString(info("CFBundleName"))
				}
				if name == "" {
					name = strings.TrimSuffix(d.Name(), filepath.Ext(path))
				}
				add(Application{Name: name, BundleID: goString(bundle.Send(sel("bundleIdentifier")))})
				return filepath.SkipDir
			})
		}
	})
	result := make([]Application, 0, len(apps))
	for _, a := range apps {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i].Name), strings.ToLower(result[j].Name)
		if a == b {
			return result[i].BundleID < result[j].BundleID
		}
		return a < b
	})
	return result
}
