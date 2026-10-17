package macos

import (
	"sort"
	"strings"
	"voiceinput/internal/config"
)

// ButtonDeviceOption distinguishes a connected device from a bindable interface.
// Audio-only entries never become HID capture targets or persisted bindings.
type ButtonDeviceOption struct {
	Device  config.ButtonDevice
	Kind    string
	CanBind bool
	Reason  string
}

func ConnectedButtonDevices() []ButtonDeviceOption {
	out := []ButtonDeviceOption{}
	hidNames := map[string]bool{}
	devices := ButtonDevices()
	for _, d := range devices {
		kind := "usbController"
		if d.Transport == "Bluetooth" || d.Transport == "Bluetooth Low Energy" {
			kind = "wirelessController"
		}
		if d.UsagePage == 1 && (d.Usage == 6 || d.Usage == 7) {
			kind = "externalKeyboard"
		}
		if d.IsMouse() {
			kind = "externalMouse"
		}
		if d.VendorID == 11427 && d.ProductID == 16401 {
			kind = "wirelessReceiver"
		}
		capability := ButtonDeviceCapabilities(d)
		out = append(out, ButtonDeviceOption{Device: d, Kind: kind, CanBind: capability.CanBind, Reason: capability.Reason})
		hidNames[strings.ToLower(d.Name)] = true
	}
	// Audio input/output endpoints can have different UIDs. Collapse their
	// display names only for these non-bindable informational rows.
	audioNames := map[string]bool{}
	for _, d := range bluetoothButtonAudioDevices() {
		name := strings.ToLower(d.Name)
		if !hidNames[name] && !audioNames[name] {
			audioNames[name] = true
			out = append(out, ButtonDeviceOption{Device: d, Kind: "bluetoothHeadphones", Reason: "deviceAudioOnly"})
		}
	}
	out = mergeButtonDeviceOptions(out)
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Device.Name) < strings.ToLower(out[j].Device.Name) })
	return out
}

// Pick one visible representative for each physical composite device. Capture
// listens to its supported siblings and persists the interface that really fired.
// Names never establish bindable-device identity.
func mergeButtonDeviceOptions(options []ButtonDeviceOption) (out []ButtonDeviceOption) {
	rank := func(o ButtonDeviceOption) int {
		n := 0
		if o.CanBind {
			n = 100
		}
		if o.Device.IsMouse() {
			return n + 3
		}
		if o.Device.UsagePage == 1 && (o.Device.Usage == 6 || o.Device.Usage == 7) {
			return n + 2
		}
		if o.Device.UsagePage == 12 {
			return n + 1
		}
		return n
	}
	for _, option := range options {
		found := -1
		for i, old := range out {
			if old.Device.SamePhysicalDevice(option.Device) {
				found = i
				break
			}
		}
		if found < 0 {
			out = append(out, option)
		} else if rank(option) > rank(out[found]) || (rank(option) == rank(out[found]) && option.Device.ID < out[found].Device.ID) {
			out[found] = option
		}
	}
	return
}
