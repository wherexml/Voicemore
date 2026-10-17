//go:build darwin && hotkeyacceptance

package macos

import (
	"github.com/ebitengine/purego"
	"strings"
	"time"
)

// AcceptHotkeyRecovery exercises the real OS tap and production callback.
// Constructed events are delivered directly to our callback: this does not
// claim physical Fn, global OS event routing, microphone or insertion coverage.
func AcceptHotkeyRecovery() []map[string]any {
	results := []map[string]any{}
	check := func(name string, ok bool) { results = append(results, map[string]any{"case": name, "passed": ok}) }
	downs, ups := make(chan bool, 64), make(chan bool, 64)
	// Simulate a lost OS interruption notification while retaining a real tap.
	// This hook exists only in the acceptance build, never the shipped app.
	dropInterruptions := false
	var oldCallback uintptr
	OnMain(func() {
		oldCallback = hotkey.callback
		hotkey.callback = purego.NewCallback(func(proxy, typ, event, user uintptr) uintptr {
			// Physical typing must not contaminate the constructed gestures
			// while the user verifies the installed app at the same time.
			if uint32(typ) == cgEventKeyDown || uint32(typ) == cgEventFlagsChanged {
				return event
			}
			if dropInterruptions && (uint32(typ) == cgEventTapDisabledByTime || uint32(typ) == cgEventTapDisabledByUser) {
				return event
			}
			return hotkeyEvent(proxy, typ, event, user)
		})
	})
	defer func() { OnMain(func() { hotkey.callback = oldCallback }) }()
	start := func() (func(), error) {
		return StartHotkey(KeyFn, func() { downs <- true }, func(combo bool) { ups <- combo }, nil)
	}
	// load before replacing the permission boundary for the denied case.
	if err := load(); err != nil {
		return []map[string]any{{"case": "load frameworks", "passed": false, "error": err.Error()}}
	}
	preflight := cgPreflightListenEvent
	OnMain(func() { cgPreflightListenEvent = func() bool { return false } })
	deniedStop, deniedErr := start()
	check("denied process cannot report a partial tap as success", deniedErr != nil && deniedStop == nil)
	OnMain(func() { cgPreflightListenEvent = preflight })
	stop, err := start()
	if err != nil {
		return append(results, map[string]any{"case": "create native tap", "passed": false, "error": err.Error()})
	}
	check("create native tap", true)
	defer func() { stop() }()
	var setType func(uintptr, uint32)
	cg, _ := purego.Dlopen(frameworks+"CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	purego.RegisterLibFunc(&setType, cg, "CGEventSetType")
	event := func(code uint16, down bool, typ uint32) {
		OnMain(func() {
			ev := cgEventCreateKeyboardEvent(0, code, down)
			defer cfRelease(ev)
			setType(ev, typ)
			flags := uint64(0)
			if down {
				flags = modifierKeys[KeyFn].flag
			}
			cgEventSetFlags(ev, flags)
			hotkeyEvent(0, uintptr(typ), ev, 0)
		})
	}
	take := func(ch chan bool, want bool) bool {
		select {
		case got := <-ch:
			return got == want
		case <-time.After(time.Second):
			return false
		}
	}
	empty := func(ch chan bool) bool {
		select {
		case <-ch:
			return false
		case <-time.After(50 * time.Millisecond):
			return true
		}
	}
	enabled := func() bool {
		ok := false
		OnMain(func() { ok = hotkey.tap != 0 && cgEventTapIsEnabled(hotkey.tap) })
		return ok
	}
	disable := func() { OnMain(func() { dropInterruptions = true; cgEventTapEnable(hotkey.tap, false) }) }
	waitRecovery := func() bool {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if enabled() {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	check("tap initially enabled", enabled())
	disable()
	check("real OS tap disabled without callback", !enabled())
	check("watchdog restores real OS tap", waitRecovery())
	OnMain(func() { cgPreflightListenEvent = func() bool { return false } })
	disable()
	time.Sleep(650 * time.Millisecond)
	check("revoked permission leaves tap disabled", !enabled())
	OnMain(func() { cgPreflightListenEvent = preflight })
	check("restored permission recovers existing tap", waitRecovery())
	event(63, false, cgEventFlagsChanged)
	check("orphan release does not toggle", empty(ups))
	ok := true
	for i := 0; i < 10; i++ {
		event(63, true, cgEventFlagsChanged)
		ok = take(downs, true) && ok
		event(63, false, cgEventFlagsChanged)
		ok = take(ups, false) && ok
	}
	check("ten complete gestures after recovery", ok)
	event(63, true, cgEventFlagsChanged)
	take(downs, true)
	disable()
	ok = waitRecovery() && take(ups, true)
	event(63, false, cgEventFlagsChanged)
	check("interrupted hold finishes once and orphan ignored", ok && empty(ups))
	event(63, true, cgEventFlagsChanged)
	take(downs, true)
	event(51, true, cgEventKeyDown)
	event(63, false, cgEventFlagsChanged)
	check("Fn Delete remains a combination", take(ups, true))
	event(63, true, cgEventFlagsChanged)
	take(downs, true)
	time.Sleep(650 * time.Millisecond)
	event(63, false, cgEventFlagsChanged)
	check("healthy watchdog preserves held gesture", take(ups, false) && empty(ups))
	event(63, true, cgEventFlagsChanged)
	take(downs, true)
	OnMain(func() { hotkeyEvent(0, cgEventTapDisabledByTime, 0, 0) })
	ok = take(ups, true)
	time.Sleep(650 * time.Millisecond)
	check("callback recovery not repeated by watchdog", ok && enabled() && empty(ups))
	stop()
	time.Sleep(650 * time.Millisecond)
	var stopped bool
	OnMain(func() { stopped = hotkey.tap == 0 && hotkey.watchdog == nil })
	check("stop cancels watchdog", stopped)
	stop, err = start()
	if err != nil {
		check("rebind", false)
		return results
	}
	event(63, true, cgEventFlagsChanged)
	take(downs, true)
	time.Sleep(650 * time.Millisecond)
	event(63, false, cgEventFlagsChanged)
	check("rebind preserves new gesture", take(ups, false) && enabled() && empty(ups))
	stop()
	// Real taps, production callbacks, constructed gestures: no system-wide posts.
	chordEvent := func(code uint16, typ uint32, flags uint64) {
		OnMain(func() {
			ev := cgEventCreateKeyboardEvent(0, code, typ != 11)
			defer cfRelease(ev)
			setType(ev, typ)
			cgEventSetFlags(ev, flags)
			hotkeyEvent(0, uintptr(typ), ev, 0)
		})
	}
	for _, chord := range []string{"Ctrl+Alt", "LeftControl+LeftOption", "Fn+K", "Fn+Ctrl+K"} {
		stop, err = StartHotkey(chord, func() { downs <- true }, func(combo bool) { ups <- combo }, nil)
		if err != nil {
			check(chord+" native registration", false)
			continue
		}
		flags := uint64(0x40000 | 0x80000)
		if strings.HasPrefix(chord, "Fn+") {
			flags = 0x800000
			if strings.Contains(chord, "Ctrl") {
				flags |= 0x40000
			}
		}
		chordEvent(59, cgEventFlagsChanged, 0x40000)
		ok = empty(downs)
		if strings.HasPrefix(chord, "Fn+") {
			chordEvent(63, cgEventFlagsChanged, flags)
			ok = empty(downs) && ok
			chordEvent(40, cgEventKeyDown, flags)
			ok = take(downs, true) && ok
			chordEvent(40, cgEventKeyDown, flags)
			ok = empty(downs) && ok
			chordEvent(40, 11, flags)
		} else {
			chordEvent(58, cgEventFlagsChanged, flags)
			ok = take(downs, true) && ok
			chordEvent(58, cgEventFlagsChanged, flags)
			ok = empty(downs) && ok
			chordEvent(58, cgEventFlagsChanged, 0x40000)
		}
		ok = take(ups, false) && ok
		chordEvent(59, cgEventFlagsChanged, 0)
		check(chord+" partial, repeat, complete and release", ok && empty(ups))
		// An extra key cancels a modifier-only gesture instead of toggling.
		if !strings.HasPrefix(chord, "Fn+") {
			chordEvent(58, cgEventFlagsChanged, flags)
			take(downs, true)
			chordEvent(0, cgEventKeyDown, flags)
			chordEvent(58, cgEventFlagsChanged, 0)
			check(chord+" with extra key does not toggle", take(ups, true))
		}
		stop()
	}
	return results
}
