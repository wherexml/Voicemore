package macos

import (
	"sort"
	"voiceinput/internal/config"
)

type ButtonKey struct {
	Page  uint32 `json:"page"`
	Usage uint32 `json:"usage"`
}

// Capability is derived from the descriptor, never a product-name allowlist.
// Actual capture still requires a successfully consumed, source-matched event.
type ButtonCapability struct {
	CanBind bool        `json:"canBind"`
	Reason  string      `json:"reason,omitempty"`
	Keys    []ButtonKey `json:"keys,omitempty"`
}

func (c ButtonCapability) Supports(page, usage uint32) bool {
	for _, key := range c.Keys {
		if key.Page == page && key.Usage == usage {
			return true
		}
	}
	return false
}

type buttonElement struct {
	Page, Usage, Type uint32
	Min, Max          int64
}

func classifyButtonDevice(d config.ButtonDevice, elements []buttonElement) ButtonCapability {
	c := ButtonCapability{Reason: "deviceSelectiveUnsupported"}
	keyboard := d.UsagePage == 1 && (d.Usage == 6 || d.Usage == 7)
	if !d.IsMouse() && !keyboard && d.UsagePage != 12 {
		return c
	}
	for _, e := range elements {
		if e.Page == 13 && e.Usage == 5 {
			return c
		}
	}
	keys := map[ButtonKey]bool{}
	add := func(page, usage uint32) {
		if usage != 0 {
			keys[ButtonKey{page, usage}] = true
		}
	}
	for _, e := range elements {
		if e.Type < 1 || e.Type > 4 || e.Min != 0 || e.Max != 1 {
			continue
		}
		switch {
		case d.IsMouse() && e.Page == 9 && e.Usage >= 3 && e.Usage <= 65535 && e.Type == 2:
			add(9, e.Usage)
		case keyboard && e.Page == 7:
			for _, usage := range keyboardButtonUsages {
				if usage == e.Usage {
					add(7, usage)
					break
				}
			}
			// macOS can turn the function row into system media events before Quartz.
			if e.Usage >= 58 && e.Usage <= 69 {
				for _, usage := range consumerButtonUsages {
					add(12, usage)
				}
			}
		case !d.IsMouse() && e.Page == 12:
			for _, usage := range consumerButtonUsages {
				if usage == e.Usage {
					add(12, usage)
					break
				}
			}
		}
	}
	for key := range keys {
		c.Keys = append(c.Keys, key)
	}
	sort.Slice(c.Keys, func(i, j int) bool {
		a, b := c.Keys[i], c.Keys[j]
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		return a.Usage < b.Usage
	})
	c.CanBind = len(c.Keys) > 0
	if c.CanBind {
		c.Reason = ""
	} else if d.IsMouse() {
		c.Reason = "deviceNoAuxiliaryButtons"
	}
	return c
}

// Physical Carbon scan codes -> USB HID usages. Locks and modifiers use
// FlagsChanged/system state rather than balanced KeyDown/KeyUp and are excluded.
var keyboardButtonUsages = map[uint32]uint32{
	0: 4, 1: 22, 2: 7, 3: 9, 4: 11, 5: 10, 6: 29, 7: 27, 8: 6, 9: 25, 11: 5, 12: 20, 13: 26, 14: 8, 15: 21, 16: 28, 17: 23,
	18: 30, 19: 31, 20: 32, 21: 33, 22: 35, 23: 34, 24: 46, 25: 38, 26: 36, 27: 45, 28: 37, 29: 39, 30: 48, 31: 18, 32: 24, 33: 47, 34: 12, 35: 19,
	36: 40, 37: 15, 38: 13, 39: 52, 40: 14, 41: 51, 42: 49, 43: 54, 44: 56, 45: 17, 46: 16, 47: 55, 48: 43, 49: 44, 50: 53, 51: 42, 53: 41,
	64: 108, 65: 99, 67: 85, 69: 87, 75: 84, 76: 88, 78: 86, 79: 109, 80: 110, 81: 103, 82: 98, 83: 89, 84: 90, 85: 91, 86: 92, 87: 93, 88: 94, 89: 95, 90: 111, 91: 96, 92: 97,
	96: 62, 97: 63, 98: 64, 99: 60, 100: 65, 101: 66, 103: 68, 105: 104, 106: 107, 107: 105, 109: 67, 111: 69, 113: 106, 114: 73, 115: 74, 116: 75, 117: 76, 118: 61, 119: 77, 120: 59, 121: 78, 122: 58, 123: 80, 124: 79, 125: 81, 126: 82,
}

func keyboardButtonUsage(code uint32) (uint32, bool) {
	usage, ok := keyboardButtonUsages[code]
	return usage, ok
}

// NX_KEYTYPE values from IOKit hidsystem/ev_keymap.h, mapped to HID Consumer.
var consumerButtonUsages = map[uint32]uint32{0: 233, 1: 234, 7: 226, 14: 184, 16: 205, 17: 181, 18: 182, 19: 179, 20: 180}

func consumerButtonUsage(flavor uint32) uint32 { return consumerButtonUsages[flavor] }
