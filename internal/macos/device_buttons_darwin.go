//go:build darwin

package macos

import (
	"crypto/sha256"
	"fmt"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"sort"
	"sync"
	"time"
	"unsafe"
	"voiceinput/internal/config"
)

// The manager enumerates only. Individual selected HID interfaces are opened;
// discovery never claims all keyboards, audio interfaces or pointing devices.
var buttonHID struct {
	once                     sync.Once
	err                      error
	create                   func(uintptr, uint32) uintptr
	match                    func(uintptr, uintptr)
	schedule, unschedule     func(uintptr, uintptr, uintptr)
	copyDevices              func(uintptr) uintptr
	count                    func(uintptr) int64
	values                   func(uintptr, unsafe.Pointer)
	property                 func(uintptr, uintptr) uintptr
	open, close              func(uintptr, uint32) int32
	input, matching, removal func(uintptr, uintptr, uintptr)
	elements                 func(uintptr, uintptr, uint32) uintptr
	arrayCount               func(uintptr) int64
	arrayValue               func(uintptr, int64) uintptr
	element                  func(uintptr) uintptr
	page, usage              func(uintptr) uint32
	elementType              func(uintptr) uint32
	logicalMin, logicalMax   func(uintptr) int64
	integer                  func(uintptr) int64
	device                   func(uintptr) uintptr
	callback, added, removed uintptr
	manager, generation      uintptr
	devices                  map[uintptr]config.ButtonDevice
	capabilities             map[uintptr]ButtonCapability
	opened                   map[uintptr]bool
	errors                   map[string]string
	down                     map[buttonInput]bool
	arrays                   map[uintptr]uint32
	lastConsumed             map[buttonInput]time.Time
	bindings                 []config.DeviceButtonBinding
	capture                  string
	event                    func(DeviceButtonEvent)
}

type buttonInput struct {
	device      uintptr
	page, usage uint32
}

func loadButtonHID() error {
	if err := load(); err != nil {
		return err
	}
	buttonHID.once.Do(func() {
		lib, e := purego.Dlopen(frameworks+"IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if e != nil {
			buttonHID.err = e
			return
		}
		for _, entry := range []struct {
			fn   any
			name string
		}{
			{&buttonHID.create, "IOHIDManagerCreate"}, {&buttonHID.match, "IOHIDManagerSetDeviceMatching"},
			{&buttonHID.schedule, "IOHIDManagerScheduleWithRunLoop"}, {&buttonHID.unschedule, "IOHIDManagerUnscheduleFromRunLoop"},
			{&buttonHID.copyDevices, "IOHIDManagerCopyDevices"}, {&buttonHID.property, "IOHIDDeviceGetProperty"},
			{&buttonHID.open, "IOHIDDeviceOpen"}, {&buttonHID.close, "IOHIDDeviceClose"}, {&buttonHID.input, "IOHIDDeviceRegisterInputValueCallback"},
			{&buttonHID.elements, "IOHIDDeviceCopyMatchingElements"},
			{&buttonHID.matching, "IOHIDManagerRegisterDeviceMatchingCallback"}, {&buttonHID.removal, "IOHIDManagerRegisterDeviceRemovalCallback"},
			{&buttonHID.element, "IOHIDValueGetElement"}, {&buttonHID.page, "IOHIDElementGetUsagePage"}, {&buttonHID.usage, "IOHIDElementGetUsage"},
			{&buttonHID.elementType, "IOHIDElementGetType"},
			{&buttonHID.logicalMin, "IOHIDElementGetLogicalMin"}, {&buttonHID.logicalMax, "IOHIDElementGetLogicalMax"},
			{&buttonHID.integer, "IOHIDValueGetIntegerValue"}, {&buttonHID.device, "IOHIDElementGetDevice"},
		} {
			purego.RegisterLibFunc(entry.fn, lib, entry.name)
		}
		cf, e := purego.Dlopen(frameworks+"CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if e != nil {
			buttonHID.err = e
			return
		}
		purego.RegisterLibFunc(&buttonHID.count, cf, "CFSetGetCount")
		purego.RegisterLibFunc(&buttonHID.values, cf, "CFSetGetValues")
		purego.RegisterLibFunc(&buttonHID.arrayCount, cf, "CFArrayGetCount")
		purego.RegisterLibFunc(&buttonHID.arrayValue, cf, "CFArrayGetValueAtIndex")
		buttonHID.added = purego.NewCallback(func(ctx uintptr, result int32, sender, device uintptr) {
			if result == 0 && ctx == buttonHID.generation && buttonHID.manager != 0 {
				addButtonDevice(device)
				activateButtonDevices()
			}
		})
		buttonHID.removed = purego.NewCallback(func(ctx uintptr, result int32, sender, device uintptr) {
			if ctx != buttonHID.generation {
				return
			}
			forgetButtonDevice(device)
			if buttonHID.opened[device] {
				buttonHID.input(device, 0, 0)
				buttonHID.close(device, 0)
			}
			delete(buttonHID.devices, device)
			delete(buttonHID.capabilities, device)
			delete(buttonHID.opened, device)
			for k := range buttonHID.down {
				if k.device == device {
					delete(buttonHID.down, k)
				}
			}
			activateButtonDevices()
		})
		buttonHID.callback = purego.NewCallback(func(ctx uintptr, result int32, sender, value uintptr) {
			if result != 0 || ctx != buttonHID.generation || buttonHID.manager == 0 {
				return
			}
			element := buttonHID.element(value)
			dev := buttonHID.device(element)
			if !buttonHID.opened[dev] {
				return
			}
			// Mouse actions are dispatched by the modifying Quartz tap, after
			// their original down/up pair is consumed. Never double-dispatch HID.
			if buttonHID.capture != "" && !isButtonCaptureDevice(buttonHID.devices[dev]) {
				return
			}
			page, usage := buttonHID.page(element), buttonHID.usage(element)
			d := buttonHID.devices[dev]
			n := buttonHID.integer(value)
			if (page == 7 || page == 12 || page == 9) && (usage == 0 || usage == ^uint32(0)) {
				prior := buttonHID.arrays[element]
				if n < 0 || n > 65535 || (page == 7 && n > 231) {
					return
				}
				if prior != 0 && prior != uint32(n) {
					delete(buttonHID.down, buttonInput{dev, page, prior})
				}
				buttonHID.arrays[element] = uint32(n)
				if n == 0 || (page == 7 && n < 4) {
					return
				}
				usage = uint32(n)
				n = 1
			}

			if n != 0 && n != 1 {
				return
			}
			key := buttonInput{dev, page, usage}
			prior := buttonHID.down[key]
			buttonHID.down[key] = n != 0
			if n == 0 || prior {
				return
			}
			// Raw HID is diagnostic only. Never trigger an action from it.
			supported := buttonHID.capabilities[dev].Supports(page, usage)
			if !supported {
				if buttonHID.capture != "" && !d.IsMouse() && (page == 7 || page == 12) && buttonHID.event != nil {
					buttonHID.event(DeviceButtonEvent{Device: d, Page: page, Usage: usage, Unsupported: true})
				}
				return
			}
			selected := isButtonCaptureDevice(d)
			if !selected {
				for _, b := range buttonHID.bindings {
					if b.Enabled && b.Device.Matches(d) && b.Page == page && b.Usage == usage {
						selected = true
						break
					}
				}
			}
			if selected {
				checkButtonCaptureSource(key, d.ID)
			}
		})
	})
	return buttonHID.err
}
func hidNumber(device uintptr, key string) uint32 {
	p := objc.ID(buttonHID.property(device, uintptr(nsString(key))))
	if p == 0 {
		return 0
	}
	if !objc.Send[bool](p, sel("respondsToSelector:"), sel("unsignedIntValue")) {
		return 0
	}
	return objc.Send[uint32](p, sel("unsignedIntValue"))
}
func describeButtonDevice(device uintptr) (config.ButtonDevice, bool) {
	d := config.ButtonDevice{VendorID: hidNumber(device, "VendorID"), ProductID: hidNumber(device, "ProductID"), LocationID: hidNumber(device, "LocationID"), UsagePage: hidNumber(device, "PrimaryUsagePage"), Usage: hidNumber(device, "PrimaryUsage")}
	text := func(k string) string { return goString(objc.ID(buttonHID.property(device, uintptr(nsString(k))))) }
	d.Name, d.Serial, d.Transport = text("Product"), text("SerialNumber"), text("Transport")
	if hidNumber(device, "Built-In") != 0 {
		return d, false
	}
	if (d.Transport != "USB" && d.Transport != "Bluetooth" && d.Transport != "Bluetooth Low Energy") || d.Name == "" {
		return d, false
	}
	// Without serial or physical location, identical interfaces cannot be safely
	// distinguished. Such devices can be shown but ambiguous duplicates rejected.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%d/%s/%s/%d/%d", d.VendorID, d.ProductID, d.LocationID, d.Serial, d.Transport, d.UsagePage, d.Usage)))
	d.ID = fmt.Sprintf("hid-%x", sum[:12])
	// Composite receivers can expose multiple keyboard interfaces with the
	// same primary usage and physical identity. Their report descriptors are
	// stable interface fingerprints; enumeration order and native pointers are
	// not persistent identities. Equal descriptors still fail closed below.
	data := objc.ID(buttonHID.property(device, uintptr(nsString("ReportDescriptor"))))
	if data != 0 && objc.Send[bool](data, sel("isKindOfClass:"), objc.GetClass("NSData")) {
		length := objc.Send[uint64](data, sel("length"))
		if length > 0 && length <= 65536 {
			bytes := objc.Send[uintptr](data, sel("bytes"))
			if bytes != 0 {
				fingerprint := sha256.Sum256(unsafe.Slice((*byte)(unsafe.Pointer(bytes)), int(length)))
				d.ID += fmt.Sprintf(":%x", fingerprint[:12])
			}
		}
	}
	return d, true
}
func addButtonDevice(device uintptr) {
	d, ok := describeButtonDevice(device)
	if !ok {
		return
	}
	if _, exists := buttonHID.devices[device]; exists {
		return
	}
	buttonHID.devices[device] = d
	buttonHID.capabilities[device] = classifyButtonDevice(d, describeButtonElements(device))
}

// Resolve all identities before opening anything, including at startup and
// after hot-plug. Legacy bindings must never silently choose one sibling.
func activateButtonDevices() {
	buttonHID.errors = map[string]string{}
	counts := map[string]int{}
	for _, d := range buttonHID.devices {
		counts[d.ID]++
	}
	for device, d := range buttonHID.devices {
		selected := isButtonCaptureDevice(d)
		problem := ""
		if counts[d.ID] > 1 {
			problem = "ambiguous device identity"
		}
		for _, b := range buttonHID.bindings {
			if !b.Enabled || !b.Device.Matches(d) {
				continue
			}
			if buttonHID.capabilities[device].Supports(b.Page, b.Usage) {
				selected = true
			}
			matches := 0
			for _, actual := range buttonHID.devices {
				if b.Device.Matches(actual) {
					matches++
				}
			}
			if matches > 1 {
				problem = "ambiguous device identity; identify button again"
			}
		}
		if problem != "" {
			buttonHID.errors[d.ID] = problem
			if buttonHID.opened[device] {
				buttonHID.input(device, 0, 0)
				buttonHID.close(device, 0)
				delete(buttonHID.opened, device)
				for key := range buttonHID.down {
					if key.device == device {
						delete(buttonHID.down, key)
					}
				}
			}
			continue
		}
		if selected && !buttonHID.opened[device] {
			capability := buttonHID.capabilities[device]
			if !capability.CanBind {
				buttonHID.errors[d.ID] = capability.Reason
				continue
			}

			openButtonDevice(device, d)
		}
	}
	refreshDeviceButtonSources()
}

func openButtonDevice(device uintptr, d config.ButtonDevice) {
	// No interface is seized: only matched Quartz down/up pairs are consumed.
	if err := ensureDeviceButtonTap(device); err != nil {
		buttonHID.errors[d.ID] = err.Error()
		return
	}
	if result := buttonHID.open(device, 0); result != 0 {
		buttonHID.errors[d.ID] = fmt.Sprintf("HID access failed: %#x", uint32(result))
		return
	}
	buttonHID.opened[device] = true
	buttonHID.input(device, buttonHID.callback, buttonHID.generation)
}

func describeButtonElements(device uintptr) (out []buttonElement) {
	elements := buttonHID.elements(device, 0, 0)
	if elements == 0 {
		return
	}
	defer cfRelease(elements)
	count := buttonHID.arrayCount(elements)
	if count < 1 || count > 4096 {
		return
	}
	for i := int64(0); i < count; i++ {
		e := buttonHID.arrayValue(elements, i)
		out = append(out, buttonElement{Page: buttonHID.page(e), Usage: buttonHID.usage(e), Type: buttonHID.elementType(e), Min: buttonHID.logicalMin(e), Max: buttonHID.logicalMax(e)})
	}
	return
}

func isButtonCaptureDevice(d config.ButtonDevice) bool {
	if buttonHID.capture == "" {
		return false
	}
	for _, selected := range buttonHID.devices {
		if selected.ID == buttonHID.capture {
			return selected.ID == d.ID || selected.SamePhysicalDevice(d)
		}
	}
	return false
}
func ButtonDeviceCapabilities(d config.ButtonDevice) (out ButtonCapability) {
	out.Reason = "deviceSelectiveUnsupported"
	OnMain(func() {
		matches := 0
		for ptr, actual := range buttonHID.devices {
			if d.Matches(actual) {
				matches++
				out = buttonHID.capabilities[ptr]
			}
		}
		if matches > 1 {
			out = ButtonCapability{Reason: "deviceIdentityAmbiguous"}
		}
	})
	return
}

func StartDeviceButtonMonitor(bindings []config.DeviceButtonBinding, capture string, event func(DeviceButtonEvent)) (func(), error) {
	if err := loadButtonHID(); err != nil {
		return nil, err
	}
	var startErr error
	var generation uintptr
	OnMain(func() {
		if buttonHID.manager != 0 {
			startErr = fmt.Errorf("device buttons already watched")
			return
		}
		buttonHID.generation++
		generation = buttonHID.generation
		buttonHID.devices = map[uintptr]config.ButtonDevice{}
		buttonHID.capabilities = map[uintptr]ButtonCapability{}
		buttonHID.opened = map[uintptr]bool{}
		buttonHID.errors = map[string]string{}
		buttonHID.down = map[buttonInput]bool{}
		buttonHID.arrays = map[uintptr]uint32{}
		buttonHID.lastConsumed = map[buttonInput]time.Time{}
		buttonHID.bindings = append([]config.DeviceButtonBinding(nil), bindings...)
		buttonHID.capture = capture
		buttonHID.event = event
		deviceButtonsTap.consumedDown, deviceButtonsTap.consumedUp = 0, 0
		manager := buttonHID.create(0, 0)
		if manager == 0 {
			startErr = fmt.Errorf("could not enumerate HID devices")
			return
		}
		buttonHID.manager = manager
		buttonHID.match(manager, 0)
		buttonHID.matching(manager, buttonHID.added, generation)
		buttonHID.removal(manager, buttonHID.removed, generation)
		buttonHID.schedule(manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
		devices := buttonHID.copyDevices(manager)
		if devices != 0 {
			count := buttonHID.count(devices)
			if count > 0 && count < 4096 {
				refs := make([]uintptr, int(count))
				buttonHID.values(devices, unsafe.Pointer(&refs[0]))
				for _, ref := range refs {
					addButtonDevice(ref)
				}
			}
			cfRelease(devices)
		}
		activateButtonDevices()
	})
	if startErr != nil {
		return nil, startErr
	}
	return func() {
		OnMain(func() {
			if buttonHID.manager == 0 || buttonHID.generation != generation {
				return
			}
			buttonHID.generation++
			for dev := range buttonHID.opened {
				buttonHID.input(dev, 0, 0)
				buttonHID.close(dev, 0)
			}
			buttonHID.unschedule(buttonHID.manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
			cfRelease(buttonHID.manager)
			buttonHID.manager = 0
			buttonHID.event = nil
			buttonHID.devices = nil
			buttonHID.capabilities = nil
			buttonHID.opened = nil
			refreshDeviceButtonSources()
		})
	}, nil
}
func ButtonDevices() []config.ButtonDevice {
	out := []config.ButtonDevice{}
	OnMain(func() {
		seen := map[string]bool{}
		for _, d := range buttonHID.devices {
			if !seen[d.ID] {
				out = append(out, d)
				seen[d.ID] = true
			}
		}
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}
func ButtonDeviceError(d config.ButtonDevice) string {
	text := ""
	OnMain(func() {
		for _, actual := range buttonHID.devices {
			if d.Matches(actual) {
				text = buttonHID.errors[actual.ID]
				return
			}
		}
	})
	return text
}

// Timing is used only to diagnose a missing consuming event. It never infers
// identity, authorizes consumption or triggers an action. Unknown sources pass.
func checkButtonCaptureSource(key buttonInput, id string) {
	started, generation := time.Now(), buttonHID.generation
	time.AfterFunc(350*time.Millisecond, func() {
		OnMain(func() {
			if buttonHID.manager == 0 || buttonHID.generation != generation {
				return
			}
			if buttonHID.lastConsumed[key].Before(started.Add(-350 * time.Millisecond)) {
				buttonHID.errors[id] = "deviceSourceUnavailable"
			}
		})
	})
}
