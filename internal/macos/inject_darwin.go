//go:build darwin

package macos

import (
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// Target is a snapshot of an app and its focused element. Recording-start
// snapshots supply context; insertion captures its own current destination.
type Target struct {
	AppName  string
	BundleID string
	pid      int32
	app      objc.ID // NSRunningApplication, retained
	element  uintptr // AXUIElement, retained; 0 when the app exposed none
	paste    PasteBinding
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

type pasteRange struct{ location, length int64 }

// Called on the main thread. AX ranges use UTF-16, including surrogate pairs.
func selectedPasteRange(el uintptr) (pasteRange, bool) {
	v := axAttribute(el, "AXSelectedTextRange")
	if v == 0 {
		return pasteRange{}, false
	}
	defer cfRelease(v)
	var r pasteRange
	ok := axValueGetType(v) == 4 && axValueGetValue(v, 4, unsafe.Pointer(&r))
	return r, ok
}

func pasteValue(el uintptr) (string, bool) {
	v := axAttribute(el, "AXValue")
	if v == 0 {
		return "", false
	}
	defer cfRelease(v)
	if !objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSString")) {
		return "", false
	}
	s := goString(objc.ID(v))
	return s, len(s) <= 65536
}

// expectedPaste is the field a paste into the bound selection must leave,
// and the UTF-16 caret after it. The binding comes from pasteBinding.
func expectedPaste(b PasteBinding, text string) (string, int64, bool) {
	if !b.Exact {
		return "", 0, false
	}
	expected := b.Prefix + text + b.Suffix
	caret := int64(len(utf16.Encode([]rune(b.Prefix + text))))
	return expected, caret, expected != b.Before
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

// captureInputTarget gives a newly activated local editor time to publish its
// input focus. A menu can already advertise Paste while the editor is still
// mounting; dispatching at that point can silently lose the transcript.
// Ready fields and configured remote clients have no settling delay. Never
// activate an application or retry a keyboard event here.
func captureInputTarget(o InsertOptions) *Target {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		t := CaptureTarget()
		ready := false
		OnMain(func() {
			ready = axIsProcessTrusted() && frontmostPID() == t.pid && editableInput(t.element)
		})
		for _, rule := range o.RemoteInputs {
			if rule.BundleID != "" && rule.BundleID == t.BundleID {
				ready = true
				break
			}
		}
		if ready || time.Now().After(deadline) || (o.Context != nil && o.Context.Err() != nil) {
			return t
		}
		t.Release()
		time.Sleep(25 * time.Millisecond)
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
	current := captureInputTarget(o)
	res.Target = current
	if o.OnTarget != nil {
		o.OnTarget(current)
	}
	if remote, handled, err := insertRemote(text, o, current); handled {
		if err != nil {
			current.Release()
			remote.Target = nil
		}
		return remote, err
	}
	var sendWindow uintptr
	sendKey := o.AutoSend.Shortcut(current.BundleID)
	// A configured remote client never inherits the local send rule, including
	// windows outside a profile or a profile whose paste shortcut is disabled.
	for _, r := range o.RemoteInputs {
		if r.BundleID == current.BundleID {
			sendKey = ""
			break
		}
	}
	if sendKey != "" {
		OnMain(func() { sendWindow = focusedWindow(current.pid) })
		defer func() {
			if sendWindow != 0 {
				OnMain(func() { cfRelease(sendWindow) })
			}
		}()
	}
	var saved []clipboardItem
	var change int
	var cancelled error
	var expected string
	var caret int64
	var verifyPaste bool
	OnMain(func() {
		if o.Context != nil && o.Context.Err() != nil {
			cancelled = o.Context.Err()
			return
		}
		cur := focusedElement(frontmostPID())
		if cur != 0 {
			defer cfRelease(cur)
		}
		sameFocus := frontmostPID() == current.pid &&
			((cur == 0 && current.element == 0) || (cur != 0 && current.element != 0 && cfEqual(cur, current.element)))
		res.FocusVerified = axIsProcessTrusted() && sameFocus && editableInput(cur)
		// Missing AX editability is not proof that keyboard input is unavailable.
		// A native enabled Paste command is independent evidence of support.
		canPaste := res.FocusVerified || (axIsProcessTrusted() && sameFocus && pasteMenuEnabled(current.pid))
		canPaste = canPaste && frontmostPID() == current.pid
		// Menu traversal can cross another focus transition. An enabled menu
		// must not authorize a paste into a different field than the one bound
		// to privacy, history, edit observation and the send rule.
		if canPaste {
			latest := focusedElement(current.pid)
			canPaste = (latest == 0 && current.element == 0) ||
				(latest != 0 && current.element != 0 && cfEqual(latest, current.element))
			if latest != 0 {
				cfRelease(latest)
			}
			canPaste = canPaste && frontmostPID() == current.pid
		}
		if canPaste {
			saved = saveClipboard()
			// The observer binds to this exact field and selection, read before
			// the paste, rather than to wherever the text appears afterwards.
			if current.element != 0 {
				current.paste = pasteBinding(current.element)
			}
			if res.FocusVerified && sendKey == "" && o.RestoreDelayMs <= 600 {
				expected, caret, verifyPaste = expectedPaste(current.paste, text)
			}
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
	if o.OnPasted != nil && sendKey == "" {
		o.OnPasted(current)
	}
	settleStart := time.Now()
	settleUntil := settleStart.Add(time.Duration(max(o.RestoreDelayMs, 200)) * time.Millisecond)
	for time.Now().Before(settleUntil) {
		// Early restoration requires proof that this exact paste was consumed,
		// not merely a substring match or a dispatched keyboard event.
		if verifyPaste && time.Since(settleStart) >= 80*time.Millisecond {
			confirmed := false
			OnMain(func() {
				if (o.Context != nil && o.Context.Err() != nil) || frontmostPID() != current.pid || objc.Send[int](pasteboard(), sel("changeCount")) != change {
					verifyPaste = false
					return
				}
				f := focusedElement(current.pid)
				if f == 0 {
					verifyPaste = false
					return
				}
				defer cfRelease(f)
				if !cfEqual(f, current.element) {
					verifyPaste = false
					return
				}
				value, ok := pasteValue(f)
				rangeNow, rangeOK := selectedPasteRange(f)
				confirmed = ok && rangeOK && value == expected && rangeNow.location == caret && rangeNow.length == 0
			})
			if confirmed {
				res.PasteConfirmedMs = time.Since(settleStart).Milliseconds()
				break
			}
		}
		if sendKey != "" && res.SendSkipped == "" {
			if o.Context != nil && o.Context.Err() != nil {
				res.SendSkipped = "cancelled"
			} else {
				OnMain(func() {
					if frontmostPID() != current.pid || sendWindow == 0 || objc.Send[int](pasteboard(), sel("changeCount")) != change {
						res.SendSkipped = "target-or-clipboard-changed"
						return
					}
					w := focusedWindow(current.pid)
					if w == 0 {
						res.SendSkipped = "target-changed"
						return
					}
					defer cfRelease(w)
					f := focusedElement(current.pid)
					if f != 0 {
						defer cfRelease(f)
					}
					if !cfEqual(w, sendWindow) || f == 0 || current.element == 0 || !cfEqual(f, current.element) {
						res.SendSkipped = "target-changed"
					}
				})
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	OnMain(func() {
		if objc.Send[int](pasteboard(), sel("changeCount")) == change {
			restoreClipboard(saved)
			res.ClipboardRestored = true
		}
		releaseClipboard(saved)
	})
	res.ClipboardRestoreMs = time.Since(settleStart).Milliseconds()
	if sendKey != "" && res.SendSkipped == "" {
		allowed := true
		if o.BeforeSend != nil {
			allowed = o.BeforeSend(current)
		} else {
			end := time.Now().Add(time.Duration(o.AutoSend.Delay()) * time.Millisecond)
			for time.Now().Before(end) {
				if (o.Context != nil && o.Context.Err() != nil) || !current.StillFocused() {
					allowed = false
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		if !allowed {
			res.SendSkipped = "cancelled"
		} else {
			res.SendSkipped = sendAfterPaste(text, sendKey, o, current, sendWindow)
		}
		if res.SendSkipped == "" {
			res.SendShortcut = sendKey
		}
	}
	return res, nil
}

// sendAfterPaste never retries a dispatched key. Clipboard ownership is checked
// during the paste settling interval by the caller; focus/window are rechecked
// immediately before posting. A readable value must contain the final text.
func sendAfterPaste(text, key string, o InsertOptions, t *Target, window uintptr) string {
	if strings.TrimSpace(text) == "" {
		return "empty"
	}
	if o.Context != nil && o.Context.Err() != nil {
		return "cancelled"
	}
	// Clipboard restoration has already completed. Wait for the target's event
	// loop, and require readback where the editor exposes it.
	deadline := time.Now().Add(900 * time.Millisecond)
	for {
		valid := false
		OnMain(func() {
			if !axIsProcessTrusted() || frontmostPID() != t.pid || window == 0 {
				return
			}
			w := focusedWindow(t.pid)
			if w == 0 {
				return
			}
			defer cfRelease(w)
			if !cfEqual(w, window) {
				return
			}
			f := focusedElement(t.pid)
			if f != 0 {
				defer cfRelease(f)
			}
			if t.element == 0 || f == 0 || !cfEqual(t.element, f) {
				return
			}
			valid = true
		})
		if !valid {
			return "target-changed"
		}
		if o.Context != nil && o.Context.Err() != nil {
			return "cancelled"
		}
		value, readable := t.ReadValue()
		if !readable || strings.Contains(value, text) {
			break
		}
		if time.Now().After(deadline) {
			return "paste-unconfirmed"
		}
		time.Sleep(25 * time.Millisecond)
	}
	reason := "target-changed"
	OnMain(func() {
		if o.Context != nil && o.Context.Err() != nil {
			reason = "cancelled"
			return
		}
		if frontmostPID() != t.pid || !axIsProcessTrusted() {
			return
		}
		w := focusedWindow(t.pid)
		if w == 0 {
			return
		}
		defer cfRelease(w)
		f := focusedElement(t.pid)
		if f == 0 {
			return
		}
		defer cfRelease(f)
		if !cfEqual(w, window) || !cfEqual(f, t.element) {
			return
		}
		flags := uint64(0)
		if key == "cmd-enter" {
			flags = 0x100000
		}
		if key == "ctrl-enter" {
			flags = 0x40000
		}
		down := cgEventCreateKeyboardEvent(0, 36, true)
		up := cgEventCreateKeyboardEvent(0, 36, false)
		if down == 0 || up == 0 {
			if down != 0 {
				cfRelease(down)
			}
			if up != 0 {
				cfRelease(up)
			}
			reason = "event-unavailable"
			return
		}
		defer cfRelease(down)
		defer cfRelease(up)
		cgEventSetFlags(down, flags)
		cgEventSetFlags(up, flags)
		cgEventPost(0, down)
		cgEventPost(0, up)
		reason = ""
	})
	return reason
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
