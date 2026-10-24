//go:build darwin

package macos

import (
	"github.com/ebitengine/purego/objc"
	"strings"
)

// Retain gives the preview its own lifetime beyond Insert's return.
func (t *Target) Retain() *Target {
	out := &Target{AppName: t.AppName, BundleID: t.BundleID, pid: t.pid, paste: t.paste}
	OnMain(func() {
		if t.element != 0 {
			out.element = cfRetain(t.element)
		}
		if t.app != 0 {
			out.app = t.app.Send(sel("retain"))
		}
	})
	return out
}
func (t *Target) StillFocused() bool {
	ok := false
	OnMain(func() {
		if frontmostPID() != t.pid {
			return
		}
		f := focusedElement(t.pid)
		if f != 0 {
			defer cfRelease(f)
		}
		ok = f != 0 && t.element != 0 && cfEqual(f, t.element)
	})
	return ok
}

// StillSelected checks the captured field without requiring its app to be
// frontmost: a nonactivating editor may take keyboard focus while editing it.
func (t *Target) StillSelected() bool {
	ok := false
	OnMain(func() {
		f := focusedElement(t.pid)
		if f != 0 {
			defer cfRelease(f)
		}
		ok = f != 0 && t.element != 0 && cfEqual(f, t.element)
	})
	return ok
}

// ReplaceValue is compare-and-set against the captured field, without activating
// another app or synthesizing keys. Unsupported editors keep the local draft.
func (t *Target) ReplaceValue(expected, value string) bool {
	ok := false
	OnMain(func() {
		if t.element == 0 {
			return
		}
		v := axAttribute(t.element, "AXValue")
		if v == 0 {
			return
		}
		defer cfRelease(v)
		if !objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSString")) || goString(objc.ID(v)) != expected {
			return
		}
		var writable bool
		if axIsAttributeSettable(t.element, uintptr(nsString("AXValue")), &writable) != 0 || !writable {
			return
		}
		if axSetAttributeValue(t.element, uintptr(nsString("AXValue")), uintptr(nsString(value))) != 0 {
			return
		}
		actual := axAttribute(t.element, "AXValue")
		if actual == 0 {
			return
		}
		defer cfRelease(actual)
		ok = goString(objc.ID(actual)) == value
	})
	return ok
}
func (t *Target) EditableSnapshot(inserted string) (string, int, bool) {
	value, ok := t.ReadValue()
	if !ok || strings.Count(value, inserted) != 1 {
		return "", 0, false
	}
	return value, strings.Index(value, inserted), true
}
