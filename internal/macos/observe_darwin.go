//go:build darwin

package macos

import (
	"math"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// kCGEventSourceStateHIDSystemState: what the hardware did, not what apps posted
// to their own session.
const hidSystemState = 1

// kAXErrorNoValue: the attribute is supported and has no value now.
const axErrorNoValue = -25212

// pasteBinding reads the field and the selection a paste is about to replace.
// Called on the main thread. AX ranges use UTF-16, including surrogate pairs.
func pasteBinding(el uintptr) PasteBinding {
	var b PasteBinding
	b.Before, b.Known = pasteValue(el)
	if !b.Known {
		return b
	}
	r, ok := selectedPasteRange(el)
	u := utf16.Encode([]rune(b.Before))
	if !ok || r.location < 0 || r.length < 0 || r.location > int64(len(u)) || r.length > int64(len(u))-r.location {
		return b
	}
	end := r.location + r.length
	// Reject a range cutting a surrogate pair rather than changing its text.
	boundary := func(i int64) bool {
		return i == 0 || i == int64(len(u)) || !(u[i-1] >= 0xd800 && u[i-1] <= 0xdbff && u[i] >= 0xdc00 && u[i] <= 0xdfff)
	}
	if !boundary(r.location) || !boundary(end) {
		return b
	}
	b.Prefix, b.Suffix = string(utf16.Decode(u[:r.location])), string(utf16.Decode(u[end:]))
	b.Exact = true
	return b
}

// Binding is the field as it was just before the paste into it.
func (t *Target) Binding() PasteBinding {
	if t == nil {
		return PasteBinding{}
	}
	return t.paste
}

// Sample reads the target field with its focus, marked text and the latest
// physical input in one main-thread pass, so that they describe one moment.
func (t *Target) Sample() InputSample {
	var s InputSample
	if !t.HasElement() {
		return s
	}
	OnMain(func() {
		if t.element == 0 || !axIsProcessTrusted() {
			return
		}
		if frontmostPID() == t.pid {
			if f := focusedElement(t.pid); f != 0 {
				s.Focused = cfEqual(f, t.element)
				cfRelease(f)
			}
		}
		if !s.Focused {
			return
		}
		s.Value, s.Readable = pasteValue(t.element)
		s.Composing, s.CompositionKnown = markedText(t.element)
		s.Input = lastPress()
		s.Undo = undoHeld()
	})
	return s
}

// markedText says whether el shows an input method's uncommitted (marked)
// text. It reads AppKit's AXTextInputMarkedRange (an AXValue CFRange), then
// WebKit's AXTextInputMarkedTextMarkerRange, whose text is read through
// AXStringForTextMarkerRange. known is false when el exposes neither in a
// readable form; the caller must not treat that as "nothing is marked".
// Called on the main thread.
func markedText(el uintptr) (composing, known bool) {
	var v uintptr
	switch err := axCopyAttributeValue(el, uintptr(nsString("AXTextInputMarkedRange")), &v); {
	case err == axErrorNoValue:
		return false, true
	case err == 0 && v != 0:
		defer cfRelease(v)
		var r pasteRange
		if axValueGetType(v) == 4 && axValueGetValue(v, 4, unsafe.Pointer(&r)) {
			// An empty range (NSNotFound location included) marks nothing.
			return r.length > 0, true
		}
		return false, false
	}
	var m uintptr
	switch err := axCopyAttributeValue(el, uintptr(nsString("AXTextInputMarkedTextMarkerRange")), &m); {
	case err == axErrorNoValue:
		return false, true
	case err == 0 && m != 0:
		defer cfRelease(m)
		var text uintptr
		if axCopyParameterizedAttributeValue(el, uintptr(nsString("AXStringForTextMarkerRange")), m, &text) == 0 && text != 0 {
			defer cfRelease(text)
			if objc.Send[bool](objc.ID(text), sel("isKindOfClass:"), class("NSString")) {
				return objc.Send[int](objc.ID(text), sel("length")) > 0, true
			}
		}
		// A marked range whose text cannot be read counts as composing.
		return true, true
	}
	return false, false
}

// lastPress is when a key or mouse button last went down; zero when the
// system gives no usable answer, which observation treats as not manual.
// Mouse moves and modifier changes (the Fn trigger among them) do not count.
func lastPress() time.Time {
	since := math.Inf(1)
	for _, typ := range []uint32{1, 3, 10, 25} { // left, right mouse down; key down; other mouse down
		since = min(since, cgEventSourceSecondsSinceLastEventType(hidSystemState, typ))
	}
	if math.IsNaN(since) || since < 0 {
		return time.Time{}
	}
	if math.IsInf(since, 1) || since > 1e6 {
		since = 1e6 // long ago, yet known
	}
	return time.Now().Add(-time.Duration(since * float64(time.Second)))
}

// undoHeld says Command and the ANSI Z key are down now, Shift or not.
// Layouts that move Z, and Undo chosen from a menu, are not seen.
func undoHeld() bool {
	const keyZ, command = 6, 0x100000
	return cgEventSourceKeyState(hidSystemState, keyZ) && cgEventSourceFlagsState(hidSystemState)&command != 0
}
