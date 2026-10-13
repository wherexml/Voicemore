//go:build darwin

package macos

import (
	"errors"
	"os/exec"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

// The Fn key is no Carbon hot key, so MyGo's global shortcuts cannot
// register it: a listen-only event tap watches the modifier keys instead.
// It observes key events and never changes or swallows them.

const (
	cgSessionEventTap        = 1
	cgHeadInsertEventTap     = 0
	cgEventTapListenOnly     = 1
	cgEventKeyDown           = 10
	cgEventFlagsChanged      = 12
	cgEventTapDisabledByTime = 0xFFFFFFFE
	cgEventTapDisabledByUser = 0xFFFFFFFF
	cgKeyboardEventKeycode   = 9
)

type modifierKey struct {
	code int64
	flag uint64
}

var modifierKeys = map[string]modifierKey{
	KeyFn:           {63, 0x800000},
	KeyRightOption:  {61, 0x080000},
	KeyRightCommand: {54, 0x100000},
	KeyRightControl: {62, 0x040000},
}

// hotkey is the one tap of the process. Its fields are read and written on
// the main thread, where the tap's callback runs.
var hotkey struct {
	sync.Mutex
	callback uintptr
	tap      uintptr
	source   uintptr
	key      modifierKey
	down     bool
	combo    bool
	onCancel func()
	onDown   func()
	onUp     func(combo bool)
}

func hotkeyEvent(proxy, typ, event, user uintptr) uintptr {
	h := &hotkey
	switch uint32(typ) {
	case cgEventTapDisabledByTime, cgEventTapDisabledByUser:
		// The system disables a tap whose callback was slow.
		if h.tap != 0 {
			cgEventTapEnable(h.tap, true)
		}
	case cgEventKeyDown:
		if cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) == 53 {
			if fn := h.onCancel; fn != nil {
				go fn()
			}
		}
		// Another key while the trigger is held: a combination such as
		// Fn+Delete, not a press of the trigger alone.
		if h.down {
			h.combo = true
		}
	case cgEventFlagsChanged:
		if cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) != h.key.code {
			if h.down {
				h.combo = true
			}
			break
		}
		down := cgEventGetFlags(event)&h.key.flag != 0
		if down == h.down {
			break // a repeat: no second press
		}
		h.down = down
		// The callbacks leave the main thread at once: the tap must
		// return quickly.
		if down {
			h.combo = false
			if fn := h.onDown; fn != nil {
				go fn()
			}
		} else if fn := h.onUp; fn != nil {
			combo := h.combo
			go fn(combo)
		}
	}
	return event
}

// StartHotkey watches a modifier key in every app. onDown is called as it
// goes down and onUp as it comes up, with combo true when another key was
// pressed in between. It needs the Input Monitoring permission, which it
// asks for once, and returns the function that stops it.
func StartHotkey(key string, onDown func(), onUp func(combo bool), onCancel func()) (func(), error) {
	if err := load(); err != nil {
		return nil, err
	}
	k, ok := modifierKeys[key]
	if key == "" {
		k, ok = modifierKey{code: -1}, true
	}
	if !ok {
		return nil, errors.New("not a modifier key: " + key)
	}
	var err error
	OnMain(func() {
		h := &hotkey
		if h.tap != 0 {
			err = errors.New("the trigger key is already watched")
			return
		}
		if h.callback == 0 {
			h.callback = purego.NewCallback(hotkeyEvent)
		}
		mask := uint64(1)<<cgEventKeyDown | uint64(1)<<cgEventFlagsChanged
		tap := cgEventTapCreate(cgSessionEventTap, cgHeadInsertEventTap, cgEventTapListenOnly, mask, h.callback, 0)
		if tap == 0 {
			err = errors.New("macOS did not allow watching the keyboard: grant Input Monitoring to Voicemore in System Settings → Privacy & Security, then start it again")
			return
		}
		h.key, h.down, h.combo, h.onDown, h.onUp = k, false, false, onDown, onUp
		h.onCancel = onCancel
		h.tap = tap
		h.source = cfMachPortCreateRunLoopSource(0, tap, 0)
		cfRunLoopAddSource(cfRunLoopGetMain(), h.source, cfRunLoopCommonModes)
		cgEventTapEnable(tap, true)
	})
	if err != nil {
		return nil, err
	}
	return func() {
		OnMain(func() {
			h := &hotkey
			if h.tap == 0 {
				return
			}
			cgEventTapEnable(h.tap, false)
			cfRunLoopRemoveSource(cfRunLoopGetMain(), h.source, cfRunLoopCommonModes)
			cfRelease(h.source)
			cfRelease(h.tap)
			h.tap, h.source, h.onDown, h.onUp = 0, 0, nil, nil
			h.onCancel = nil
		})
	}, nil
}

// InputMonitoringGranted reports the permission the event tap needs, and
// asks for it when prompt is set.
func InputMonitoringGranted(prompt bool) bool {
	if load() != nil {
		return false
	}
	if cgPreflightListenEvent() {
		return true
	}
	if prompt {
		return cgRequestListenEvent()
	}
	return false
}

// FnKeyConflict describes what the system does on a press of the Fn
// (Globe) key, when it does something, or returns "". It only reads the
// setting: the user changes it, in System Settings → Keyboard.
func FnKeyConflict() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "com.apple.HIToolbox", "AppleFnUsageType").Output()
	if err != nil {
		return "" // the key is unset on some systems: unknown, not a conflict
	}
	switch strings.TrimSpace(string(out)) {
	case "1":
		return "change the input source"
	case "2":
		return "show Emoji & Symbols"
	case "3":
		return "start Dictation"
	}
	return ""
}
