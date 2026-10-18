//go:build darwin

package macos

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type dragPoint struct{ X, Y float64 }
type dragSize struct{ W, H float64 }
type dragRect struct {
	P dragPoint
	S dragSize
}

type fileDragSource struct {
	view            objc.ID
	frame           dragRect
	path            string
	down            objc.ID
	press           dragPoint
	active, enabled bool
	start           func()
	done            func(bool)
}

var fileDrags = map[uintptr]*fileDragSource{}
var fileDragViews = map[objc.ID]*fileDragSource{}
var fileDragSetup struct {
	sync.Once
	err       error
	cls       objc.Class
	init      func(objc.ID, objc.SEL, dragRect) objc.ID
	frame     func(objc.ID, objc.SEL, dragRect)
	bounds    func(objc.ID, objc.SEL) dragRect
	point     func(objc.ID, objc.SEL) dragPoint
	itemFrame func(objc.ID, objc.SEL, dragRect, objc.ID)
}

// SetAppFileDrag installs a transparent AppKit drag source over the drawn app
// card. Start in mouseDragged itself, with an eager NSURL pasteboard writer:
// no queued native start or cross-process lazy file-data callback is involved.
// Call on the main thread. Coordinates are content DIPs from the top left.
func SetAppFileDrag(handle uintptr, path string, x, y, w, h float64, start func(), done func(bool)) error {
	if err := load(); err != nil {
		return err
	}
	fileDragSetup.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			fileDragSetup.err = err
			return
		}
		addr, err := purego.Dlsym(lib, "objc_msgSend")
		if err != nil {
			fileDragSetup.err = err
			return
		}
		purego.RegisterFunc(&fileDragSetup.init, addr)
		purego.RegisterFunc(&fileDragSetup.frame, addr)
		purego.RegisterFunc(&fileDragSetup.bounds, addr)
		purego.RegisterFunc(&fileDragSetup.point, addr)
		purego.RegisterFunc(&fileDragSetup.itemFrame, addr)
		method := func(name string, fn any) objc.MethodDef { return objc.MethodDef{Cmd: sel(name), Fn: fn} }
		fileDragSetup.cls, fileDragSetup.err = objc.RegisterClass("VoicemoreAppFileDragView", objc.GetClass("NSView"), []*objc.Protocol{objc.GetProtocol("NSDraggingSource")}, nil, []objc.MethodDef{
			method("isFlipped", func(self objc.ID, _ objc.SEL) bool { return true }),
			method("mouseDownCanMoveWindow", func(self objc.ID, _ objc.SEL) bool { return false }),
			method("acceptsFirstMouse:", func(self objc.ID, _ objc.SEL, event objc.ID) bool { return true }),
			method("shouldDelayWindowOrderingForEvent:", func(self objc.ID, _ objc.SEL, event objc.ID) bool { return true }),
			method("mouseDown:", func(self objc.ID, _ objc.SEL, event objc.ID) {
				s := fileDragViews[self]
				if s == nil || !s.enabled {
					return
				}
				if s.down != 0 {
					s.down.Send(sel("release"))
				}
				s.down = event.Send(sel("retain"))
				s.press = fileDragSetup.point(event, sel("locationInWindow"))
			}),
			method("mouseUp:", func(self objc.ID, _ objc.SEL, event objc.ID) {
				if s := fileDragViews[self]; s != nil && s.down != 0 {
					s.down.Send(sel("release"))
					s.down = 0
				}
			}),
			method("mouseDragged:", func(self objc.ID, _ objc.SEL, event objc.ID) {
				s := fileDragViews[self]
				if s == nil || !s.enabled || s.active || s.down == 0 || !PrimaryMouseDown() {
					return
				}
				point := fileDragSetup.point(event, sel("locationInWindow"))
				dx, dy := point.X-s.press.X, point.Y-s.press.Y
				if dx*dx+dy*dy < 16 {
					return
				}
				url := class("NSURL").Send(sel("fileURLWithPath:"), nsString(s.path))
				item := class("NSDraggingItem").Send(sel("alloc")).Send(sel("initWithPasteboardWriter:"), url)
				defer item.Send(sel("release"))
				image := class("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("iconForFile:"), nsString(s.path))
				fileDragSetup.itemFrame(item, sel("setDraggingFrame:contents:"), dragRect{dragPoint{8, 4}, dragSize{30, 30}}, image)
				items := class("NSArray").Send(sel("arrayWithObject:"), item)
				s.active = true
				if s.start != nil {
					s.start()
				}
				session := self.Send(sel("beginDraggingSessionWithItems:event:source:"), items, s.down, self)
				if session == 0 && s.active {
					s.finish(false)
				}
			}),
			method("draggingSession:sourceOperationMaskForDraggingContext:", func(self objc.ID, _ objc.SEL, session objc.ID, context int) uint {
				if s := fileDragViews[self]; s != nil && s.enabled {
					return 1
				}
				return 0
			}),
			method("ignoreModifierKeysForDraggingSession:", func(self objc.ID, _ objc.SEL, session objc.ID) bool { return true }),
			method("draggingSession:endedAtPoint:operation:", func(self objc.ID, _ objc.SEL, session objc.ID, point dragPoint, operation uint) {
				if s := fileDragViews[self]; s != nil {
					s.finish(operation == 1)
				}
			}),
		})
	})
	if fileDragSetup.err != nil {
		return fileDragSetup.err
	}
	if handle == 0 || path == "" {
		return errors.New("app file drag needs a window and path")
	}
	parent := objc.ID(handle).Send(sel("contentView"))
	frame := dragRect{dragPoint{x, y}, dragSize{w, h}}
	if !objc.Send[bool](parent, sel("isFlipped")) {
		frame.P.Y = fileDragSetup.bounds(parent, sel("bounds")).S.H - y - h
	}
	s := fileDrags[handle]
	if s == nil {
		view := fileDragSetup.init(objc.ID(fileDragSetup.cls).Send(sel("alloc")), sel("initWithFrame:"), frame)
		s = &fileDragSource{view: view, frame: frame}
		fileDrags[handle] = s
		fileDragViews[view] = s
		parent.Send(sel("addSubview:"), view)
		view.Send(sel("release")) // parent owns the view for this panel's lifetime
	}
	if !s.active {
		wasEnabled := s.enabled
		s.path, s.enabled, s.start, s.done = path, true, start, done
		if s.frame != frame {
			fileDragSetup.frame(s.view, sel("setFrame:"), frame)
			s.frame = frame
		}
		if !wasEnabled {
			s.view.Send(sel("setHidden:"), false)
		}
	}
	return nil
}
func (s *fileDragSource) finish(copied bool) {
	if !s.active {
		return
	}
	s.active = false
	if s.down != 0 {
		s.down.Send(sel("release"))
		s.down = 0
	}
	if s.done != nil {
		s.done(copied)
	}
}

// HideAppFileDrag prevents a hidden/previous permission card from starting or
// accepting more work. AppKit retains control of an in-flight gesture until up.
func HideAppFileDrag(handle uintptr) {
	if s := fileDrags[handle]; s != nil {
		if s.enabled {
			s.enabled = false
			s.view.Send(sel("setHidden:"), true)
		}
	}
}
