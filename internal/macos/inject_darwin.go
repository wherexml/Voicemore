//go:build darwin

package macos

import (
	"os/exec"
	"time"

	"github.com/ebitengine/purego/objc"
)

// Target is the app and the input element that had the keyboard as a
// recording started, where its text goes.
type Target struct {
	AppName  string
	BundleID string
	pid      int32
	app      objc.ID // NSRunningApplication, retained
	element  uintptr // AXUIElement, retained; 0 when the app exposed none
}

// focusedElement returns the element with the keyboard focus in an app,
// which the caller releases, or 0.
func focusedElement(pid int32) uintptr {
	app := axCreateApplication(pid)
	if app == 0 {
		return 0
	}
	defer cfRelease(app)
	var el uintptr
	if axCopyAttributeValue(app, uintptr(nsString("AXFocusedUIElement")), &el) != 0 {
		return 0
	}
	return el
}

func frontmostPID() int32 {
	app := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("frontmostApplication"))
	if app == 0 {
		return 0
	}
	return objc.Send[int32](app, sel("processIdentifier"))
}

// CaptureTarget remembers the frontmost app and its focused element. The
// element is only known with the Accessibility permission, and in apps
// that expose one.
func CaptureTarget() *Target {
	t := &Target{}
	if load() != nil {
		return t
	}
	OnMain(func() {
		app := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("frontmostApplication"))
		if app == 0 {
			return
		}
		t.app = app.Send(sel("retain"))
		t.pid = objc.Send[int32](app, sel("processIdentifier"))
		t.AppName = goString(app.Send(sel("localizedName")))
		t.BundleID = goString(app.Send(sel("bundleIdentifier")))
		if axIsProcessTrusted() {
			t.element = focusedElement(t.pid)
		}
	})
	return t
}

// HasElement says the input element itself is known, not only its app.
func (t *Target) HasElement() bool { return t != nil && t.element != 0 }

// Release frees what the target holds. The target is not used after it.
func (t *Target) Release() {
	if t == nil {
		return
	}
	OnMain(func() {
		if t.element != 0 {
			cfRelease(t.element)
			t.element = 0
		}
		if t.app != 0 {
			t.app.Send(sel("release"))
			t.app = 0
		}
	})
}

// ReadValue reads the text of the target element, where the app allows it.
func (t *Target) ReadValue() (string, bool) {
	if !t.HasElement() {
		return "", false
	}
	var s string
	ok := false
	OnMain(func() {
		var v uintptr
		if t.element == 0 || axCopyAttributeValue(t.element, uintptr(nsString("AXValue")), &v) != 0 || v == 0 {
			return
		}
		defer cfRelease(v)
		if objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSString")) {
			s, ok = goString(objc.ID(v)), true
		}
	})
	return s, ok
}

// editableInput checks the current focus without moving it. Read-only text,
// buttons and windows must leave the transcript on the clipboard.
func editableInput(el uintptr) bool {
	if el == 0 {
		return false
	}
	var enabled uintptr
	if axCopyAttributeValue(el, uintptr(nsString("AXEnabled")), &enabled) == 0 && enabled != 0 {
		ok := objc.Send[bool](objc.ID(enabled), sel("boolValue"))
		cfRelease(enabled)
		if !ok {
			return false
		}
	}
	for _, attr := range []string{"AXSelectedText", "AXValue"} {
		var writable bool
		if axIsAttributeSettable(el, uintptr(nsString(attr)), &writable) == 0 && writable {
			return true
		}
	}
	return false
}

// axAttribute returns a retained attribute, or zero. Caller releases it.
func axAttribute(el uintptr, name string) uintptr {
	var value uintptr
	if el == 0 || axCopyAttributeValue(el, uintptr(nsString(name)), &value) != 0 {
		return 0
	}
	return value
}

// pasteMenuEnabled supports custom editors (including Qt/WeChat) which do
// not expose editable AX elements. Inspect the standard Paste command only;
// do not activate the app, open a menu, or invoke any accessibility action.
func pasteMenuEnabled(pid int32) bool {
	app := axCreateApplication(pid)
	if app == 0 {
		return false
	}
	defer cfRelease(app)
	menu := axAttribute(app, "AXMenuBar")
	if menu == 0 {
		return false
	}
	defer cfRelease(menu)
	budget := 250
	var walk func(uintptr, int) bool
	walk = func(el uintptr, depth int) bool {
		budget--
		if budget < 0 || depth > 5 {
			return false
		}
		identifier := axAttribute(el, "AXIdentifier")
		paste := false
		if identifier != 0 {
			paste = goString(objc.ID(identifier)) == "paste:"
			cfRelease(identifier)
		}
		if !paste {
			key := axAttribute(el, "AXMenuItemCmdChar")
			if key != 0 {
				ch := goString(objc.ID(key))
				cfRelease(key)
				if ch == "v" || ch == "V" {
					mods := axAttribute(el, "AXMenuItemCmdModifiers")
					if mods != 0 {
						paste = objc.Send[int](objc.ID(mods), sel("intValue")) == 0
						cfRelease(mods)
					}
				}
			}
		}
		if paste {
			enabled := axAttribute(el, "AXEnabled")
			if enabled == 0 {
				return false
			}
			defer cfRelease(enabled)
			return objc.Send[bool](objc.ID(enabled), sel("boolValue"))
		}
		children := axAttribute(el, "AXChildren")
		if children == 0 {
			return false
		}
		defer cfRelease(children)
		list := objc.ID(children)
		for i := 0; i < objc.Send[int](list, sel("count")) && budget > 0; i++ {
			if walk(uintptr(list.Send(sel("objectAtIndex:"), i)), depth+1) {
				return true
			}
		}
		return false
	}
	return walk(menu, 0)
}

// PasteAvailable checks the currently frontmost application's native Paste
// command without modifying focus, the clipboard, or any text.
func (t *Target) PasteAvailable() bool {
	if t == nil {
		return false
	}
	allowed := false
	OnMain(func() {
		allowed = axIsProcessTrusted() && frontmostPID() == t.pid && pasteMenuEnabled(t.pid) && frontmostPID() == t.pid
	})
	return allowed
}

// clipboardItem is one saved item of the pasteboard: retained types and data.
type clipboardItem struct{ types, data []objc.ID }

func pasteboard() objc.ID { return class("NSPasteboard").Send(sel("generalPasteboard")) }

func saveClipboard() []clipboardItem {
	var saved []clipboardItem
	items := pasteboard().Send(sel("pasteboardItems"))
	if items == 0 {
		return nil
	}
	for i := range objc.Send[int](items, sel("count")) {
		item := items.Send(sel("objectAtIndex:"), i)
		types := item.Send(sel("types"))
		var it clipboardItem
		for j := range objc.Send[int](types, sel("count")) {
			typ := types.Send(sel("objectAtIndex:"), j)
			if data := item.Send(sel("dataForType:"), typ); data != 0 {
				it.types = append(it.types, typ.Send(sel("retain")))
				it.data = append(it.data, data.Send(sel("retain")))
			}
		}
		saved = append(saved, it)
	}
	return saved
}

func restoreClipboard(saved []clipboardItem) {
	pb := pasteboard()
	pb.Send(sel("clearContents"))
	if len(saved) == 0 {
		return
	}
	objects := class("NSMutableArray").Send(sel("array"))
	for _, it := range saved {
		item := class("NSPasteboardItem").Send(sel("alloc")).Send(sel("init")).Send(sel("autorelease"))
		for k := range it.types {
			item.Send(sel("setData:forType:"), it.data[k], it.types[k])
		}
		objects.Send(sel("addObject:"), item)
	}
	pb.Send(sel("writeObjects:"), objects)
}

func releaseClipboard(saved []clipboardItem) {
	for _, it := range saved {
		for k := range it.types {
			it.types[k].Send(sel("release"))
			it.data[k].Send(sel("release"))
		}
	}
}

// Insert uses only the input focused now, never activating or refocusing an
// app. If none is editable, the transcript stays on the clipboard for Cmd+V.
// The caller owns res.Target and must release it when finished observing edits.
func Insert(text string, o InsertOptions) (InsertResult, error) {
	var res InsertResult
	if o.Context != nil && o.Context.Err() != nil {
		return res, o.Context.Err()
	}
	if err := load(); err != nil {
		return res, err
	}
	current := CaptureTarget()
	res.Target = current
	if remote, handled, err := insertRemote(text, o, current); handled {
		if err != nil {
			current.Release()
			remote.Target = nil
		}
		return remote, err
	}
	var saved []clipboardItem
	var change int
	var cancelled error
	OnMain(func() {
		if o.Context != nil && o.Context.Err() != nil {
			cancelled = o.Context.Err()
			return
		}
		cur := focusedElement(frontmostPID())
		if cur != 0 {
			defer cfRelease(cur)
		}
		res.FocusVerified = axIsProcessTrusted() && current.element != 0 &&
			frontmostPID() == current.pid && cur != 0 && cfEqual(cur, current.element) && editableInput(cur)
		// Missing AX editability is not proof that keyboard input is unavailable.
		// A native enabled Paste command is independent evidence of support.
		canPaste := res.FocusVerified || (axIsProcessTrusted() && frontmostPID() == current.pid && pasteMenuEnabled(current.pid))
		canPaste = canPaste && frontmostPID() == current.pid
		if canPaste {
			saved = saveClipboard()
		}
		pb := pasteboard()
		pb.Send(sel("clearContents"))
		pb.Send(sel("setString:forType:"), nsString(text), nsString("public.utf8-plain-text"))
		if !canPaste {
			res.ClipboardOnly = true
			return
		}
		pb.Send(sel("setString:forType:"), nsString(""), nsString("org.nspasteboard.TransientType"))
		change = objc.Send[int](pb, sel("changeCount"))
		const keyV, cmd, hidTap = 9, 0x100000, 0
		for _, down := range []bool{true, false} {
			ev := cgEventCreateKeyboardEvent(0, keyV, down)
			cgEventSetFlags(ev, cmd)
			cgEventPost(hidTap, ev)
			cfRelease(ev)
		}
	})
	if cancelled != nil {
		current.Release()
		res.Target = nil
		return res, cancelled
	}
	if res.ClipboardOnly {
		return res, nil
	}
	time.Sleep(time.Duration(max(o.RestoreDelayMs, 200)) * time.Millisecond)
	OnMain(func() {
		if objc.Send[int](pasteboard(), sel("changeCount")) == change {
			restoreClipboard(saved)
			res.ClipboardRestored = true
		}
		releaseClipboard(saved)
	})
	return res, nil
}

// AccessibilityTrusted reports the Accessibility permission, which pasting
// and reading the focused element need, and shows the system's prompt
// when prompt is set and it is missing.
func AccessibilityTrusted(prompt bool) bool {
	if load() != nil {
		return false
	}
	if !prompt {
		return axIsProcessTrusted()
	}
	trusted := false
	OnMain(func() {
		yes := class("NSNumber").Send(sel("numberWithBool:"), true)
		opts := class("NSDictionary").Send(sel("dictionaryWithObject:forKey:"), yes, nsString("AXTrustedCheckOptionPrompt"))
		trusted = axIsProcessTrustedWithOptions(uintptr(opts))
	})
	return trusted
}

// OpenPrivacySettings opens a pane of System Settings → Privacy & Security:
// "Privacy_Microphone", "Privacy_Accessibility" or "Privacy_ListenEvent".
func OpenPrivacySettings(pane string) {
	exec.Command("/usr/bin/open", "x-apple.systempreferences:com.apple.preference.security?"+pane).Start()
}
