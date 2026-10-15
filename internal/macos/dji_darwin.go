//go:build darwin

package macos

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const djiDoubleClickWindow = 350 * time.Millisecond

type djiClick struct {
	timer    *time.Timer
	deadline time.Time
	cooldown time.Time
	pid      int32
}

// All click state is confined to the main run loop, including timer completion.
func cancelDJIClick(device uintptr) {
	if click := dji.clicks[device]; click != nil && click.timer != nil {
		click.timer.Stop()
	}
	delete(dji.clicks, device)
}

func djiPress(device uintptr) {
	now := time.Now()
	click := dji.clicks[device]
	if click != nil && now.Before(click.cooldown) {
		return
	}
	if click != nil && click.timer != nil {
		click.timer.Stop()
		click.timer = nil
		if now.Before(click.deadline) && click.pid == frontmostPID() {
			click.cooldown = now.Add(djiDoubleClickWindow)
			log.Print("trigger: DJI double click -> Enter")
			// Explicit plain Return, sent to the currently focused application.
			if !axIsProcessTrusted() {
				log.Print("trigger: DJI Enter skipped: accessibility unavailable")
				return
			}
			press := cgEventCreateKeyboardEvent(0, 36, true)
			release := cgEventCreateKeyboardEvent(0, 36, false)
			if press != 0 && release != 0 {
				cgEventSetFlags(press, 0)
				cgEventSetFlags(release, 0)
				cgEventPost(0, press)
				cgEventPost(0, release)
			}
			if press != 0 {
				cfRelease(press)
			}
			if release != 0 {
				cfRelease(release)
			}
			return
		}
		// An overdue timer can be queued behind this HID callback.
		if click.pid == frontmostPID() && dji.toggle != nil {
			go dji.toggle()
		}
	}
	click = &djiClick{deadline: now.Add(djiDoubleClickWindow), pid: frontmostPID()}
	dji.clicks[device] = click
	click.timer = time.AfterFunc(djiDoubleClickWindow, func() {
		OnMain(func() {
			if dji.manager == 0 || dji.clicks[device] != click || click.timer == nil {
				return
			}
			click.timer = nil
			if click.pid != frontmostPID() {
				log.Print("trigger: DJI single click cancelled: focus changed")
				return
			}
			if dji.toggle != nil {
				log.Print("trigger: DJI single click -> toggle")
				go dji.toggle()
			}
		})
	})
}

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
	clicks          map[uintptr]*djiClick
	toggle          func()
}

// StartDJIButton maps single clicks to recording and double clicks to Return.
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
		dji.removedCallback = purego.NewCallback(func(context uintptr, result int32, sender, device uintptr) {
			delete(dji.down, device)
			cancelDJIClick(device)
		})
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
				djiPress(device)
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
		dji.clicks = make(map[uintptr]*djiClick)
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
			for device := range dji.clicks {
				cancelDJIClick(device)
			}
			dji.unschedule(dji.manager, cfRunLoopGetMain(), cfRunLoopCommonModes)
			dji.close(dji.manager, 0)
			cfRelease(dji.manager)
			dji.manager, dji.toggle, dji.down = 0, nil, nil
		})
	}, nil
}
