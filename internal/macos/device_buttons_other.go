//go:build !darwin

package macos

import "voiceinput/internal/config"

func StartDeviceButtonMonitor([]config.DeviceButtonBinding, string, func(DeviceButtonEvent)) (func(), error) {
	return nil, errUnsupported
}
func ButtonDevices() []config.ButtonDevice         { return nil }
func ButtonDeviceError(config.ButtonDevice) string { return "" }

func ButtonDeviceCapabilities(config.ButtonDevice) ButtonCapability {
	return ButtonCapability{Reason: "deviceSelectiveUnsupported"}
}
