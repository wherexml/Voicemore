//go:build darwin

package macos

import "runtime"

// withPool runs fn on one thread inside an autorelease pool, so the
// Foundation objects it makes are freed whatever goroutine calls it.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	fn()
}
