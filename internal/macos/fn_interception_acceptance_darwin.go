//go:build darwin && hotkeyacceptance

package macos

import (
	"fmt"
	"github.com/ebitengine/purego"
	"reflect"
	"sync"
	"time"
)

// AcceptFnInterception traverses real Quartz taps. A tagged inert carrier is
// converted only AFTER other apps' HID head listeners, and a downstream sink
// consumes every tagged output. No test key reaches another application's UI.
// This is synthetic OS routing, not physical keyboard/input-source acceptance.
func AcceptFnInterception() []map[string]any {
	results := []map[string]any{}
	beforePreference := FnKeyConflict()
	check := func(name string, ok bool, detail any) {
		results = append(results, map[string]any{"case": name, "passed": ok, "detail": detail})
	}
	const tag int64 = 0x564d464e
	type edge struct {
		Type  uint32
		Code  int64
		Flags uint64
	}
	var mu sync.Mutex
	var desired edge
	var seen []edge
	barrier := make(chan bool, 1)
	var setType func(uintptr, uint32)
	var setInteger func(uintptr, uint32, int64)
	cg, _ := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	purego.RegisterLibFunc(&setType, cg, "CGEventSetType")
	purego.RegisterLibFunc(&setInteger, cg, "CGEventSetIntegerValueField")
	injectorCallback := purego.NewCallback(func(proxy, typ, event, user uintptr) uintptr {
		if typ != 5 || cgEventGetIntegerValueField(event, 42) != tag {
			return event
		}
		mu.Lock()
		e := desired
		mu.Unlock()
		setType(event, e.Type)
		setInteger(event, 9, e.Code)
		cgEventSetFlags(event, e.Flags)
		hotkey.event(proxy, uintptr(e.Type), event, 0)
		translationHotkey.event(proxy, uintptr(e.Type), event, 0)
		// A nil return plus tapPost forces Quartz to re-evaluate the new event type.
		fnFilter.postEvent(proxy, event)
		barrier <- true
		return 0
	})
	sinkCallback := purego.NewCallback(func(proxy, typ, event, user uintptr) uintptr {
		if event == 0 || cgEventGetIntegerValueField(event, 42) != tag {
			return event
		}
		mu.Lock()
		seen = append(seen, edge{uint32(typ), cgEventGetIntegerValueField(event, 9), cgEventGetFlags(event)})
		mu.Unlock()
		return 0
	})
	var injector, injectorSource, sink, sinkSource uintptr
	OnMain(func() {
		injector = cgEventTapCreate(cgHIDEventTap, 1, 0, 1<<5, injectorCallback, 0)
		if injector != 0 {
			injectorSource = cfMachPortCreateRunLoopSource(0, injector, 0)
			cfRunLoopAddSource(cfRunLoopGetMain(), injectorSource, cfRunLoopCommonModes)
			cgEventTapEnable(injector, true)
		}
	})
	if injector == 0 {
		check("routing injector", false, "permission denied")
		return results
	}
	defer func() {
		OnMain(func() {
			for _, p := range [][2]uintptr{{sink, sinkSource}, {injector, injectorSource}} {
				if p[0] != 0 {
					cgEventTapEnable(p[0], false)
					cfRunLoopRemoveSource(cfRunLoopGetMain(), p[1], cfRunLoopCommonModes)
					if fnFilter.invalidate != nil {
						fnFilter.invalidate(p[0])
					}
					cfRelease(p[1])
					cfRelease(p[0])
				}
			}
		})
	}()
	ups := make(chan bool, 64)
	stop, err := StartHotkey(KeyFn, nil, func(combo bool) { ups <- combo }, nil)
	if err != nil {
		check("active Fn registration", false, err.Error())
		return results
	}
	defer stop()
	translations := make(chan bool, 16)
	stopTranslation, err := StartTranslationHotkey("Fn+K", func(combo bool) { translations <- combo })
	if err != nil {
		check("translation registration", false, err.Error())
		return results
	}
	defer stopTranslation()
	OnMain(func() {
		sink = cgEventTapCreate(cgHIDEventTap, 1, 0, 1<<10|1<<11|1<<12|1<<14|1<<1|1<<3|1<<25|1<<22, sinkCallback, 0)
		if sink != 0 {
			sinkSource = cfMachPortCreateRunLoopSource(0, sink, 0)
			cfRunLoopAddSource(cfRunLoopGetMain(), sinkSource, cfRunLoopCommonModes)
			cgEventTapEnable(sink, true)
		}
	})
	if sink == 0 {
		check("routing safety sink", false, "could not create")
		return results
	}
	send := func(e edge) {
		mu.Lock()
		desired = e
		mu.Unlock()
		OnMain(func() {
			ev := cgEventCreateKeyboardEvent(0, 255, true)
			setType(ev, 5)
			setInteger(ev, 42, tag)
			cgEventPost(cgHIDEventTap, ev)
			cfRelease(ev)
		})
		select {
		case <-barrier:
		case <-time.After(2 * time.Second):
			check("native event delivery barrier", false, e)
		}
		time.Sleep(150 * time.Millisecond)
	}
	run := func(name string, input, want []edge, wantCombo bool) {
		mu.Lock()
		seen = nil
		mu.Unlock()
		for _, e := range input {
			send(e)
		}
		mu.Lock()
		got := append([]edge(nil), seen...)
		mu.Unlock()
		ok := reflect.DeepEqual(got, want)
		select {
		case combo := <-ups:
			ok = ok && combo == wantCombo
		case <-time.After(time.Second):
			ok = false
		}
		select {
		case <-ups:
			ok = false
		default:
		}
		check(name, ok, map[string]any{"received": got, "expected": want})
	}
	down, up := edge{12, 63, 0x800000}, edge{12, 63, 0}
	for i := 0; i < 10; i++ {
		run(fmt.Sprintf("standalone Fn gesture %d suppressed once", i+1), []edge{down, up}, nil, false)
	}
	for _, code := range []int64{117, 123, 126, 122, 40} {
		kd, ku := edge{10, code, 0x800000}, edge{11, code, 0x800000}
		run(fmt.Sprintf("Fn chord key %d preserves edges and flags", code), []edge{down, kd, ku, up}, []edge{down, kd, ku, up}, true)
	}
	select {
	case combo := <-translations:
		check("Fn K translation survives interception", !combo, nil)
	case <-time.After(time.Second):
		check("Fn K translation survives interception", false, "no callback")
	}
	ctrl := edge{12, 59, 0x40000}
	fnCtrl := edge{12, 63, 0x840000}
	releaseFn := edge{12, 63, 0x40000}
	releaseCtrl := edge{12, 59, 0}
	run("modifier before Fn passes unchanged", []edge{ctrl, fnCtrl, releaseFn, releaseCtrl}, []edge{ctrl, fnCtrl, releaseFn, releaseCtrl}, true)
	run("modifier after Fn replays down before chord", []edge{down, edge{12, 59, 0x840000}, edge{12, 59, 0x800000}, up}, []edge{down, edge{12, 59, 0x840000}, edge{12, 59, 0x800000}, up}, true)
	run("Fn release before terminal release", []edge{down, edge{10, 117, 0x800000}, up, edge{11, 117, 0}}, []edge{down, edge{10, 117, 0x800000}, up, edge{11, 117, 0}}, true)
	run("repeat Fn edge does not duplicate toggle", []edge{down, down, up}, nil, false)

	for _, kind := range []uint32{1, 3, 25, 14, 22} {
		action := edge{kind, 0, 0x800000}
		run(fmt.Sprintf("Fn pointer or media %d preserves gesture", kind), []edge{down, action, up}, []edge{down, action, up}, true)
	}
	// Disable the real modifying tap with a buffered down. Neither recovery
	// nor the orphan release may toggle, and the next complete press works.
	send(down)
	OnMain(func() { cgEventTapEnable(fnFilter.tap, false) })
	time.Sleep(650 * time.Millisecond)
	recovered := false
	OnMain(func() { recovered = cgEventTapIsEnabled(fnFilter.tap) && fnFilter.pending == 0 })
	select {
	case combo := <-ups:
		recovered = recovered && combo
	case <-time.After(time.Second):
		recovered = false
	}
	send(up)
	select {
	case <-ups:
		recovered = false
	default:
	}
	check("active filter watchdog clears interrupted gesture", recovered, nil)
	run("complete Fn after filter recovery", []edge{down, up}, nil, false)
	check("system Fn preference unchanged", FnKeyConflict() == beforePreference, nil)
	stop()
	stopTranslation()
	var released bool
	OnMain(func() { released = fnFilter.tap == 0 && fnFilter.pending == 0 && fnFilter.watchdog == nil })
	check("unbind releases interception and buffered events", released, nil)
	return results
}
