//go:build darwin && devicebuttonsacceptance

package macos

import (
	"github.com/ebitengine/purego"
	"unsafe"
	"voiceinput/internal/config"
)

type DeviceElementEvidence struct {
	Page, Usage, Type uint32
	Min, Max          int64
}
type DeviceDescriptorEvidence struct {
	Device   config.ButtonDevice
	Elements []DeviceElementEvidence
}

// Read-only descriptor evidence, never opens or seizes the enumerated devices.
func DeviceDescriptorsEvidence() (out []DeviceDescriptorEvidence) {
	OnMain(func() {
		io, _ := purego.Dlopen(frameworks+"IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		var min, max func(uintptr) int64
		purego.RegisterLibFunc(&min, io, "IOHIDElementGetLogicalMin")
		purego.RegisterLibFunc(&max, io, "IOHIDElementGetLogicalMax")
		set := buttonHID.copyDevices(buttonHID.manager)
		if set == 0 {
			return
		}
		defer cfRelease(set)
		count := buttonHID.count(set)
		if count < 1 || count > 4096 {
			return
		}
		refs := make([]uintptr, count)
		buttonHID.values(set, unsafe.Pointer(&refs[0]))
		for _, ptr := range refs {
			d, _ := describeButtonDevice(ptr)
			if d.Name == "" || (d.Transport != "USB" && d.Transport != "Bluetooth" && d.Transport != "Bluetooth Low Energy") {
				continue
			}
			row := DeviceDescriptorEvidence{Device: d}
			es := buttonHID.elements(ptr, 0, 0)
			if es == 0 {
				continue
			}
			for i := int64(0); i < buttonHID.arrayCount(es); i++ {
				e := buttonHID.arrayValue(es, i)
				t := buttonHID.elementType(e)
				if t < 1 || t > 4 {
					continue
				}
				row.Elements = append(row.Elements, DeviceElementEvidence{buttonHID.page(e), buttonHID.usage(e), t, min(e), max(e)})
			}
			cfRelease(es)
			out = append(out, row)
		}
	})
	return
}
