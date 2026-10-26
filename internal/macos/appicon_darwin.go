//go:build darwin

package macos

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"os"
	"path/filepath"
	"sync"
	"unsafe"
)

var iconDrawing struct {
	sync.Once
	draw func(objc.ID, objc.SEL, dragRect, dragRect, uint64, float64)
}

// AppIconPNG returns the system application icon, without Dock badges.
// Old records resolve only an unambiguous running application name.
func AppIconPNG(bundleID, name string) (out []byte) {
	if load() != nil {
		return nil
	}
	OnMain(func() {
		pool := objc.Send[objc.ID](class("NSAutoreleasePool"), sel("new"))
		defer objc.Send[objc.ID](pool, sel("drain"))
		ws := objc.Send[objc.ID](class("NSWorkspace"), sel("sharedWorkspace"))
		var icon objc.ID
		if bundleID != "" {
			url := objc.Send[objc.ID](ws, sel("URLForApplicationWithBundleIdentifier:"), nsString(bundleID))
			if url != 0 {
				path := objc.Send[objc.ID](url, sel("path"))
				icon = objc.Send[objc.ID](ws, sel("iconForFile:"), path)
			}
		} else if name != "" {
			apps := objc.Send[objc.ID](ws, sel("runningApplications"))
			matches := map[string]objc.ID{}
			for i, n := uint64(0), objc.Send[uint64](apps, sel("count")); i < n; i++ {
				app := objc.Send[objc.ID](apps, sel("objectAtIndex:"), i)
				if goString(objc.Send[objc.ID](app, sel("localizedName"))) == name {
					id := goString(objc.Send[objc.ID](app, sel("bundleIdentifier")))
					matches[id] = objc.Send[objc.ID](app, sel("icon"))
				}
			}
			// Installed top-level bundles disambiguate helper processes with
			// the same localized name (for example WeChat extensions).
			paths := installedAppPaths(name)
			if len(paths) > 0 {
				matches = map[string]objc.ID{}
				for _, path := range paths {
					bundle := objc.Send[objc.ID](class("NSBundle"), sel("bundleWithPath:"), nsString(path))
					id := goString(objc.Send[objc.ID](bundle, sel("bundleIdentifier")))
					if id == "" {
						id = path
					}
					matches[id] = objc.Send[objc.ID](ws, sel("iconForFile:"), nsString(path))
				}
			}
			if len(matches) == 1 {
				for _, img := range matches {
					icon = img
				}
			}
		}
		if icon == 0 {
			return
		}
		// Render only the 2x pixels needed for a 20-DIP icon. Serializing all
		// NSImage representations expands large icon catalogs into gigabytes.
		iconDrawing.Do(func() {
			lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				return
			}
			addr, err := purego.Dlsym(lib, "objc_msgSend")
			if err == nil {
				purego.RegisterFunc(&iconDrawing.draw, addr)
			}
		})
		if iconDrawing.draw == nil {
			return
		}
		rep := class("NSBitmapImageRep").Send(sel("alloc")).Send(sel("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:"), uintptr(0), 40, 40, 8, 4, true, false, nsString("NSDeviceRGBColorSpace"), 160, 32)
		if rep == 0 {
			return
		}
		defer rep.Send(sel("release"))
		ctx := class("NSGraphicsContext").Send(sel("graphicsContextWithBitmapImageRep:"), rep)
		if ctx == 0 {
			return
		}
		class("NSGraphicsContext").Send(sel("saveGraphicsState"))
		class("NSGraphicsContext").Send(sel("setCurrentContext:"), ctx)
		iconDrawing.draw(icon, sel("drawInRect:fromRect:operation:fraction:"), dragRect{S: dragSize{40, 40}}, dragRect{}, 1, 1)
		class("NSGraphicsContext").Send(sel("restoreGraphicsState"))
		png := objc.Send[objc.ID](rep, sel("representationUsingType:properties:"), uint64(4), objc.Send[objc.ID](class("NSDictionary"), sel("dictionary")))
		if png == 0 {
			return
		}
		n := objc.Send[uint64](png, sel("length"))
		ptr := objc.Send[uintptr](png, sel("bytes"))
		if n > 0 && n < 16*1024*1024 && ptr != 0 {
			out = append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(n))...)
		}
	})
	return
}

// Enumerate bundle names once; legacy names with multiple matches stay generic.
var installedNames map[string][]string

func installedAppPaths(name string) []string {
	if installedNames == nil {
		installedNames = map[string][]string{}
		home, _ := os.UserHomeDir()
		for _, root := range []string{"/Applications", "/System/Applications", filepath.Join(home, "Applications")} {
			filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil || !d.IsDir() {
					return nil
				}
				if filepath.Ext(path) != ".app" {
					return nil
				}
				bundle := objc.Send[objc.ID](class("NSBundle"), sel("bundleWithPath:"), nsString(path))
				names := map[string]bool{d.Name()[:len(d.Name())-4]: true}
				for _, key := range []string{"CFBundleName", "CFBundleDisplayName"} {
					value := goString(objc.Send[objc.ID](bundle, sel("objectForInfoDictionaryKey:"), nsString(key)))
					if value != "" {
						names[value] = true
					}
				}
				for n := range names {
					installedNames[n] = append(installedNames[n], path)
				}
				return filepath.SkipDir
			})
		}
	}
	return installedNames[name]
}
