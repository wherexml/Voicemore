//go:build darwin

package macos

import (
	"errors"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"voiceinput/internal/config"

	"github.com/ebitengine/purego"
)

// The Fn key is no Carbon hot key, so MyGo's global shortcuts cannot
// register it: a listen-only event tap watches the modifier keys instead.
// It observes key events and never changes or swallows them.

const (
	cgHIDEventTap            = 0
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
	"LeftOption":    {58, 0x080000}, "LeftCommand": {55, 0x100000},
	"LeftControl": {59, 0x040000}, "LeftShift": {56, 0x020000}, "RightShift": {60, 0x020000},
}

// hotkey is the one tap of the process. Its fields are read and written on
// the main thread, where the tap's callback runs.
type hotkeyListener struct {
	sync.Mutex
	callback          uintptr
	tap               uintptr
	source            uintptr
	watchdog          *time.Timer
	generation        uint64
	waitingPermission bool
	key               modifierKey
	chordModifiers    []modifierKey
	chordKey          int64
	fnDown            bool
	down              bool
	combo             bool
	onCancel          func()
	onDown            func()
	onUp              func(combo bool)
}

var hotkey, translationHotkey hotkeyListener

// resetHotkeyGesture also finishes an interrupted hold. combo=true keeps
// recovery from toggling a recording or starting one on an orphan release.
func resetHotkeyGesture() { hotkey.reset() }
func (h *hotkeyListener) reset() {
	wasDown := h.down
	h.down, h.combo, h.fnDown = false, false, false
	if wasDown && h.onUp != nil {
		fn := h.onUp
		go fn(true)
	}
}

// A disabled tap does not always deliver its interruption callback. Check
// independently so one missed notification cannot leave Fn dead until restart.
// Every access remains on the tap's main run loop; generations fence rebinds.
func watchHotkey(generation uint64) { hotkey.watch(generation) }
func (h *hotkeyListener) watch(generation uint64) {
	h.watchdog = time.AfterFunc(500*time.Millisecond, func() {
		OnMain(func() {
			if h.tap == 0 || h.generation != generation {
				return
			}
			if !cgEventTapIsEnabled(h.tap) {
				h.reset()
				if !cgPreflightListenEvent() {
					if !h.waitingPermission {
						log.Print("trigger: event tap paused; Input Monitoring unavailable for current process")
					}
					h.waitingPermission = true
				} else {
					h.waitingPermission = false
					cgEventTapEnable(h.tap, true)
					log.Printf("trigger: watchdog re-enabled event tap (enabled=%t)", cgEventTapIsEnabled(h.tap))
				}
			}
			h.watch(generation)
		})
	})
}

func hotkeyEvent(proxy, typ, event, user uintptr) uintptr {
	return hotkey.event(proxy, typ, event, user)
}
func (h *hotkeyListener) event(proxy, typ, event, user uintptr) uintptr {
	switch uint32(typ) {
	case cgEventTapDisabledByTime, cgEventTapDisabledByUser:
		// An interruption can lose a release. Do not treat the next press
		// as a repeat, or turn an orphan release into a toggle. In hold
		// mode, finish/cancel the interrupted gesture instead of leaving
		// the microphone waiting forever for the missing release.
		h.reset()
		log.Printf("trigger: event tap interrupted (%#x), re-enabling", uint32(typ))
		if h.tap != 0 {
			cgEventTapEnable(h.tap, true)
		}
	case 11: // A terminal key released before its modifiers.
		if len(h.chordModifiers) > 0 && h.chordKey >= 0 && cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) == h.chordKey && h.down {
			h.finishChord()
		}
	case cgEventKeyDown:
		if len(h.chordModifiers) > 0 && h.chordKey >= 0 && cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) == h.chordKey && h.matchesChord(cgEventGetFlags(event)) {
			if !h.down {
				h.down, h.combo = true, false
				if fn := h.onDown; fn != nil {
					go fn()
				}
			}
			return event
		}
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
		if cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) == 63 {
			h.fnDown = cgEventGetFlags(event)&0x800000 != 0
		}
		if len(h.chordModifiers) > 0 {
			matched := h.matchesChord(cgEventGetFlags(event))
			if h.down {
				if !h.holdsChordModifiers(cgEventGetFlags(event)) {
					h.finishChord()
				} else if !matched {
					h.combo = true
				}
			} else if matched && h.chordKey < 0 {
				h.down, h.combo = true, false
				if fn := h.onDown; fn != nil {
					go fn()
				}
			}
			return event
		}
		if cgEventGetIntegerValueField(event, cgKeyboardEventKeycode) != h.key.code {
			if h.down {
				h.combo = true
			}
			break
		}
		flags := cgEventGetFlags(event)
		down := flags&h.key.flag != 0
		if mask := modifierDeviceMask(uint16(h.key.code)); mask != 0 && flags&0xffff != 0 {
			down = flags&mask != 0
		}
		if down == h.down {
			break // a repeat: no second press
		}
		h.down = down
		// The callbacks leave the main thread at once: the tap must
		// return quickly.
		if down {
			// A modifier pressed after another modifier is part of that chord.
			// In particular, Ctrl+RightOption must not also toggle a standalone
			// RightOption translation binding when Ctrl was pressed first.
			h.combo = flags&(0x100000|0x40000|0x80000|0x20000|0x800000)&^h.key.flag != 0
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

func (h *hotkeyListener) finishChord() {
	h.down = false
	if fn := h.onUp; fn != nil {
		combo := h.combo
		go fn(combo)
	}
}
func (h *hotkeyListener) matchesChord(flags uint64) bool {
	if !h.holdsChordModifiers(flags) {
		return false
	}
	required := uint64(0)
	for _, key := range h.chordModifiers {
		required |= key.flag
	}
	return flags&(0x100000|0x40000|0x80000|0x20000|0x800000) == required
}
func (h *hotkeyListener) holdsChordModifiers(flags uint64) bool {
	for _, key := range h.chordModifiers {
		if key.code == 63 && !h.fnDown {
			return false
		}
		if flags&key.flag == 0 {
			return false
		}
		if key.code >= 0 && flags&0xffff != 0 {
			if mask := modifierDeviceMask(uint16(key.code)); mask != 0 && flags&mask == 0 {
				return false
			}
		}
	}
	return true
}
func nativeChord(key string) ([]modifierKey, int64, error) {
	identity := NativeShortcutIdentity(key)
	if identity == "" {
		return nil, -1, errors.New("unsupported shortcut: " + key)
	}
	mods := []modifierKey{}
	terminal := int64(-1)
	for _, part := range strings.Split(identity, "+") {
		if m, ok := modifierKeys[part]; ok {
			mods = append(mods, m)
			continue
		}
		flag := map[string]uint64{"Cmd": 0x100000, "Ctrl": 0x40000, "Alt": 0x80000, "Shift": 0x20000}[part]
		if flag != 0 {
			mods = append(mods, modifierKey{code: -1, flag: flag})
			continue
		}
		for code, name := range shortcutKeyCodes {
			if strings.EqualFold(name, part) {
				terminal = int64(code)
				break
			}
		}
		if terminal < 0 {
			return nil, -1, errors.New("unsupported key: " + part)
		}
	}
	return mods, terminal, nil
}

// StartHotkey watches a modifier key in every app. onDown is called as it
// goes down and onUp as it comes up, with combo true when another key was
// pressed in between. It needs the Input Monitoring permission, which it
// asks for once, and returns the function that stops it.
func StartHotkey(key string, onDown func(), onUp func(combo bool), onCancel func()) (func(), error) {
	return startHotkey(&hotkey, key, onDown, onUp, onCancel)
}
func StartTranslationHotkey(key string, onUp func(bool)) (func(), error) {
	return startHotkey(&translationHotkey, key, nil, onUp, nil)
}
func startHotkey(h *hotkeyListener, key string, onDown func(), onUp func(bool), onCancel func()) (func(), error) {
	if err := config.ValidateShortcut(key); err != nil {
		return nil, err
	}
	if err := load(); err != nil {
		return nil, err
	}
	k, ok := modifierKeys[key]
	if key == "" {
		k, ok = modifierKey{code: -1}, true
	}
	var chord []modifierKey
	terminal := int64(-1)
	if !ok {
		var err error
		chord, terminal, err = nativeChord(key)
		if err != nil {
			return nil, err
		}
	}
	var err error
	OnMain(func() {
		if h.tap != 0 {
			err = errors.New("the trigger key is already watched")
			return
		}
		// macOS can return a nonzero tap with keyboard events removed when
		// the current signing identity is not authorized. A saved system
		// Settings switch does not establish permission for this process.
		if !cgPreflightListenEvent() {
			err = errors.New("macOS Input Monitoring is not granted to this Voicemore process: authorize the currently installed app in System Settings → Privacy & Security → Input Monitoring, then reopen it")
			return
		}
		if h.callback == 0 {
			h.callback = purego.NewCallback(h.event)
		}
		mask := uint64(1)<<cgEventKeyDown | uint64(1)<<cgEventFlagsChanged | uint64(1)<<11
		// Observe before session-level shortcut filters. Some keyboard and
		// dictation utilities delay or consume Fn while another app is
		// frontmost. Remain listen-only: never suppress system shortcuts.
		location := uint32(cgHIDEventTap)
		tap := cgEventTapCreate(location, cgHeadInsertEventTap, cgEventTapListenOnly, mask, h.callback, 0)
		if tap == 0 {
			// Some macOS/security configurations do not allow HID taps.
			// Preserve the existing session-level compatibility path.
			location = cgSessionEventTap
			tap = cgEventTapCreate(location, cgHeadInsertEventTap, cgEventTapListenOnly, mask, h.callback, 0)
		}
		if tap == 0 {
			err = errors.New("macOS did not allow watching the keyboard: grant Input Monitoring to Voicemore in System Settings → Privacy & Security, then start it again")
			return
		}
		source := cfMachPortCreateRunLoopSource(0, tap, 0)
		if source == 0 {
			cfRelease(tap)
			err = errors.New("macOS could not create the keyboard listener run-loop source")
			return
		}
		log.Printf("trigger: listening key=%s layer=%d (0=HID, 1=session)", key, location)
		h.key, h.down, h.combo, h.onDown, h.onUp = k, false, false, onDown, onUp
		h.chordModifiers, h.chordKey, h.fnDown = chord, terminal, false
		h.onCancel = onCancel
		h.tap = tap
		h.source = source
		h.waitingPermission = false
		cfRunLoopAddSource(cfRunLoopGetMain(), h.source, cfRunLoopCommonModes)
		cgEventTapEnable(tap, true)
		h.generation++
		h.watch(h.generation)
	})
	if err != nil {
		return nil, err
	}
	return func() {
		OnMain(func() {
			if h.tap == 0 {
				return
			}
			h.generation++
			if h.watchdog != nil {
				h.watchdog.Stop()
				h.watchdog = nil
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
