//go:build darwin

package macos

import (
	"fmt"
	"github.com/ebitengine/purego/objc"
	"sort"
	"strings"
)

// CaptureShortcut is a local, consuming AppKit monitor. It never records
// another app's input and does not require an extra global input permission.
// done receives an empty shortcut for cancellation. All callbacks run on main.
func CaptureShortcut(handle uintptr, preview func(string), done func(string)) (func(), error) {
	if err := load(); err != nil {
		return nil, err
	}
	held := map[uint16]bool{}
	candidate := ""
	finished := false
	events := class("NSEvent")
	block := objc.NewBlock(func(_ objc.Block, event objc.ID) objc.ID {
		if finished {
			return event
		}
		typ := event.Send(sel("type"))
		keyWindow := class("NSApplication").Send(sel("sharedApplication")).Send(sel("keyWindow"))
		if keyWindow != objc.ID(handle) || typ == 1 || typ == 3 || typ == 25 {
			finished = true
			done("")
			return event
		}
		code := uint16(event.Send(sel("keyCode")))
		if code == 53 || code == 48 {
			finished = true
			done("")
			return 0
		}
		flags := uint64(event.Send(sel("modifierFlags")))
		if typ == 12 {
			name, spec := modifierByCode(code)
			if name == "" {
				return 0
			}
			down := flags&spec.flag != 0
			if mask := modifierDeviceMask(code); mask != 0 && flags&0xffff != 0 {
				down = flags&mask != 0
			}
			if down {
				held[code] = true
				parts := []string{}
				for heldCode := range held {
					if modifier, _ := modifierByCode(heldCode); modifier != "" {
						parts = append(parts, modifier)
					}
				}
				sort.Strings(parts)
				candidate = strings.Join(parts, "+")
				preview(candidate)
			} else {
				delete(held, code)
			}
		} else if typ == 10 {
			if objc.Send[bool](event, sel("isARepeat")) {
				return 0
			}
			key := shortcutKeyCodes[code]
			if key == "" {
				candidate = ""
				return 0
			}

			held[code] = true
			parts := []string{}
			if flags&0x100000 != 0 {
				parts = append(parts, "Cmd")
			}
			if flags&0x40000 != 0 {
				parts = append(parts, "Ctrl")
			}
			if flags&0x80000 != 0 {
				parts = append(parts, "Alt")
			}
			if flags&0x20000 != 0 {
				parts = append(parts, "Shift")
			}
			if held[63] {
				parts = append(parts, "Fn")
			}
			candidate = strings.Join(append(parts, key), "+")
			preview(candidate)
		} else if typ == 11 {
			delete(held, code)
		}
		if len(held) == 0 && candidate != "" {
			finished = true
			done(candidate)
		}
		return 0
	})
	monitor := events.Send(sel("addLocalMonitorForEventsMatchingMask:handler:"), uint64(1<<10|1<<11|1<<12|1<<1|1<<3|1<<25), block)
	block.Release()
	if monitor == 0 {
		return nil, fmt.Errorf("could not start shortcut recording")
	}
	return func() {
		finished = true
		if monitor != 0 {
			events.Send(sel("removeMonitor:"), monitor)
			monitor = 0
		}
	}, nil
}
func modifierByCode(code uint16) (string, modifierKey) {
	for name, spec := range modifierKeys {
		if spec.code == int64(code) {
			return name, spec
		}
	}
	return "", modifierKey{}
}
func modifierDeviceMask(code uint16) uint64 {
	return map[uint16]uint64{59: 1, 62: 0x2000, 56: 2, 60: 4, 55: 8, 54: 16, 58: 32, 61: 64}[code]
}

var shortcutKeyCodes = map[uint16]string{
	0: "A", 1: "S", 2: "D", 3: "F", 4: "H", 5: "G", 6: "Z", 7: "X", 8: "C", 9: "V", 11: "B", 12: "Q", 13: "W", 14: "E", 15: "R", 16: "Y", 17: "T",
	18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5", 24: "=", 25: "9", 26: "7", 27: "-", 28: "8", 29: "0", 30: "]", 31: "O", 32: "U", 33: "[", 34: "I", 35: "P", 36: "Enter", 37: "L", 38: "J", 39: "'", 40: "K", 41: ";", 42: "\\", 43: ",", 44: "/", 45: "N", 46: "M", 47: ".", 49: "Space", 50: "`", 51: "Backspace", 65: "NumDec", 67: "NumMult", 69: "NumAdd", 75: "NumDiv", 76: "Enter", 78: "NumSub", 82: "Num0", 83: "Num1", 84: "Num2", 85: "Num3", 86: "Num4", 87: "Num5", 88: "Num6", 89: "Num7", 91: "Num8", 92: "Num9", 64: "F17", 79: "F18", 80: "F19", 90: "F20", 96: "F5", 97: "F6", 98: "F7", 99: "F3", 100: "F8", 101: "F9", 103: "F11", 105: "F13", 106: "F16", 107: "F14", 109: "F10", 111: "F12", 113: "F15", 114: "Insert", 115: "Home", 116: "PageUp", 117: "Delete", 118: "F4", 119: "End", 120: "F2", 121: "PageDown", 122: "F1", 123: "Left", 124: "Right", 125: "Down", 126: "Up",
}
