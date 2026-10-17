//go:build !darwin

package macos

import "voiceinput/internal/config"

func bluetoothButtonAudioDevices() []config.ButtonDevice { return nil }
