// Package macos is what the app needs from macOS beyond MyGo: the Fn key,
// microphone capture, the focused element of other apps and pasting into
// it. It calls the system frameworks through purego, without cgo, as MyGo
// does. Other systems get stubs that fail, for the pure Go packages to
// build and be tested there; none of it is a working implementation.
package macos

import (
	"context"
	"sort"
	"strings"
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
	case KeyFn, KeyRightOption, KeyRightCommand, KeyRightControl, "LeftOption", "LeftCommand", "LeftControl", "LeftShift", "RightShift":
		return true
	}
	return false
}

// NativeShortcutIdentity canonicalizes shortcuts that Carbon cannot register:
// modifier-only chords and combinations containing Fn or sided modifiers.
func NativeShortcutIdentity(key string) string {
	parts := strings.Split(key, "+")
	mods := []string{}
	ordinary := ""
	native := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch strings.ToLower(part) {
		case "cmd", "command", "meta", "super", "cmdorctrl", "commandorcontrol":
			part = "Cmd"
		case "ctrl", "control":
			part = "Ctrl"
		case "alt", "option", "altgr":
			part = "Alt"
		case "shift":
			part = "Shift"
		}
		if IsModifierKey(part) {
			native = true
		}
		if IsModifierKey(part) || part == "Cmd" || part == "Ctrl" || part == "Alt" || part == "Shift" {
			mods = append(mods, part)
		} else {
			if ordinary != "" || part == "" {
				return ""
			}
			ordinary = strings.ToUpper(part)
		}
	}
	if len(mods) == 0 || (!native && ordinary != "") {
		return ""
	}
	sort.Strings(mods)
	if ordinary != "" {
		mods = append(mods, ordinary)
	}
	return strings.Join(mods, "+")
}

func UsesNativeShortcut(key string) bool { return NativeShortcutIdentity(key) != "" }

// InsertOptions says how text is pasted into a target.
type InsertOptions struct {
	// Context cancels before any clipboard write or paste.
	Context context.Context
	// RestoreDelayMs is how long the text stays on the clipboard before
	// the earlier content is put back.
	RestoreDelayMs int
	RemoteInputs   []config.RemoteInput
	AutoSend       config.AutoSend
	// OnTarget observes the actual destination before any paste, fallback or error.
	// It must not retain or release the target.
	OnTarget func(*Target)
	// OnPasted starts observation before clipboard restoration. Local successful pastes only.
	// The callback may retain Target until observation finishes; the caller owns its release.
	OnPasted   func(*Target)
	BeforeSend func(*Target) bool
}

// InsertResult says what could be checked about an insertion.
type InsertResult struct {
	SendShortcut  string // Nonempty only when the key pair was dispatched; not a delivery receipt.
	SendSkipped   string // Stable diagnostic reason, if configured but skipped.
	ClipboardOnly bool
	// Remote reports a shortcut dispatched without remote readback.
	Remote bool
	Target *Target
	// FocusVerified means the current focus was verified as editable.
	FocusVerified bool
	// ClipboardRestored is false when the user copied something new in
	// the meantime, which was left alone.
	ClipboardRestored bool
	// Nonzero only after exact full-value and UTF-16 caret readback.
	PasteConfirmedMs   int64
	ClipboardRestoreMs int64
}

// Application identifies a selectable running desktop client.
type Application struct{ Name, BundleID string }
