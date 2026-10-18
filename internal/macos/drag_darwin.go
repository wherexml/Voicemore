//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// PrimaryMouseDown reads AppKit's current button state, including its native
// drag tracking loop. It does not install an input monitor or ask for access.
func PrimaryMouseDown() bool {
	if load() != nil {
		return false
	}
	var down bool
	OnMain(func() {
		down = objc.Send[uint64](class("NSEvent"), sel("pressedMouseButtons"))&1 != 0
	})
	return down
}
