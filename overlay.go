package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/session"
)

// The overlay is the black capsule of docs/images/recording-overlay-reference.png,
// at the size mock/index.html settled on: a 144×40 pill with a cross on the
// left, dots in the middle and a white check on the right.
//
// It is a flyout hung from the tray icon, which MyGo makes a borderless
// nonactivating panel above other apps' windows: it shows and takes clicks
// without activating this app, so the target input keeps the keyboard. It
// is placed at the bottom of the display the recording started on, not
// next to the icon.

const (
	capsuleW, capsuleH = 144, 40
	circle             = 30 // the cancel and confirm circles
	hitW               = 36 // their hit targets, as high as the pill
	overlayW           = 300
	compactH           = capsuleH + 8
	panelH             = 124
)

var (
	white     = ui.RGB(255, 255, 255)
	black     = ui.RGB(0, 0, 0)
	dim       = ui.RGBA(255, 255, 255, 0.45)
	buttonBg  = ui.RGB(58, 58, 60)
	errorText = ui.RGB(255, 140, 130)
)

type overlay struct {
	a                *app
	win              *mygo.Window
	v                session.View
	expanded         bool           // the transcript preview is open
	area             mygo.Rectangle // the work area the dictation started on
	height           int
	closing          bool
	fadeAt           time.Time
	fadeFrom, fadeTo float32
	fadeID           uint64
	levels           [session.Levels]float32
	levelAt          time.Time
}

func newOverlay(a *app) *overlay {
	o := &overlay{a: a}
	o.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:    a.tray,
		Width:   overlayW,
		Height:  compactH,
		Hidden:  true,
		Content: ui.View(o.view),
	})
	o.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
		}
	})
	return o
}

// sync shows the controller's state, from any goroutine.
func (o *overlay) sync() {
	v := o.a.ctrl.View()
	o.win.Update(func() {
		was := o.v.Phase
		if v.Phase == session.Idle {
			if o.closing || !o.win.IsVisible() {
				return
			}
			o.closing = true
			o.fade(0)
			id := o.fadeID
			go func() {
				time.Sleep(180 * time.Millisecond)
				o.win.Update(func() {
					if o.closing && o.fadeID == id {
						o.win.Hide()
						o.v = session.View{}
						o.expanded = false
						o.closing = false
					}
				})
			}()
			return
		}
		o.v = v
		if was == session.Idle || o.closing {
			o.closing = false
			o.fade(1)
			o.levels = [session.Levels]float32{}
			o.levelAt = time.Time{}
			// The display the pointer is on stands for the one the
			// user is typing on; the overlay stays there until the
			// dictation ends.
			o.area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
			o.height = 0
		}
		o.place()
		if !o.win.IsVisible() {
			o.win.ShowInactive()
		}
	})
}

// Retarget from the currently displayed opacity, so a rapid restart never jumps.
func (o *overlay) opacity(now time.Time) float32 {
	t := min(1, max(0, float32(now.Sub(o.fadeAt).Seconds()/0.18)))
	t = 1 - (1-t)*(1-t)*(1-t)
	return o.fadeFrom + (o.fadeTo-o.fadeFrom)*t
}
func (o *overlay) fade(to float32) {
	now := time.Now()
	o.fadeFrom, o.fadeTo = o.opacity(now), to
	o.fadeAt = now
	o.fadeID++
}

// place sizes the window for what it shows and keeps its bottom edge still.
func (o *overlay) place() {
	h := compactH
	if o.panel() {
		h += panelH + 8
	}
	if h == o.height {
		return
	}
	o.height = h
	offset := max(0, o.a.settings.Get().OverlayBottomOffset)
	o.win.SetBounds(mygo.Rectangle{
		X:      o.area.X + (o.area.Width-overlayW)/2,
		Y:      o.area.Y + o.area.Height - h - offset,
		Width:  overlayW,
		Height: h,
	})
}

// panel says the box above the capsule shows: the preview the user opened,
// or the message of a failure or of a dictation that inserted nothing.
func (o *overlay) panel() bool {
	return o.expanded || o.v.Phase == session.Failed || (o.v.Phase == session.Done && o.v.Message != "")
}

func (o *overlay) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	v := o.v
	ctrl := o.a.ctrl
	alpha := o.opacity(c.Now())
	if c.Preferences().ReduceMotion {
		alpha = o.fadeTo
	}
	if c.Now().Sub(o.fadeAt) < 180*time.Millisecond {
		c.After(16 * time.Millisecond)
	}
	ui.Column(c).Fill().Justify(ui.End).AlignItems(ui.Center).Gap(6).Opacity(alpha).Children(func() {
		if o.panel() {
			o.panelView(c)
		}
		ui.Row(c).Size(capsuleW, capsuleH).Scale(0.96+0.04*alpha, 0.96+0.04*alpha).Radius(capsuleH/2).Background(black).
			Border(0.5, ui.RGBA(255, 255, 255, 0.22)).Padding(0, 2).Gap(2).Children(func() {
			// Cancel: nothing is inserted, whatever arrives later. The
			// circle is drawn inside a taller, wider box that takes the
			// click, so the small control stays easy to hit.
			if ui.Box(c.Key("cancel")).Size(hitW, capsuleH).
				Label(T("ovCancel")).Tooltip(T("ovCancel")).Cursor(ui.CursorPointer).Draw(drawCross).Clicked() {
				if !o.closing {
					go ctrl.Cancel()
				}
			}

			if v.Phase == session.Preparing {
				ui.Text(c, T("stPreparing")).FontSize(10).TextColor(dim).Grow(1).MaxLines(1)
			} else {
				if ui.Box(c.Key("dots")).Grow(1).Height(capsuleH).Label(T("ovPreview")).
					Cursor(ui.CursorPointer).Draw(func(p *ui.Painter, r ui.Rect) { o.drawDots(c, p, r) }).Clicked() {
					// After this frame: resizing the window is not part of
					// building it.
					go o.win.Update(func() {
						o.expanded = !o.expanded
						o.place()
					})
				}

			}

			// Confirm ends the recording once; while the dictation is
			// finishing it does nothing more.
			canConfirm := !o.closing && v.Phase == session.Recording
			confirm := ui.Box(c.Key("confirm")).Size(hitW, capsuleH).
				Label(T("ovConfirm")).Tooltip(T("ovConfirm")).Draw(drawCheck)
			if !canConfirm {
				confirm.Opacity(0.35)
			} else if confirm.Cursor(ui.CursorPointer).Clicked() {
				go ctrl.Confirm()
			}
		})
	})

	// The phases that wait animate, unless the system asks for less
	// motion; recording levels interpolate between audio callbacks.
	switch v.Phase {
	case session.Recording, session.Preparing, session.Finalizing, session.Cleaning, session.Inserting:
		if !c.Preferences().ReduceMotion {
			c.After(16 * time.Millisecond)
		}
	}
}

func (o *overlay) panelView(c *ui.Context) {
	v := o.v
	ctrl := o.a.ctrl
	ui.Column(c).Width(overlayW).Height(panelH).Radius(12).Background(ui.RGBA(0, 0, 0, 0.92)).
		Border(1, ui.RGBA(255, 255, 255, 0.2)).Padding(9, 12).Gap(5).Children(func() {
		switch {
		case v.Phase == session.Failed:
			ui.Text(c, v.Message).FontSize(12).TextColor(errorText).MaxLines(3)
			if v.Text != "" {
				ui.Text(c, tail(v.Text, 60)).FontSize(12).TextColor(white).MaxLines(2)
			}
			ui.Spacer(c)
			ui.Row(c).Gap(8).Children(func() {
				if v.CleanupFailed {
					if ui.Button(c.Key("retry"), T("retryClean")).Clicked() {
						go ctrl.RetryCleanup()
					}
					if ui.Button(c.Key("raw"), T("insertRaw")).Clicked() {
						go ctrl.InsertOriginal()
					}
				}
				if v.Text != "" && ui.Button(c.Key("copy"), T("copyText")).Clicked() {
					go ctrl.CopyFailed()
				}
				ui.Spacer(c)
				if ui.Button(c.Key("close"), T("close")).Clicked() {
					go ctrl.Dismiss()
				}
			})
		case v.Phase == session.Done:
			ui.Text(c, v.Message).FontSize(13).TextColor(white)
		default:
			// Final text is white; what may still change is dimmed and
			// marked as such.
			if v.Final == "" && v.Pending == "" {
				ui.Text(c, T("noText")).FontSize(12).TextColor(dim)
			}
			if v.Final != "" {
				ui.Text(c, tail(v.Final, 90)).FontSize(13).TextColor(white).MaxLines(4)
			}
			if v.Pending != "" {
				ui.Text(c, T("pendingMark", tail(v.Pending, 60))).FontSize(13).TextColor(dim).Italic().MaxLines(2)
			}
			ui.Spacer(c)
			if v.Cleanup {
				ui.Text(c, T("willClean")).FontSize(11).TextColor(dim)
			}
		}
	})
}

// tail returns the end of a text, where the newest words are.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// disc fills the 30 px circle in the middle of a button's hit target.
func disc(p *ui.Painter, r ui.Rect, col ui.Color) (cx, cy float32) {
	cx, cy = r.X+r.W/2, r.Y+r.H/2
	p.Fill(ui.Rect{X: cx - circle/2, Y: cy - circle/2, W: circle, H: circle}, col, circle/2)
	return cx, cy
}

// drawCross strokes the X as a path of two diagonals. Painter.Line only
// draws horizontal and vertical lines: given a diagonal it fills a flat
// bar, which is how the X once showed as a dash.
func drawCross(p *ui.Painter, r ui.Rect) {
	cx, cy := disc(p, r, buttonBg)
	const d = 3
	var x ui.Path
	x.MoveTo(cx-d, cy-d).LineTo(cx+d, cy+d).MoveTo(cx+d, cy-d).LineTo(cx-d, cy+d)
	p.StrokePath(&x, 1.6, white)
}

func drawCheck(p *ui.Painter, r ui.Rect) {
	cx, cy := disc(p, r, white)
	// The mock's check: 12 px wide, the same weight as the cross.
	check := (&ui.Path{}).MoveTo(cx-3.4, cy+0.4).LineTo(cx-1.1, cy+2.7).LineTo(cx+3.4, cy-2.5)
	p.StrokePath(check, 1.6, black)
}

// drawDots paints the middle of the capsule for the current phase.
//
//   - Recording: a dot per recent level; the dots of levels with sound grow
//     into bars of that level, and silence leaves small still dots. Nothing
//     moves unless the microphone heard something.
//   - Preparing: dim dots, one lit after the other.
//   - Finalizing: a wave running through dots of one size.
//   - Cleaning: pairs of dots pulsing from the middle outward.
//   - Inserting, Done, Failed: still dots, a check mark, or a dash.
func (o *overlay) drawDots(c *ui.Context, p *ui.Painter, r ui.Rect) {
	const n = session.Levels
	still := c.Preferences().ReduceMotion
	t := float64(c.Now().UnixMilli()) / 1000
	// As in the mock: the first and last dots sit 9 px from the circles,
	// and the others are spread evenly between them.
	left, span := r.X+5+1.25, r.W-10-2.5
	cy := r.Y + r.H/2
	dot := func(i int, w, h float32, col ui.Color, dy float32) {
		x := left + span*float32(i)/(n-1)
		p.Fill(ui.Rect{X: x - w/2, Y: cy - h/2 + dy, W: w, H: h}, col, w/2)
	}
	switch o.v.Phase {
	case session.Recording:
		dt := c.Now().Sub(o.levelAt).Seconds()
		o.levelAt = c.Now()
		for i, target := range o.v.Levels {
			if !session.Voiced(target) {
				target = 0
			}
			tau := 0.09
			if target > o.levels[i] {
				tau = 0.035
			}
			blend := float32(1 - math.Exp(-min(dt, 0.05)/tau))
			if still {
				blend = 1
			}
			o.levels[i] += (target - o.levels[i]) * blend
			l := o.levels[i]
			if l > 0.005 {
				dot(i, 3, 3+l*15, white, 0)
			} else {
				dot(i, 2.5, 2.5, ui.RGBA(255, 255, 255, 0.65), 0)
			}
		}
	case session.Preparing:
		lit := -1
		if !still {
			lit = int(t*8) % n
		}
		for i := range n {
			col := ui.RGBA(255, 255, 255, 0.3)
			if i == lit {
				col = white
			}
			dot(i, 2.5, 2.5, col, 0)
		}
	case session.Finalizing:
		for i := range n {
			dy := float32(0)
			if !still {
				dy = float32(3 * math.Sin(t*7-float64(i)*0.8))
			}
			dot(i, 3, 3, white, dy)
		}
	case session.Cleaning:
		for i := range n {
			size := float32(3)
			if !still {
				d := math.Abs(float64(i - n/2))
				size = float32(2 + 2.5*(0.5+0.5*math.Sin(t*5-d*1.1)))
			}
			dot(i, size, size, ui.RGB(160, 200, 255), 0)
		}
	case session.Inserting:
		for i := range n {
			dot(i, 2.5, 2.5, white, 0)
		}
	case session.Done:
		cx := r.X + r.W/2
		if o.v.Message == "" {
			check := (&ui.Path{}).MoveTo(cx-5, cy).LineTo(cx-1.5, cy+3.5).LineTo(cx+5, cy-3.8)
			p.StrokePath(check, 1.8, ui.RGB(120, 220, 140))
		} else {
			p.Line(cx-5, cy, cx+5, cy, 1.8, dim)
		}
	case session.Failed:
		cx := r.X + r.W/2
		p.Line(cx, cy-6, cx, cy+1.5, 2, errorText)
		p.Fill(ui.Rect{X: cx - 1.2, Y: cy + 4.5, W: 2.4, H: 2.4}, errorText, 1.2)
	}
}
