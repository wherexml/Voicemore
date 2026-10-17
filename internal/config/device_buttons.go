package config

import "strings"

// ButtonDevice identifies a HID interface, independently of its display name.
// Audio interfaces are never opened; mouse interfaces are observed nonexclusively.
type ButtonDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	VendorID   uint32 `json:"vendorId"`
	ProductID  uint32 `json:"productId"`
	LocationID uint32 `json:"locationId,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Transport  string `json:"transport,omitempty"`
	UsagePage  uint32 `json:"usagePage"`
	Usage      uint32 `json:"usage"`
}
type DeviceButtonBinding struct {
	ID      string       `json:"id"`
	Device  ButtonDevice `json:"device"`
	Page    uint32       `json:"page"`
	Usage   uint32       `json:"usage"`
	Action  string       `json:"action"`
	Enabled bool         `json:"enabled"`
}

func (d ButtonDevice) IsMouse() bool { return d.UsagePage == 1 && d.Usage == 2 }

// A composite receiver can have keyboard companions. Only a serial or physical
// location establishes that they belong to this mouse; a display name cannot.
func (d ButtonDevice) SamePhysicalDevice(other ButtonDevice) bool {
	if d.VendorID != other.VendorID || d.ProductID != other.ProductID || d.Transport != other.Transport {
		return false
	}
	if d.Serial != "" && other.Serial != "" && d.Serial != other.Serial {
		return false
	}
	if d.LocationID != 0 && other.LocationID != 0 {
		return d.LocationID == other.LocationID
	}
	return d.Serial != "" && d.Serial == other.Serial
}

const LegacyDJIButtonID = "legacy-dji"

func (s Settings) AllDeviceButtons() []DeviceButtonBinding {
	out := append([]DeviceButtonBinding(nil), s.DeviceButtons...)
	if s.DJIButtonEnabled {
		out = append([]DeviceButtonBinding{{ID: LegacyDJIButtonID, Device: ButtonDevice{ID: LegacyDJIButtonID, Name: "DJI", VendorID: 11427, ProductID: 16401, UsagePage: 12, Usage: 1}, Page: 12, Usage: 233, Action: s.DJIButtonAction, Enabled: true}}, out...)
	}
	return out
}
func (s Settings) HasDeviceTranslation() bool {
	for _, b := range s.AllDeviceButtons() {
		if b.Enabled && b.Action == "translation" {
			return true
		}
	}
	return false
}

// Bindings are unique per interface and button, not per device. An edit may
// keep its own key, while disabled bindings still reserve their button.
func (s Settings) DeviceButtonTaken(device ButtonDevice, page, usage uint32, editing string) bool {
	for _, b := range s.AllDeviceButtons() {
		if b.ID != editing && b.Device.Matches(device) && b.Page == page && b.Usage == usage {
			return true
		}
	}
	return false
}

func (s *Settings) PutDeviceButton(b DeviceButtonBinding) {
	if b.ID == LegacyDJIButtonID {
		s.DJIButtonEnabled = false
	}
	for i, old := range s.DeviceButtons {
		if old.ID == b.ID {
			s.DeviceButtons[i] = b
			return
		}
	}
	s.DeviceButtons = append(s.DeviceButtons, b)
}
func (s *Settings) RemoveDeviceButton(id string) {
	if id == LegacyDJIButtonID {
		s.DJIButtonEnabled = false
	}
	for i, b := range s.DeviceButtons {
		if b.ID == id {
			s.DeviceButtons = append(s.DeviceButtons[:i], s.DeviceButtons[i+1:]...)
			return
		}
	}
}
func (d ButtonDevice) Matches(other ButtonDevice) bool {
	if d.ID == LegacyDJIButtonID {
		return other.VendorID == 11427 && other.ProductID == 16401 && other.UsagePage == 12 && other.Usage == 1
	}
	// Older bindings identify only the physical device and primary usage. They
	// remain compatible with a unique interface; the native monitor rejects an
	// old identity if it now matches multiple interfaces.
	return d.ID != "" && (d.ID == other.ID || (!strings.Contains(d.ID, ":") && strings.HasPrefix(other.ID, d.ID+":")))
}
