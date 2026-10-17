package macos

import (
	"fmt"
	"voiceinput/internal/config"
)

type DeviceButtonEvent struct {
	Device      config.ButtonDevice
	Page, Usage uint32
	Unsupported bool `json:"unsupported,omitempty"`
}

func DeviceButtonName(page, usage uint32) string {
	if page == 12 {
		switch usage {
		case 233:
			return "Volume +"
		case 234:
			return "Volume −"
		case 205:
			return "Play / Pause"
		case 226:
			return "Mute"
		}
	}
	if page == 7 {
		if usage >= 58 && usage <= 69 {
			return fmt.Sprintf("F%d", usage-57)
		}
		if usage >= 104 && usage <= 115 {
			return fmt.Sprintf("F%d", usage-91)
		}
		if usage >= 4 && usage <= 29 {
			return string(rune('A' + usage - 4))
		}
		if usage >= 30 && usage <= 38 {
			return fmt.Sprintf("%d", usage-29)
		}
		if name := map[uint32]string{39: "0", 40: "Return", 41: "Escape", 42: "Delete", 43: "Tab", 44: "Space", 79: "Right", 80: "Left", 81: "Down", 82: "Up", 224: "Left Control", 225: "Left Shift", 226: "Left Option", 227: "Left Command", 228: "Right Control", 229: "Right Shift", 230: "Right Option", 231: "Right Command"}[usage]; name != "" {
			return name
		}
		return fmt.Sprintf("Key %02X", usage)
	}
	return fmt.Sprintf("%02X · %02X", page, usage)
}
