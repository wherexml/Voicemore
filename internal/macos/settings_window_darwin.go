//go:build darwin

package macos

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"math"
	"sync"
)

var settingsWindows struct {
	sync.Once
	list func(uint32, uint32) uintptr
}

// SystemSettingsFrame reads window-server geometry without requesting AX or
// screen-recording access. No window titles or pixels are read. Main-thread only.
func SystemSettingsFrame() (frame SettingsFrame, visible bool) {
	if load() != nil {
		return
	}
	settingsWindows.Do(func() {
		lib, err := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err == nil {
			purego.RegisterLibFunc(&settingsWindows.list, lib, "CGWindowListCopyWindowInfo")
		}
	})
	if settingsWindows.list == nil {
		return
	}
	apps := class("NSRunningApplication").Send(sel("runningApplicationsWithBundleIdentifier:"), nsString("com.apple.systempreferences"))
	pids := map[int]bool{}
	for i := 0; i < objc.Send[int](apps, sel("count")); i++ {
		pids[objc.Send[int](apps.Send(sel("objectAtIndex:"), i), sel("processIdentifier"))] = true
	}
	if len(pids) == 0 {
		return
	}
	list := settingsWindows.list(1|16, 0) // on-screen, excluding desktop elements
	if list == 0 {
		return
	}
	defer cfRelease(list)
	number := func(d objc.ID, key string) float64 {
		return objc.Send[float64](d.Send(sel("objectForKey:"), nsString(key)), sel("doubleValue"))
	}
	for i := 0; i < objc.Send[int](objc.ID(list), sel("count")); i++ {
		w := objc.ID(list).Send(sel("objectAtIndex:"), i)
		if !pids[int(number(w, "kCGWindowOwnerPID"))] || number(w, "kCGWindowLayer") != 0 || number(w, "kCGWindowAlpha") <= 0 {
			continue
		}
		b := w.Send(sel("objectForKey:"), nsString("kCGWindowBounds"))
		f := SettingsFrame{int(math.Round(number(b, "X"))), int(math.Round(number(b, "Y"))), int(math.Round(number(b, "Width"))), int(math.Round(number(b, "Height")))}
		if f.Width >= 320 && f.Height >= 240 && f.Width*f.Height > frame.Width*frame.Height {
			frame, visible = f, true
		}
	}
	return
}
