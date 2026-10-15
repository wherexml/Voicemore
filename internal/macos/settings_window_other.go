//go:build !darwin

package macos

func SystemSettingsFrame() (SettingsFrame, bool) { return SettingsFrame{}, false }
