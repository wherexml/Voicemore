//go:build darwin

package macos

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	loadOnce sync.Once
	loadErr  error

	cfRelease func(ref uintptr)
	cfRetain  func(ref uintptr) uintptr
	cfEqual   func(a, b uintptr) bool

	cfMachPortCreateRunLoopSource func(alloc, port uintptr, order int) uintptr
	cfRunLoopGetMain              func() uintptr
	cfRunLoopAddSource            func(loop, source, mode uintptr)
	cfRunLoopRemoveSource         func(loop, source, mode uintptr)
	cfRunLoopCommonModes          uintptr

	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool
	axCreateApplication           func(pid int32) uintptr
	axCopyAttributeValue          func(element, attribute uintptr, value *uintptr) int32
	axIsAttributeSettable         func(element, attribute uintptr, settable *bool) int32
	axSetAttributeValue           func(element, attribute, value uintptr) int32

	cgEventTapCreate            func(tap, place, options uint32, mask uint64, callback, user uintptr) uintptr
	cgEventTapEnable            func(tap uintptr, enable bool)
	cgEventTapIsEnabled         func(tap uintptr) bool
	cgEventGetFlags             func(event uintptr) uint64
	cgEventGetIntegerValueField func(event uintptr, field uint32) int64
	cgEventCreateKeyboardEvent  func(source uintptr, key uint16, down bool) uintptr
	cgEventSetFlags             func(event uintptr, flags uint64)
	cgEventPost                 func(tap uint32, event uintptr)
	cgPreflightListenEvent      func() bool
	cgRequestListenEvent        func() bool
)

const frameworks = "/System/Library/Frameworks/"

// load opens the frameworks once. Every exported function calls it first.
func load() error {
	loadOnce.Do(func() {
		open := func(name string) uintptr {
			lib, err := purego.Dlopen(frameworks+name+".framework/"+name, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil && loadErr == nil {
				loadErr = err
			}
			return lib
		}
		cf := open("CoreFoundation")
		cg := open("CoreGraphics")
		as := open("ApplicationServices")
		open("AppKit")
		open("AVFoundation")
		if loadErr != nil {
			return
		}
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfRetain, cf, "CFRetain")
		purego.RegisterLibFunc(&cfEqual, cf, "CFEqual")
		purego.RegisterLibFunc(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
		purego.RegisterLibFunc(&cfRunLoopGetMain, cf, "CFRunLoopGetMain")
		purego.RegisterLibFunc(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		purego.RegisterLibFunc(&cfRunLoopRemoveSource, cf, "CFRunLoopRemoveSource")
		if p, err := purego.Dlsym(cf, "kCFRunLoopCommonModes"); err == nil {
			cfRunLoopCommonModes = *(*uintptr)(unsafe.Pointer(p))
		} else {
			loadErr = err
			return
		}

		purego.RegisterLibFunc(&axIsProcessTrusted, as, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&axCreateApplication, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axIsAttributeSettable, as, "AXUIElementIsAttributeSettable")
		purego.RegisterLibFunc(&axSetAttributeValue, as, "AXUIElementSetAttributeValue")

		purego.RegisterLibFunc(&cgEventTapCreate, cg, "CGEventTapCreate")
		purego.RegisterLibFunc(&cgEventTapEnable, cg, "CGEventTapEnable")
		purego.RegisterLibFunc(&cgEventTapIsEnabled, cg, "CGEventTapIsEnabled")
		purego.RegisterLibFunc(&cgEventGetFlags, cg, "CGEventGetFlags")
		purego.RegisterLibFunc(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventSetFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cgPreflightListenEvent, cg, "CGPreflightListenEventAccess")
		purego.RegisterLibFunc(&cgRequestListenEvent, cg, "CGRequestListenEventAccess")
	})
	return loadErr
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }
func sel(name string) objc.SEL  { return objc.RegisterName(name) }

// nsString makes an autoreleased NSString, which is a CFString too.
func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	str := class("NSString").Send(sel("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
	runtime.KeepAlive(b)
	return str
}

// goString copies an NSString, or returns "" for nil and other objects.
func goString(str objc.ID) string {
	if str == 0 || !objc.Send[bool](str, sel("isKindOfClass:"), class("NSString")) {
		return ""
	}
	p := objc.Send[uintptr](str, sel("UTF8String"))
	if p == 0 {
		return ""
	}
	n := objc.Send[int](str, sel("lengthOfBytesUsingEncoding:"), 4) // NSUTF8StringEncoding
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
}
