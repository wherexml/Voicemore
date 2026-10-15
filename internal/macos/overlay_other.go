//go:build !darwin

package macos

func ConfigureOverlay(handle uintptr)              {}
func ActiveOverlayWindow() (uint32, SettingsFrame) { return 0, SettingsFrame{} }
