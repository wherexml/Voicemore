// Package macos is what the app needs from macOS beyond MyGo: the Fn key,
// microphone capture, the focused element of other apps and pasting into
// it. It calls the system frameworks through purego, without cgo, as MyGo
// does. Other systems get stubs that fail, for the pure Go packages to
// build and be tested there; none of it is a working implementation.
package macos

import (
	"context"
	"voiceinput/internal/config"
)

// OnMain runs a function on the main thread and waits for it; the app sets
// it to mygo.RunOnMain. AppKit objects are only touched through it.
var OnMain = func(fn func()) { fn() }

// Key names of the modifier keys a recording can start from.
const (
	KeyFn           = "Fn"
	KeyRightOption  = "RightOption"
	KeyRightCommand = "RightCommand"
	KeyRightControl = "RightControl"
)

// IsModifierKey says a trigger key is one of the keys above, which need the
// event tap; other triggers are accelerators of MyGo's global shortcuts.
func IsModifierKey(key string) bool {
	switch key {
	case KeyFn, KeyRightOption, KeyRightCommand, KeyRightControl:
		return true
	}
	return false
}

// InsertOptions says how text is pasted into a target.
type InsertOptions struct {
	// Context cancels before any clipboard write or paste.
	Context context.Context
	// RestoreDelayMs is how long the text stays on the clipboard before
	// the earlier content is put back.
	RestoreDelayMs int
	RemoteInputs   []config.RemoteInput
	// OnPasted starts observation before clipboard restoration. Local successful pastes only.
	// The callback may retain Target until observation finishes; the caller owns its release.
	OnPasted func(*Target)
}

// InsertResult says what could be checked about an insertion.
type InsertResult struct {
	ClipboardOnly bool
	// Remote reports a shortcut dispatched without remote readback.
	Remote bool
	Target *Target
	// FocusVerified means the current focus was verified as editable.
	FocusVerified bool
	// ClipboardRestored is false when the user copied something new in
	// the meantime, which was left alone.
	ClipboardRestored bool
}

// Application identifies a selectable running desktop client.
type Application struct{ Name, BundleID string }
