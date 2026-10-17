//go:build darwin

package macos

import (
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

// A shared modifying Quartz tap consumes only source-matched configured keys.
// Every HID interface stays nonexclusive; unbound input is never reconstructed.
// All state lives on the main run loop.
var deviceButtonsTap struct {
	once                  sync.Once
	err                   error
	deviceService         func(uintptr) uint32
	registryID            func(uint32, *uint64) int32
	matchingID            func(uint64) uintptr
	matchingService       func(uint32, uintptr) uint32
	parent                func(uint32, string, *uint32) int32
	releaseObject         func(uint32) int32
	invalidatePort        func(uintptr)
	tap, source, callback uintptr
	generation            uint64
	watchdog              *time.Timer
	roots                 map[uint64]uintptr
	senders               map[uint64]uintptr
	held                  map[deviceButtonPress]string
	consumedDown          int
	consumedUp            int
}

type deviceButtonPress struct {
	sender      uint64
	page, usage uint32
}

// Quartz exposes the IORegistry sender as field 87 on supported macOS versions.
// This metadata is undocumented: never guess the device if it is absent or no
// ancestor matches a currently opened HID interface. Such events pass through.
const deviceEventSenderField = 87

func loadDeviceButtonTap() error {
	deviceButtonsTap.once.Do(func() {
		io, err := purego.Dlopen(frameworks+"IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			deviceButtonsTap.err = err
			return
		}
		for _, entry := range []struct {
			fn   any
			name string
		}{
			{&deviceButtonsTap.deviceService, "IOHIDDeviceGetService"},
			{&deviceButtonsTap.registryID, "IORegistryEntryGetRegistryEntryID"},
			{&deviceButtonsTap.matchingID, "IORegistryEntryIDMatching"},
			{&deviceButtonsTap.matchingService, "IOServiceGetMatchingService"},
			{&deviceButtonsTap.parent, "IORegistryEntryGetParentEntry"},
			{&deviceButtonsTap.releaseObject, "IOObjectRelease"},
		} {
			purego.RegisterLibFunc(entry.fn, io, entry.name)
		}
		cf, err := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			deviceButtonsTap.err = err
			return
		}
		purego.RegisterLibFunc(&deviceButtonsTap.invalidatePort, cf, "CFMachPortInvalidate")
		deviceButtonsTap.callback = purego.NewCallback(deviceButtonTapEvent)
	})
	return deviceButtonsTap.err
}

func ensureDeviceButtonTap(device uintptr) error {
	if err := loadDeviceButtonTap(); err != nil {
		return err
	}
	var id uint64
	service := deviceButtonsTap.deviceService(device)
	if service == 0 || deviceButtonsTap.registryID(service, &id) != 0 || id == 0 {
		return fmt.Errorf("could not identify device event source")
	}
	if deviceButtonsTap.tap != 0 {
		return nil
	}
	if !AccessibilityTrusted(false) {
		return fmt.Errorf("Accessibility permission is required to intercept device buttons")
	}
	mask := uint64(1)<<25 | uint64(1)<<26 | uint64(1)<<10 | uint64(1)<<11 | uint64(1)<<14 // Buttons, keyboard, auxiliary media; never flags, motion or scroll.
	deviceButtonsTap.generation++
	generation := deviceButtonsTap.generation
	tap := cgEventTapCreate(cgHIDEventTap, cgHeadInsertEventTap, 0, mask, deviceButtonsTap.callback, uintptr(generation))
	if tap == 0 {
		tap = cgEventTapCreate(cgSessionEventTap, cgHeadInsertEventTap, 0, mask, deviceButtonsTap.callback, uintptr(generation))
	}
	if tap == 0 {
		return fmt.Errorf("macOS did not allow intercepting device buttons")
	}
	source := cfMachPortCreateRunLoopSource(0, tap, 0)
	if source == 0 {
		deviceButtonsTap.invalidatePort(tap)
		cfRelease(tap)
		return fmt.Errorf("could not schedule device button interception")
	}
	deviceButtonsTap.tap, deviceButtonsTap.source = tap, source
	deviceButtonsTap.held = map[deviceButtonPress]string{}
	cfRunLoopAddSource(cfRunLoopGetMain(), source, cfRunLoopCommonModes)
	cgEventTapEnable(tap, true)
	watchDeviceButtonTap(generation)
	return nil
}

func refreshDeviceButtonSources() {
	if deviceButtonsTap.tap == 0 {
		return
	}
	deviceButtonsTap.roots = map[uint64]uintptr{}
	deviceButtonsTap.senders = map[uint64]uintptr{}
	for device := range buttonHID.devices {
		if !buttonHID.capabilities[device].CanBind || !buttonHID.opened[device] {
			continue
		}
		var id uint64
		service := deviceButtonsTap.deviceService(device)
		if service != 0 && deviceButtonsTap.registryID(service, &id) == 0 && id != 0 {
			deviceButtonsTap.roots[id] = device
		}
	}
	// Preserve swallowed downs across a rebind until their corresponding ups.
	// The next monitor can reuse the tap without leaking an orphan mouse-up.
	if len(deviceButtonsTap.roots) == 0 && len(deviceButtonsTap.held) == 0 {
		stopDeviceButtonTap()
	}
}

func forgetButtonDevice(device uintptr) {
	id := buttonHID.devices[device].ID
	for press, owner := range deviceButtonsTap.held {
		if owner == id {
			delete(deviceButtonsTap.held, press)
		}
	}
}

func stopDeviceButtonTap() {
	if deviceButtonsTap.tap == 0 {
		return
	}
	deviceButtonsTap.generation++
	if deviceButtonsTap.watchdog != nil {
		deviceButtonsTap.watchdog.Stop()
		deviceButtonsTap.watchdog = nil
	}
	cgEventTapEnable(deviceButtonsTap.tap, false)
	cfRunLoopRemoveSource(cfRunLoopGetMain(), deviceButtonsTap.source, cfRunLoopCommonModes)
	deviceButtonsTap.invalidatePort(deviceButtonsTap.tap)
	cfRelease(deviceButtonsTap.source)
	cfRelease(deviceButtonsTap.tap)
	deviceButtonsTap.tap, deviceButtonsTap.source = 0, 0
	deviceButtonsTap.roots, deviceButtonsTap.senders, deviceButtonsTap.held = nil, nil, nil
}

func watchDeviceButtonTap(generation uint64) {
	deviceButtonsTap.watchdog = time.AfterFunc(500*time.Millisecond, func() {
		OnMain(func() {
			if deviceButtonsTap.tap == 0 || deviceButtonsTap.generation != generation {
				return
			}
			if len(deviceButtonsTap.roots) == 0 && len(deviceButtonsTap.held) == 0 {
				stopDeviceButtonTap()
				return
			}
			if !cgEventTapIsEnabled(deviceButtonsTap.tap) && AccessibilityTrusted(false) {
				cgEventTapEnable(deviceButtonsTap.tap, true)
			}
			enabled := cgEventTapIsEnabled(deviceButtonsTap.tap)
			for _, device := range deviceButtonsTap.roots {
				d := buttonHID.devices[device]
				if !enabled {
					buttonHID.errors[d.ID] = "device button interception paused"
				} else if buttonHID.errors[d.ID] == "device button interception paused" {
					delete(buttonHID.errors, d.ID)
				}
			}
			watchDeviceButtonTap(generation)
		})
	})
}

func deviceEventSource(sender uint64) uintptr {
	if sender == 0 {
		return 0
	}
	if device, found := deviceButtonsTap.senders[sender]; found {
		return device
	}
	device := deviceButtonsTap.roots[sender]
	if device == 0 {
		matching := deviceButtonsTap.matchingID(sender)
		if matching != 0 {
			// IOServiceGetMatchingService consumes the matching dictionary.
			entry := deviceButtonsTap.matchingService(0, matching)
			for depth := 0; depth < 32 && entry != 0; depth++ {
				var id uint64
				if deviceButtonsTap.registryID(entry, &id) == 0 {
					device = deviceButtonsTap.roots[id]
				}
				if device != 0 {
					deviceButtonsTap.releaseObject(entry)
					entry = 0
					break
				}
				var parent uint32
				status := deviceButtonsTap.parent(entry, "IOService", &parent)
				deviceButtonsTap.releaseObject(entry)
				entry = 0
				if status != 0 {
					break
				}
				entry = parent
			}
			if entry != 0 {
				deviceButtonsTap.releaseObject(entry)
			}
		}
	}
	// Cache only a bounded set of sources. Hot-plug/rebind invalidates this map.
	if len(deviceButtonsTap.senders) < 256 {
		deviceButtonsTap.senders[sender] = device
	}
	return device
}

func deviceButtonTapEvent(proxy, typ, event, context uintptr) uintptr {
	if deviceButtonsTap.tap == 0 || uint64(context) != deviceButtonsTap.generation {
		return event
	}
	if uint32(typ) == cgEventTapDisabledByTime || uint32(typ) == cgEventTapDisabledByUser {
		// A lost release must not leave a key held forever or generate a new press.
		deviceButtonsTap.held = map[deviceButtonPress]string{}
		if AccessibilityTrusted(false) {
			cgEventTapEnable(deviceButtonsTap.tap, true)
		}
		return event
	}
	page, usage, down, repeat, ok := decodeDeviceButtonEvent(typ, event)
	if !ok {
		return event
	}
	sender := uint64(cgEventGetIntegerValueField(event, deviceEventSenderField))
	press := deviceButtonPress{sender: sender, page: page, usage: usage}
	if _, held := deviceButtonsTap.held[press]; held {
		if !down {
			delete(deviceButtonsTap.held, press)
			deviceButtonsTap.consumedUp++
		}
		return 0
	}
	if !down || sender == 0 || buttonHID.manager == 0 {
		return event
	}
	device := deviceEventSource(sender)
	d, known := buttonHID.devices[device]
	if !known || !buttonHID.opened[device] || !buttonHID.capabilities[device].Supports(page, usage) {
		return event
	}
	forward := isButtonCaptureDevice(d)
	if buttonHID.capture != "" && !forward {
		return event
	}
	if !forward {
		for _, binding := range buttonHID.bindings {
			if binding.Enabled && binding.Device.Matches(d) && binding.Page == page && binding.Usage == usage {
				forward = true
				break
			}
		}
	}
	if !forward {
		return event
	}
	deviceButtonsTap.held[press] = d.ID
	// Modifier-only trigger listeners no longer receive this consumed key.
	// Preserve their normal combination handling without dispatching another action.
	for _, listener := range []*hotkeyListener{&hotkey, &translationHotkey} {
		if listener.down {
			listener.combo = true
		}
	}
	deviceButtonsTap.consumedDown++
	buttonHID.lastConsumed[buttonInput{device, page, usage}] = time.Now()
	if buttonHID.errors[d.ID] == "deviceSourceUnavailable" {
		delete(buttonHID.errors, d.ID)
	}
	if !repeat && buttonHID.event != nil {
		buttonHID.event(DeviceButtonEvent{Device: d, Page: page, Usage: usage})
	}
	return 0
}

func decodeDeviceButtonEvent(typ, event uintptr) (page, usage uint32, down, repeat, ok bool) {
	switch typ {
	case 25, 26:
		n := cgEventGetIntegerValueField(event, 3) + 1
		if n < 3 || n > 65535 {
			return
		}
		return 9, uint32(n), typ == 25, false, true
	case 10, 11:
		n := cgEventGetIntegerValueField(event, cgKeyboardEventKeycode)
		if n < 0 || n > 65535 {
			return
		}
		u, known := keyboardButtonUsage(uint32(n))
		if !known {
			return
		}
		return 7, u, typ == 10, cgEventGetIntegerValueField(event, 8) != 0, true
	case 14:
		// NX_SYSDEFINED / NX_SUBTYPE_AUX_CONTROL_BUTTONS. Other system-defined
		// events (power, gestures, etc.) are never interpreted or modified.
		ns := class("NSEvent").Send(sel("eventWithCGEvent:"), event)
		if ns == 0 || ns.Send(sel("subtype")) != 8 {
			return
		}
		data := uint32(ns.Send(sel("data1")))
		state := (data >> 8) & 255
		if state != 10 && state != 11 {
			return
		}
		u := consumerButtonUsage(data >> 16)
		if u == 0 {
			return
		}
		return 12, u, state == 10, data&1 != 0, true
	}
	return
}
