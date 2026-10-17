//go:build darwin

package macos

import (
	"crypto/sha256"
	"fmt"
	"voiceinput/internal/config"
)

// CoreAudio exposes connected Bluetooth audio endpoints even when macOS does
// not expose their controls as a per-device HID interface. Discovery is read-only.
func bluetoothButtonAudioDevices() []config.ButtonDevice {
	if loadDeviceAudio() != nil {
		return nil
	}
	out := []config.ButtonDevice{}
	seen := map[string]bool{}
	for _, id := range allDevices() {
		transport, err := readOutputProperty(id, audioPropertyAddress{0x7472616e, scopeGlob, 0})
		if err != nil || (transport != 0x626c7565 && transport != 0x626c6561) {
			continue
		}
		uid := deviceString(id, selUID)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		name := deviceString(id, selName)
		if name == "" {
			name = uid
		}
		sum := sha256.Sum256([]byte(uid))
		out = append(out, config.ButtonDevice{ID: fmt.Sprintf("audio-%x", sum[:12]), Name: name, Transport: "Bluetooth"})
	}
	return out
}
