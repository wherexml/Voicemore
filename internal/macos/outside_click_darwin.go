//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// WatchOutsideClicks observes mouse-down without consuming it. Unlike the
// framework's flyout monitor, it also runs while ShowInactive keeps typing in
// the target app. Register and dispose on the main thread.
func WatchOutsideClicks(handle uintptr, outside func()) func() {
	const mask = 1<<1 | 1<<3 | 1<<25
	events := class("NSEvent")
	global := objc.NewBlock(func(_ objc.Block, event objc.ID) { outside() })
	local := objc.NewBlock(func(_ objc.Block, event objc.ID) objc.ID {
		if event.Send(sel("window")) != objc.ID(handle) {
			outside()
		}
		return event
	})
	g := events.Send(sel("addGlobalMonitorForEventsMatchingMask:handler:"), uint64(mask), global)
	l := events.Send(sel("addLocalMonitorForEventsMatchingMask:handler:"), uint64(mask), local)
	global.Release()
	local.Release()
	return func() {
		if g != 0 {
			events.Send(sel("removeMonitor:"), g)
			g = 0
		}
		if l != 0 {
			events.Send(sel("removeMonitor:"), l)
			l = 0
		}
	}
}
