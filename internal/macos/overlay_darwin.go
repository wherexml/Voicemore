//go:build darwin

package macos

import (
	"math"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// ConfigureOverlay keeps the nonactivating panel in ordinary and full-screen
// Spaces. Call on the UI thread; never activate the app or take keyboard focus.
func ConfigureOverlay(handle uintptr) {
	if handle == 0 {
		return
	}
	w := objc.ID(handle)
	const allSpaces = 1 << 0
	const moveToActiveSpace = 1 << 1
	const fullScreenAuxiliary = 1 << 8
	behavior := objc.Send[uint](w, sel("collectionBehavior"))
	w.Send(sel("setCollectionBehavior:"), (behavior&^moveToActiveSpace)|allSpaces|fullScreenAuxiliary)
	w.Send(sel("setHidesOnDeactivate:"), false)
	w.Send(sel("setLevel:"), 101) // NSPopUpMenuWindowLevel, same as MyGo's tray flyout.
}

var overlayWindows struct {
	sync.Once
	list func(uint32, uint32) uintptr
}

// ActiveOverlayWindow returns the frontmost app's foremost visible normal
// window. Window-server geometry avoids blocking AX queries and needs no new
// permission. Only IDs and bounds are read, never titles or pixels.
func ActiveOverlayWindow() (id uint32, frame SettingsFrame) {
	if load() != nil {
		return
	}
	overlayWindows.Do(func() {
		lib, err := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err == nil {
			purego.RegisterLibFunc(&overlayWindows.list, lib, "CGWindowListCopyWindowInfo")
		}
	})
	if overlayWindows.list == nil {
		return
	}
	pid := frontmostPID()
	list := overlayWindows.list(1|16, 0)
	if list == 0 {
		return
	}
	defer cfRelease(list)
	number := func(d objc.ID, key string) float64 {
		return objc.Send[float64](d.Send(sel("objectForKey:"), nsString(key)), sel("doubleValue"))
	}
	for i := 0; i < objc.Send[int](objc.ID(list), sel("count")); i++ {
		w := objc.ID(list).Send(sel("objectAtIndex:"), i)
		if int32(number(w, "kCGWindowOwnerPID")) != pid || number(w, "kCGWindowLayer") != 0 || number(w, "kCGWindowAlpha") <= 0 {
			continue
		}
		b := w.Send(sel("objectForKey:"), nsString("kCGWindowBounds"))
		frame = SettingsFrame{int(math.Round(number(b, "X"))), int(math.Round(number(b, "Y"))), int(math.Round(number(b, "Width"))), int(math.Round(number(b, "Height")))}
		if frame.Width > 0 && frame.Height > 0 {
			return uint32(number(w, "kCGWindowNumber")), frame
		}
	}
	return 0, SettingsFrame{}
}
