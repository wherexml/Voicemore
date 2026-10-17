//go:build darwin && devicebuttonsacceptance

package macos

import "voiceinput/internal/config"

type DeviceCapabilityCase struct {
	Case   string `json:"case"`
	Passed bool   `json:"passed"`
}

// Policy counterexamples are declared before implementation. This complements
// the real-device Chrome E2E, and does not claim to simulate physical hardware.
func DeviceCapabilityPolicyAcceptance() (out []DeviceCapabilityCase) {
	mouse := config.ButtonDevice{ID: "one", Name: "Custom Mouse", UsagePage: 1, Usage: 2}
	keyboard := config.ButtonDevice{ID: "two", Name: "Custom Pedal", UsagePage: 1, Usage: 6}
	consumer := config.ButtonDevice{ID: "three", Name: "Custom Remote", UsagePage: 12, Usage: 1}
	btn := func(page, usage uint32) buttonElement {
		return buttonElement{Page: page, Usage: usage, Type: 2, Min: 0, Max: 1}
	}
	check := func(name string, ok bool) { out = append(out, DeviceCapabilityCase{name, ok}) }
	m := classifyButtonDevice(mouse, []buttonElement{btn(9, 1), btn(9, 2), btn(9, 4), btn(9, 8)})
	check("arbitrary-brand mouse side buttons support selective interception", m.Supports(9, 4) && m.Supports(9, 8) && !m.Supports(9, 1) && !m.Supports(9, 2))
	check("two-button mouse has explicit unsupported reason", classifyButtonDevice(mouse, []buttonElement{btn(9, 1), btn(9, 2)}).Reason != "")
	check("touchpad mouse compatibility is rejected", !classifyButtonDevice(mouse, []buttonElement{btn(9, 3), {Page: 13, Usage: 5, Type: 513}}).CanBind)
	k := classifyButtonDevice(keyboard, []buttonElement{btn(7, 13), btn(7, 14), btn(7, 224), btn(7, 57)})
	check("keyboard and pedal support physical J K but not modifier or lock", k.Supports(7, 13) && k.Supports(7, 14) && !k.Supports(7, 224) && !k.Supports(7, 57))
	c := classifyButtonDevice(consumer, []buttonElement{btn(12, 233), btn(12, 234), btn(11, 176)})
	check("multi-button media interface supports known keys without seizing interface", c.Supports(12, 233) && c.Supports(12, 234) && !c.Supports(11, 176))
	check("private media control is explicitly unsupported", !classifyButtonDevice(consumer, []buttonElement{btn(12, 9999)}).CanBind)
	private := config.ButtonDevice{ID: "private", Name: "Private Controller", UsagePage: 65280, Usage: 1}
	check("vendor protocol with keyboard-looking data is not guessed", !classifyButtonDevice(private, []buttonElement{btn(7, 13)}).CanBind)
	for _, pair := range [][2]uint32{{38, 13}, {40, 14}, {122, 58}, {36, 40}, {53, 41}, {123, 80}, {124, 79}, {76, 88}} {
		got, ok := keyboardButtonUsage(pair[0])
		check("scan code maps to physical HID usage", ok && got == pair[1])
	}
	_, ok := keyboardButtonUsage(57)
	check("caps lock is not mistaken for key-down/up", !ok)
	_, ok = keyboardButtonUsage(61)
	check("right option is not consumed without filtering flags", !ok)
	check("media up down and mute decode independently", consumerButtonUsage(0) == 233 && consumerButtonUsage(1) == 234 && consumerButtonUsage(7) == 226 && consumerButtonUsage(999) == 0)
	group := mergeButtonDeviceOptions([]ButtonDeviceOption{{Device: config.ButtonDevice{ID: "mouse", Name: "Same Name", VendorID: 10, ProductID: 20, LocationID: 1, Transport: "USB", UsagePage: 1, Usage: 2}, CanBind: true}, {Device: config.ButtonDevice{ID: "keys", Name: "Same Name", VendorID: 10, ProductID: 20, LocationID: 1, Transport: "USB", UsagePage: 1, Usage: 6}, CanBind: true}, {Device: config.ButtonDevice{ID: "other", Name: "Same Name", VendorID: 10, ProductID: 20, LocationID: 2, Transport: "USB", UsagePage: 1, Usage: 2}, CanBind: true}})
	check("composite interface grouped but same-name other device preserved", len(group) == 2)

	a := config.ButtonDevice{VendorID: 10, ProductID: 20, Transport: "USB", Serial: "one", LocationID: 1}
	b := a
	b.LocationID = 2
	check("cloned serial at different known ports stays separate", !a.SamePhysicalDevice(b))
	b.Serial = "two"
	b.LocationID = 1
	check("different serial never groups merely by name or location", !a.SamePhysicalDevice(b))

	b = a
	b.LocationID = 0
	check("serial can identify an interface without a reported location", a.SamePhysicalDevice(b))
	a.Serial = ""
	a.LocationID = 0
	b.Serial = ""
	b.LocationID = 0
	check("missing serial and location cannot establish physical identity", !a.SamePhysicalDevice(b))
	return
}
