//go:build darwin

package macos

import (
	"errors"
	"log"
	"time"

	"github.com/ebitengine/purego"
)

// Tail placement is essential: both dictation and translation's head listeners
// see the original Fn edges before this filter consumes a standalone gesture.
// Replayed Fn presses go after this tap, never through our listeners twice.
var fnFilter struct {
	tap, source, callback, pending uintptr
	copyEvent                      func(uintptr) uintptr
	postEvent                      func(uintptr, uintptr)
	invalidate                     func(uintptr)
	users                          map[*hotkeyListener]bool
	keys                           map[int64]bool
	forwarded                      bool
	generation                     uint64
	watchdog                       *time.Timer
}

func resetFnFilter() {
	if fnFilter.pending != 0 {
		cfRelease(fnFilter.pending)
		fnFilter.pending = 0
	}
	fnFilter.forwarded = false
	fnFilter.keys = map[int64]bool{}
}

func acquireFnFilter(h *hotkeyListener, location uint32) error {
	if !axIsProcessTrusted() {
		return errors.New("Accessibility permission is required to intercept Fn")
	}
	// Session-level interception cannot promise ownership before system shortcuts.
	if location != cgHIDEventTap {
		return errors.New("macOS did not allow HID-level Fn interception")
	}
	if fnFilter.tap != 0 {
		fnFilter.users[h] = true
		return nil
	}
	if fnFilter.callback == 0 {
		cg, err := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return err
		}
		purego.RegisterLibFunc(&fnFilter.copyEvent, cg, "CGEventCreateCopy")
		purego.RegisterLibFunc(&fnFilter.postEvent, cg, "CGEventTapPostEvent")
		cf, err := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return err
		}
		purego.RegisterLibFunc(&fnFilter.invalidate, cf, "CFMachPortInvalidate")
		fnFilter.callback = purego.NewCallback(fnFilterEvent)
	}
	mask := uint64(1)<<10 | uint64(1)<<11 | uint64(1)<<12 | uint64(1)<<14 | uint64(1)<<1 | uint64(1)<<3 | uint64(1)<<25 | uint64(1)<<22
	fnFilter.generation++
	tap := cgEventTapCreate(cgHIDEventTap, 1, 0, mask, fnFilter.callback, uintptr(fnFilter.generation))
	if tap == 0 {
		return errors.New("macOS did not allow active Fn interception; check Accessibility permission")
	}
	source := cfMachPortCreateRunLoopSource(0, tap, 0)
	if source == 0 {
		fnFilter.invalidate(tap)
		cfRelease(tap)
		return errors.New("could not schedule Fn interception")
	}
	fnFilter.tap, fnFilter.source = tap, source
	fnFilter.users = map[*hotkeyListener]bool{h: true}
	resetFnFilter()
	cfRunLoopAddSource(cfRunLoopGetMain(), source, cfRunLoopCommonModes)
	cgEventTapEnable(tap, true)
	watchFnFilter(fnFilter.generation)
	log.Print("trigger: active Fn interception enabled at HID tail")
	return nil
}

func releaseFnFilter(h *hotkeyListener) {
	delete(fnFilter.users, h)
	if len(fnFilter.users) != 0 || fnFilter.tap == 0 {
		return
	}
	fnFilter.generation++
	if fnFilter.watchdog != nil {
		fnFilter.watchdog.Stop()
		fnFilter.watchdog = nil
	}
	resetFnFilter()
	cgEventTapEnable(fnFilter.tap, false)
	cfRunLoopRemoveSource(cfRunLoopGetMain(), fnFilter.source, cfRunLoopCommonModes)
	fnFilter.invalidate(fnFilter.tap)
	cfRelease(fnFilter.source)
	cfRelease(fnFilter.tap)
	fnFilter.tap, fnFilter.source = 0, 0
}

func watchFnFilter(generation uint64) {
	fnFilter.watchdog = time.AfterFunc(500*time.Millisecond, func() {
		OnMain(func() {
			if fnFilter.tap == 0 || generation != fnFilter.generation {
				return
			}
			if !cgEventTapIsEnabled(fnFilter.tap) {
				resetFnFilter()
				for h := range fnFilter.users {
					h.reset()
				}
				if axIsProcessTrusted() && cgPreflightListenEvent() {
					cgEventTapEnable(fnFilter.tap, true)
				}
			}
			watchFnFilter(generation)
		})
	})
}

func fnFilterEvent(proxy, typ, event, user uintptr) uintptr {
	if user != uintptr(fnFilter.generation) {
		return event
	}
	if uint32(typ) == cgEventTapDisabledByTime || uint32(typ) == cgEventTapDisabledByUser {
		resetFnFilter()
		for h := range fnFilter.users {
			h.reset()
		}
		if fnFilter.tap != 0 && axIsProcessTrusted() && cgPreflightListenEvent() {
			cgEventTapEnable(fnFilter.tap, true)
		}
		return event
	}
	flags := cgEventGetFlags(event)
	code := cgEventGetIntegerValueField(event, cgKeyboardEventKeycode)
	if uint32(typ) == cgEventFlagsChanged && code == 63 {
		if flags&0x800000 != 0 {
			if fnFilter.forwarded {
				return event
			}
			if fnFilter.pending != 0 {
				return 0
			}
			if flags&(0x100000|0x40000|0x80000|0x20000) != 0 || len(fnFilter.keys) != 0 {
				fnFilter.forwarded = true
				cancelFnToggle()
				return event
			}
			fnFilter.pending = fnFilter.copyEvent(event)
			// Never consume an edge that could not be retained for a future chord.
			if fnFilter.pending == 0 {
				fnFilter.forwarded = true
				cancelFnToggle()
				return event
			}
			return 0
		}
		if fnFilter.pending != 0 {
			cfRelease(fnFilter.pending)
			fnFilter.pending = 0
			return 0
		}
		if fnFilter.forwarded {
			fnFilter.forwarded = false
			return event
		}
		return event // pre-registration/orphan release: preserve system state
	}
	if uint32(typ) == 10 {
		fnFilter.keys[code] = true
	}
	if uint32(typ) == 11 {
		delete(fnFilter.keys, code)
	}
	if fnFilter.pending != 0 {
		// The next keyboard/modifier/media/mouse action makes this a chord.
		cancelFnToggle()
		pending := fnFilter.pending
		fnFilter.pending, fnFilter.forwarded = 0, true
		fnFilter.postEvent(proxy, pending)
		cfRelease(pending)
	}
	return event
}

func cancelFnToggle() {
	for _, h := range []*hotkeyListener{&hotkey, &translationHotkey} {
		if h.key.code == 63 && h.down {
			h.combo = true
		}
	}
}
