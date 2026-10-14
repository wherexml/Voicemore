//go:build darwin

package macos

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// InputDevice is a microphone the system offers. UID is stable across
// reconnections and launches, unlike the numeric device ID: it is what the
// settings remember.
type InputDevice struct {
	UID, Name string
	Default   bool // the system's current default input
}

var (
	deviceLoad    sync.Once
	deviceErr     error
	deviceGetSize func(uint32, *audioPropertyAddress, uint32, uintptr, *uint32) int32
)

func loadDeviceAudio() error {
	deviceLoad.Do(func() {
		if deviceErr = loadOutputAudio(); deviceErr != nil {
			return
		}
		if deviceErr = load(); deviceErr != nil {
			return
		}
		lib, err := purego.Dlopen(frameworks+"CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			deviceErr = err
			return
		}
		purego.RegisterLibFunc(&deviceGetSize, lib, "AudioObjectGetPropertyDataSize")
	})
	return deviceErr
}

const (
	selDevices = 0x64657623 // dev#
	selStreams = 0x73746d23 // stm#
	selName    = 0x6c6e616d // lnam
	selUID     = 0x75696420 // uid
	scopeGlob  = 0x676c6f62 // glob
	scopeInput = 0x696e7074 // inpt
)

// deviceString reads a CFString property of a device, released after copying.
func deviceString(device, selector uint32) string {
	var ref uintptr
	size := uint32(unsafe.Sizeof(ref))
	addr := audioPropertyAddress{selector, scopeGlob, 0}
	if outputGet(device, &addr, 0, 0, &size, unsafe.Pointer(&ref)) != 0 || ref == 0 {
		return ""
	}
	defer cfRelease(ref)
	return goString(objc.ID(ref))
}

// hasInput says the device has at least one input stream.
func hasInput(device uint32) bool {
	var size uint32
	addr := audioPropertyAddress{selStreams, scopeInput, 0}
	return deviceGetSize(device, &addr, 0, 0, &size) == 0 && size > 0
}

// allDevices lists the audio device IDs the system knows.
func allDevices() []uint32 {
	var size uint32
	addr := audioPropertyAddress{selDevices, scopeGlob, 0}
	if deviceGetSize(1, &addr, 0, 0, &size) != 0 || size == 0 {
		return nil
	}
	ids := make([]uint32, size/4)
	if outputGet(1, &addr, 0, 0, &size, unsafe.Pointer(&ids[0])) != 0 {
		return nil
	}
	return ids[:size/4]
}

// InputDevices lists the connected microphones, the default one marked,
// sorted by name. It is cheap enough to call when a settings page draws.
func InputDevices() []InputDevice {
	if loadDeviceAudio() != nil {
		return nil
	}
	def, _ := defaultInputDevice()
	var out []InputDevice
	for _, id := range allDevices() {
		if !hasInput(id) {
			continue
		}
		uid := deviceString(id, selUID)
		if uid == "" {
			continue
		}
		name := deviceString(id, selName)
		if name == "" {
			name = uid
		}
		out = append(out, InputDevice{UID: uid, Name: name, Default: id == def})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// inputDeviceByUID finds the connected input device with this UID; 0 when
// it is not connected.
func inputDeviceByUID(uid string) uint32 {
	if uid == "" || loadDeviceAudio() != nil {
		return 0
	}
	for _, id := range allDevices() {
		if hasInput(id) && deviceString(id, selUID) == uid {
			return id
		}
	}
	return 0
}

// routeQueue points an input queue at a device before it starts. It
// returns the device used, or 0 when the system default is kept: no device
// was chosen, the chosen one is not connected, or the queue refused it.
func routeQueue(q uintptr, uid string) uint32 {
	device := inputDeviceByUID(uid)
	if device == 0 {
		if uid != "" {
			log.Printf("microphone %q is not connected; recording from the system default", uid)
		}
		return 0
	}
	str := nsString(uid)
	ref := uintptr(str)
	if st := audioQueueSetProperty(q, 0x61716364, unsafe.Pointer(&ref), uint32(unsafe.Sizeof(ref))); st != 0 { // aqcd
		log.Printf("microphone %q could not be selected (OSStatus %d); recording from the system default", uid, st)
		return 0
	}
	return device
}

// Login item: the app's own entry in System Settings → General → Login
// Items, through SMAppService (macOS 13 and later). It only works for the
// signed app bundle; a bare executable gets an error.

var (
	smLoad sync.Once
	smErr  error
)

func loadServiceManagement() error {
	smLoad.Do(func() {
		if smErr = load(); smErr != nil {
			return
		}
		_, smErr = purego.Dlopen(frameworks+"ServiceManagement.framework/ServiceManagement", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	})
	return smErr
}

// LoginItemStatus is "enabled", "approval" (registered, but the user must
// allow it in System Settings), "off" or "unavailable".
func LoginItemStatus() string {
	if loadServiceManagement() != nil {
		return "unavailable"
	}
	status := -1
	OnMain(func() {
		if svc := class("SMAppService").Send(sel("mainAppService")); svc != 0 {
			status = objc.Send[int](svc, sel("status"))
		}
	})
	switch status {
	case 1:
		return "enabled"
	case 2:
		return "approval"
	case 0:
		return "off"
	}
	return "unavailable"
}

// SetLoginItem registers or unregisters the app as a login item.
func SetLoginItem(on bool) error {
	if err := loadServiceManagement(); err != nil {
		return err
	}
	var failure string
	OnMain(func() {
		svc := class("SMAppService").Send(sel("mainAppService"))
		if svc == 0 {
			failure = "SMAppService is unavailable"
			return
		}
		var nsErr objc.ID
		name := "unregisterAndReturnError:"
		if on {
			name = "registerAndReturnError:"
		}
		if !objc.Send[bool](svc, sel(name), unsafe.Pointer(&nsErr)) {
			failure = "the system refused"
			if nsErr != 0 {
				failure = goString(nsErr.Send(sel("localizedDescription")))
			}
		}
	})
	if failure != "" {
		return fmt.Errorf("%s", failure)
	}
	return nil
}

// OpenLoginItemsSettings opens System Settings → General → Login Items,
// where a registration waiting for approval is allowed.
func OpenLoginItemsSettings() {
	if loadServiceManagement() != nil {
		return
	}
	OnMain(func() { class("SMAppService").Send(sel("openSystemSettingsLoginItems")) })
}

// SetDockIcon shows the app in the Dock and the app switcher (regular
// activation policy) or hides it there (accessory). The menu bar item and
// open windows remain; hiding keeps the app frontmost so its window stays.
func SetDockIcon(show bool) {
	if load() != nil {
		return
	}
	OnMain(func() {
		nsApp := class("NSApplication").Send(sel("sharedApplication"))
		policy := 1 // NSApplicationActivationPolicyAccessory
		if show {
			policy = 0 // NSApplicationActivationPolicyRegular
		}
		if objc.Send[int](nsApp, sel("activationPolicy")) == policy {
			return
		}
		objc.Send[bool](nsApp, sel("setActivationPolicy:"), policy)
		// Changing the policy deactivates the app: bring it back so the
		// settings window the user just used stays in front.
		nsApp.Send(sel("activateIgnoringOtherApps:"), true)
	})
}
