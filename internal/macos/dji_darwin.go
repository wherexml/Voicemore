//go:build darwin

package macos

import (
	"fmt"
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"log"
	"sync"
)

// Only the verified DJI receiver's consumer-control interface is claimed.
// Its audio interface and all other keyboards remain available to macOS.
var dji struct {
	once            sync.Once
	err             error
	create          func(uintptr, uint32) uintptr
	match           func(uintptr, uintptr)
	register        func(uintptr, uintptr, uintptr)
	removal         func(uintptr, uintptr, uintptr)
	removedCallback uintptr
	schedule        func(uintptr, uintptr, uintptr)
	unschedule      func(uintptr, uintptr, uintptr)
	open            func(uintptr, uint32) int32
	close           func(uintptr, uint32) int32
	element         func(uintptr) uintptr
	page            func(uintptr) uint32
	usage           func(uintptr) uint32
	integer         func(uintptr) int64
	device          func(uintptr) uintptr
	callback        uintptr
	manager         uintptr
	down            map[uintptr]bool
	toggle          func()
}

// StartDJIButton maps the receiver's volume-up button to a recording toggle.
// Exclusive HID access suppresses its original volume action. No global
// volume events are intercepted. Closing the manager restores normal use.
func StartDJIButton(toggle func()) (func(), error) {
	if err := load(); err != nil {
		return nil, err
	}
	dji.once.Do(func() {
		lib, err := purego.Dlopen(frameworks+"IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			dji.err = err
			return
		}
		for _, entry := range []struct {
			fn   any
			name string
		}{
			{&dji.create, "IOHIDManagerCreate"}, {&dji.match, "IOHIDManagerSetDeviceMatching"},
			{&dji.removal, "IOHIDManagerRegisterDeviceRemovalCallback"}, {&dji.register, "IOHIDManagerRegisterInputValueCallback"}, {&dji.schedule, "IOHIDManagerScheduleWithRunLoop"},
			{&dji.unschedule, "IOHIDManagerUnscheduleFromRunLoop"}, {&dji.open, "IOHIDManagerOpen"}, {&dji.close, "IOHIDManagerClose"},
			{&dji.element, "IOHIDValueGetElement"}, {&dji.page, "IOHIDElementGetUsagePage"}, {&dji.usage, "IOHIDElementGetUsage"},
			{&dji.integer, "IOHIDValueGetIntegerValue"}, {&dji.device, "IOHIDElementGetDevice"},
		} {
			purego.RegisterLibFunc(entry.fn, lib, entry.name)
		}
		dji.removedCallback = purego.NewCallback(func(context uintptr, result int32, sender, device uintptr) { delete(dji.down, device) })
		dji.callback = purego.NewCallback(func(context uintptr, result int32, sender, value uintptr) {
			if result != 0 || dji.manager == 0 {
				return
			}
			e := dji.element(value)
			if dji.page(e) != 12 || dji.usage(e) != 233 {
				return
			}
			device := dji.device(e)
			down := dji.integer(value) != 0
			previous := dji.down[device]
			dji.down[device] = down
			if down && !previous && dji.toggle != nil {
				log.Print("trigger: DJI receiver button toggle")
				go dji.toggle()
			}
		})
	})
	if dji.err != nil {
		return nil, dji.err
	}
	var startErr error
	OnMain(func() {
		if dji.manager != 0 {
			startErr = fmt.Errorf("DJI button already watched")
			return
		}
		dict := objc.ID(objc.GetClass("NSMutableDictionary")).Send(objc.RegisterName("dictionary"))
		for key, value := range map[string]int{"VendorID": 11427, "ProductID": 16401, "PrimaryUsagePage": 12, "PrimaryUsage": 1} {
			number := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithInt:"), value)
			dict.Send(objc.RegisterName("setObject:forKey:"), number, nsString(key))
		}
		manager := dji.create(0, 0)
		if manager == 0 {
			startErr = fmt.Errorf("could not create DJI HID manager")
			return
		}
		dji.match(manager, uintptr(dict))
		dji.manager, dji.toggle, dji.down = manager, toggle, make(map[uintptr]bool)
		dji.register(manager, dji.callback, 0)
		dji.removal(manager, dji.removedCallback, 0)
		dji.schedule(manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
		if result := dji.open(manager, 1); result != 0 {
			dji.unschedule(manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
			dji.close(manager, 0)
			cfRelease(manager)
			dji.manager, dji.toggle = 0, nil
			startErr = fmt.Errorf("DJI receiver exclusive HID access failed: %#x", uint32(result))
			return
		}
		log.Print("trigger: DJI receiver button listener enabled")
	})
	if startErr != nil {
		return nil, startErr
	}
	return func() {
		OnMain(func() {
			if dji.manager == 0 {
				return
			}
			dji.unschedule(dji.manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
			dji.close(dji.manager, 0)
			cfRelease(dji.manager)
			dji.manager, dji.toggle, dji.down = 0, nil, nil
		})
	}, nil
}
