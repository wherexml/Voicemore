//go:build darwin

package macos

import (
	"github.com/ebitengine/purego/objc"
	"strings"
	"time"
)

func RunningApplications() []Application {
	var result []Application
	if load() != nil {
		return nil
	}
	OnMain(func() {
		apps := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("runningApplications"))
		for i := 0; i < objc.Send[int](apps, sel("count")); i++ {
			app := apps.Send(sel("objectAtIndex:"), i)
			id := goString(app.Send(sel("bundleIdentifier")))
			if id != "" && id != "dev.steve.voice-input" && objc.Send[int](app, sel("activationPolicy")) == 0 {
				result = append(result, Application{goString(app.Send(sel("localizedName"))), id})
			}
		}
	})
	return result
}

func focusedWindow(pid int32) uintptr {
	app := axCreateApplication(pid)
	if app == 0 {
		return 0
	}
	defer cfRelease(app)
	return axAttribute(app, "AXFocusedWindow")
}

// insertRemote returns handled only for an explicit profile matching this
// client/window. The clipboard remains available for asynchronous sync.
func insertRemote(text string, o InsertOptions, current *Target) (res InsertResult, handled bool, err error) {
	res.Target = current
	var window uintptr
	var change, delay int
	var flags uint64
	OnMain(func() {
		window = focusedWindow(current.pid)
		title := ""
		if v := axAttribute(window, "AXTitle"); v != 0 {
			title = goString(objc.ID(v))
			cfRelease(v)
		}
		// Prefer the most specific matching title over a client-wide fallback.
		specificity := -1
		for _, rule := range o.RemoteInputs {
			if rule.BundleID == "" || rule.BundleID != current.BundleID || !strings.Contains(title, rule.WindowTitle) {
				continue
			}
			if len(rule.WindowTitle) < specificity {
				continue
			}
			specificity = len(rule.WindowTitle)
			handled = rule.Shortcut == "cmd-v" || rule.Shortcut == "ctrl-v"
			flags = 0x100000
			if rule.Shortcut == "ctrl-v" {
				flags = 0x40000
			}
			delay = rule.SyncDelayMs
		}
		if !handled {
			return
		}
		res.ClipboardOnly = true
		if o.Context != nil && o.Context.Err() != nil {
			err = o.Context.Err()
			return
		}
		pb := pasteboard()
		pb.Send(sel("clearContents"))
		pb.Send(sel("setString:forType:"), nsString(text), nsString("public.utf8-plain-text"))
		change = objc.Send[int](pb, sel("changeCount"))
	})
	defer func() {
		if window != 0 {
			OnMain(func() { cfRelease(window) })
		}
	}()
	if !handled || err != nil {
		return
	}
	valid := func() bool {
		if !axIsProcessTrusted() || window == 0 || frontmostPID() != current.pid || objc.Send[int](pasteboard(), sel("changeCount")) != change {
			return false
		}
		now := focusedWindow(current.pid)
		if now == 0 {
			return false
		}
		defer cfRelease(now)
		if !cfEqual(now, window) {
			return false
		}
		if current.element != 0 {
			focus := focusedElement(current.pid)
			if focus == 0 {
				return false
			}
			defer cfRelease(focus)
			if !cfEqual(focus, current.element) {
				return false
			}
		}
		return true
	}
	deadline := time.Now().Add(time.Duration(min(max(delay, 200), 10000)) * time.Millisecond)
	for {
		if o.Context != nil && o.Context.Err() != nil {
			err = o.Context.Err()
			return
		}
		ok := false
		OnMain(func() { ok = valid() })
		if !ok {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	OnMain(func() {
		if o.Context != nil && o.Context.Err() != nil {
			err = o.Context.Err()
			return
		}
		if !valid() {
			return
		}
		// Remote clients may need explicit modifier transitions, not just V flags.
		modifier := uint16(55)
		if flags == 0x40000 {
			modifier = 59
		}
		for _, key := range []struct {
			code  uint16
			down  bool
			flags uint64
		}{
			{modifier, true, flags}, {9, true, flags}, {9, false, flags}, {modifier, false, 0},
		} {
			ev := cgEventCreateKeyboardEvent(0, key.code, key.down)
			if ev == 0 {
				return
			}
			cgEventSetFlags(ev, key.flags)
			cgEventPost(0, ev)
			cfRelease(ev)
		}
		res.ClipboardOnly, res.Remote = false, true
	})
	return
}
