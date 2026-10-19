package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/session"
)

// The overlay is the black capsule of docs/images/recording-overlay-reference.png,
// at the size mock/index.html settled on: a 144×40 pill with a cross on the
// left, dots in the middle and a white check on the right.
//
// It is a flyout hung from the tray icon, which MyGo makes a borderless
// nonactivating panel above other apps' windows: it shows and takes clicks
// without activating this app, so the target input keeps the keyboard. It
// is placed at the bottom of the display currently being used, not
// next to the icon.

const (
	capsuleW, capsuleH = 144, 40
	circle             = 30 // the cancel and confirm circles
	hitW               = 36 // their hit targets, as high as the pill
	overlayW           = 300
	compactH           = capsuleH + 8
	panelH             = 124 // the transcript preview and the recovery panels
	noticeH            = 64  // a notice of one centered text
	shadowRoom         = 16  // below the capsule, for its shadow
)

var (
	white     = ui.RGB(255, 255, 255)
	black     = ui.RGB(0, 0, 0)
	dim       = ui.RGBA(255, 255, 255, 0.45)
	buttonBg  = ui.RGB(58, 58, 60)
	errorText = ui.RGB(255, 140, 130)
	panelBg   = ui.RGBA(0, 0, 0, 0.92)
	panelLine = ui.RGBA(255, 255, 255, 0.2)
)

type overlay struct {
	a                *app
	win              *mygo.Window
	v                session.View
	expanded         bool           // the transcript preview is open
	area             mygo.Rectangle // the work area currently being used
	followID         uint64
	pointerArea      mygo.Rectangle
	activeWindow     uint32
	activeFrame      macos.SettingsFrame
	height           int
	room             int // kept below the capsule for its shadow
	closing          bool
	fadeAt           time.Time
	fadeFrom, fadeTo float32
	fadeID           uint64
	levels           [session.Levels]float32
	levelAt          time.Time
	// classic is the renderer of this dictation, read from the settings
	// when it starts, so a change of style never lands mid-dictation.
	classic      bool
	width        int
	errorID      uint64
	errorView    session.View
	errorHidden  bool
	sendFinished bool // Suppress the terminal notice after a completed countdown.
	v2           v2State
}

func newOverlay(a *app) *overlay {
	o := &overlay{a: a}
	o.classic = a.settings.Get().CapsuleStyle == config.CapsuleClassic
	o.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      a.tray,
		Width:     overlayW,
		Height:    compactH,
		Hidden:    true,
		Focusable: true,
		Content:   ui.View(o.view),
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
	o.win.Update(func() { o.syncView(v) })
}

// syncView runs on the UI thread, including error notice lifetime management.
func (o *overlay) syncView(v session.View) {
	if v.Phase != session.Failed {
		o.errorID++
		o.errorView = session.View{}
		o.errorHidden = false
	} else if v != o.errorView {
		o.errorID++
		o.errorView = v
		o.errorHidden = false
		id := o.errorID
		time.AfterFunc(3*time.Second, func() {
			o.win.Update(func() {
				if id != o.errorID || o.v.Phase != session.Failed || o.a.quitting {
					return
				}
				o.errorHidden = true
				o.hideAfterFade(nil)
			})
		})
	} else if o.errorHidden {
		return
	}
	v = conciseFailure(v)
	if o.a.sendPreview != nil && o.a.sendPreview.visible {
		if v.Phase == session.Preparing {
			o.a.sendPreview.close()
		} else {
			return
		}
	}
	if v.Phase == session.Preparing || v.Phase == session.Failed {
		if o.a.sendPreview != nil {
			o.a.sendPreview.exiting = false
		}
	}
	if v.Phase == session.Preparing || v.Phase == session.Failed {
		o.sendFinished = false
	}
	if o.sendFinished {
		o.v = v
		return
	}
	was := o.v.Phase
	if v.Phase == session.Idle {
		if o.closing || !o.win.IsVisible() {
			return
		}
		o.hideAfterFade(nil)
		return
	}
	o.v = v
	if v.Phase == session.Preparing && was != session.Preparing {
		o.expanded = false // each dictation starts with the preview closed
	}
	if was == session.Idle || o.closing {
		o.closing = false
		o.fade(1)
		o.levels = [session.Levels]float32{}
		o.levelAt = time.Time{}
		// Start at the pointer, then follow display and focus changes.
		o.area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
		o.pointerArea = o.area
		o.activeWindow, o.activeFrame = macos.ActiveOverlayWindow()
		o.followID++
		o.follow(o.followID)
		macos.ConfigureOverlay(o.win.NativeHandle())
		o.height = 0
		// A new dictation: the style chosen in Settings applies now.
		o.classic = o.a.settings.Get().CapsuleStyle == config.CapsuleClassic
		o.v2 = v2State{}
	}
	o.place()
	if !o.win.IsVisible() {
		o.win.ShowInactive()
	}
}

// A session-scoped timer also follows during silence, static results and reduced
// motion. It stops on hide; generation checks discard callbacks from old sessions.
func (o *overlay) follow(id uint64) {
	time.AfterFunc(200*time.Millisecond, func() {
		o.win.Update(func() {
			if id != o.followID || o.a.quitting {
				return
			}
			if o.a.sendPreview != nil && o.a.sendPreview.visible {
				o.follow(id)
				return
			}
			pointer := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
			window, frame := macos.ActiveOverlayWindow()
			switch {
			case pointer != o.pointerArea:
				o.area = pointer
			case window != 0 && (window != o.activeWindow || frame != o.activeFrame):
				o.area = mygo.Screen.DisplayMatching(mygo.Rectangle{X: frame.X, Y: frame.Y, Width: frame.Width, Height: frame.Height}).WorkArea
			default:
				// Refresh the work area after Dock/menu-bar or display configuration changes.
				o.area = mygo.Screen.DisplayMatching(o.area).WorkArea
			}
			o.pointerArea = pointer
			o.activeWindow, o.activeFrame = window, frame
			o.place()
			o.follow(id)
		})
	})
}

// Retarget from the currently displayed opacity, so a rapid restart never jumps.
func (o *overlay) opacity(now time.Time) float32 {
	t := min(1, max(0, float32(now.Sub(o.fadeAt).Seconds()/0.22)))
	t = 1 - (1-t)*(1-t)*(1-t)
	return o.fadeFrom + (o.fadeTo-o.fadeFrom)*t
}
func (o *overlay) fade(to float32) {
	now := time.Now()
	o.fadeFrom, o.fadeTo = o.opacity(now), to
	o.fadeAt = now
	o.fadeID++
}

// A generation check protects a new dictation or countdown from an old exit.
func (o *overlay) hideAfterFade(after func()) {
	o.closing = true
	o.fade(0)
	id := o.fadeID
	time.AfterFunc(220*time.Millisecond, func() {
		o.win.Update(func() {
			if !o.closing || o.fadeID != id {
				return
			}
			o.followID++
			o.win.Hide()
			o.v = session.View{}
			o.expanded = false
			o.closing = false
			if after != nil {
				after()
			}
		})
	})
}

// place sizes the window for what it shows and keeps the capsule still.
// Below the capsule the window keeps room for its shadow, taken from the
// offset to the bottom of the screen, so the capsule itself does not move.
func (o *overlay) place() {
	h, w := compactH, overlayW
	if !o.classic {
		w = overlayV2W
		h += v2HintRoom
	}
	if k := o.panelKind(); k != panelNone {
		if o.classic {
			h += panelHeight(k) + 8
		} else {
			h += o.v2PanelHeight(k) + v2PanelGap
		}
	}
	offset := max(0, o.a.settings.Get().OverlayBottomOffset)
	o.room = min(shadowRoom, offset)
	bounds := mygo.Rectangle{
		X:      o.area.X + (o.area.Width-w)/2,
		Y:      o.area.Y + o.area.Height - h - offset,
		Width:  w,
		Height: h + o.room,
	}
	o.height, o.width = h+o.room, w
	if o.win.Bounds() != bounds {
		o.win.SetBounds(bounds)
	}
}

// What the box above the capsule is, as mock/capsule-states.html has it.
const (
	panelNone     = iota
	panelNotice   // 300×64, one centered text: nothing to preview yet, what a finished dictation did, or a failure with nothing to recover
	panelPreview  // 300×124, the transcript as it arrives
	panelRetained // 300×124, text that was not inserted, whatever ended the dictation, and Copy
	panelRecover  // 300×124, a failed cleanup: Retry cleanup and Copy
)

func (o *overlay) panelKind() int {
	v := o.v
	switch v.Phase {
	case session.Idle:
		return panelNone
	case session.Done:
		// The green check needs no words; a preview that was open closes.
		if v.Message != "" {
			return panelNotice
		}
		return panelNone
	case session.Failed:
		switch {
		case v.Text == "":
			return panelNotice
		case v.Retained:
			return panelRetained
		}
		return panelRecover
	}
	switch {
	case !o.expanded:
		return panelNone
	case v.Final == "" && v.Pending == "":
		return panelNotice
	}
	return panelPreview
}

func panelHeight(kind int) int {
	if kind == panelNotice {
		return noticeH
	}
	return panelH
}

func (o *overlay) view(c *ui.Context) {
	if p := o.a.sendPreview; p != nil && (p.visible || p.exiting) {
		if !o.classic {
			o.viewV2(c)
		} else {
			p.view(c)
		}
		return
	}
	if !o.classic {
		o.viewV2(c)
		return
	}
	o.viewClassic(c)
}

// viewClassic is the black capsule, kept as the setting's Classic style.
func (o *overlay) viewClassic(c *ui.Context) {
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
	ui.Column(c).Fill().Padding(0, 0, float32(o.room), 0).Justify(ui.End).AlignItems(ui.Center).Gap(6).Opacity(alpha).Children(func() {
		if kind := o.panelKind(); kind != panelNone {
			o.panelView(c, kind)
		}
		ui.Row(c).Size(capsuleW, capsuleH).Scale(0.96+0.04*alpha, 0.96+0.04*alpha).Radius(capsuleH/2).Background(black).
			Border(0.5, ui.RGBA(255, 255, 255, 0.22)).Shadow(0, 5, 16, 0, ui.RGBA(0, 0, 0, 0.13)).Padding(0, 2).Gap(2).Children(func() {
			// Cancel: nothing is inserted, whatever arrives later. The
			// circle is drawn inside a taller, wider box that takes the
			// click, so the small control stays easy to hit. On a notice
			// or a failure it closes the overlay, as Escape does.
			if ui.Box(c.Key("cancel")).Size(hitW, capsuleH).
				Label(T("ovCancel")).Tooltip(T("ovCancel")).Cursor(ui.CursorPointer).Draw(drawCross).Clicked() {
				if !o.closing {
					go ctrl.Cancel()
				}
			}

			draw := func(p *ui.Painter, r ui.Rect) { o.drawDots(c, p, r) }
			switch v.Phase {
			case session.Preparing:
				ui.Box(c.Key("preparing-dots")).Grow(1).Height(capsuleH).Draw(draw)
			case session.Done, session.Failed:
				// A mark, not a control: what there is to read or to do
				// is in the box above.
				ui.Box(c.Key("mark")).Grow(1).Height(capsuleH).Draw(draw)
			default:
				if ui.Box(c.Key("dots")).Grow(1).Height(capsuleH).Label(T("ovPreview")).
					Cursor(ui.CursorPointer).Draw(draw).Clicked() {
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

func (o *overlay) panelView(c *ui.Context, kind int) {
	v := o.v
	ctrl := o.a.ctrl
	if kind == panelNotice {
		// One centered text: white for what a finished dictation did, the
		// error color for a failure, dimmed while there is no transcript.
		text, size, col, lines := T("noText"), float32(13), dim, 2
		switch v.Phase {
		case session.Done:
			text, col = v.Message, white
		case session.Failed:
			// A failure's reason may need a third line; it stays centered.
			text, size, col, lines = v.Message, 12, errorText, 3
		}
		ui.Column(c).Width(overlayW).Height(noticeH).Radius(10).
			Background(panelBg).Border(1, panelLine).Padding(8, 16).
			Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, text).FillWidth().FontSize(size).LineHeight(1.3).TextColor(col).
				TextAlign(ui.Center).MaxLines(lines)
		})
		return
	}
	box := ui.Column(c).Width(overlayW).Height(panelH).Radius(12).Background(panelBg).
		Border(1, panelLine).Padding(9, 12).Gap(5)
	// copyButton puts the held text on the clipboard; the overlay then says so
	// for a moment and closes.
	copyButton := func() {
		if panelButton(c, "copy", T("copyText")) {
			go ctrl.CopyFailed()
		}
	}
	switch kind {
	case panelRetained:
		// The text is whole and was not written: nothing here is an
		// error to read, there is one thing to do.
		box.AlignItems(ui.Center).Children(func() {
			// Why it ended, when there is a reason to give: the microphone
			// went away, the recognition broke off. In the same neutral
			// white: the words are safe, and that is what matters here.
			caption := T("ovRetained")
			if v.Notice != "" {
				caption = v.Notice
			}
			ui.Text(c, caption).FillWidth().FontSize(13).LineHeight(1.3).TextColor(white).TextAlign(ui.Center).MaxLines(2)
			ui.Text(c, tail(v.Text, 60)).FillWidth().FontSize(12).LineHeight(1.3).TextColor(ui.RGBA(255, 255, 255, 0.6)).
				TextAlign(ui.Center).MaxLines(2)
			ui.Spacer(c)
			ui.Row(c).Justify(ui.Center).Children(copyButton)
		})
	case panelRecover:
		// A failed cleanup: Retry cleanup on the left, Copy on the right.
		// The original is not inserted from here, and the cross or
		// Escape closes.
		box.Children(func() {
			ui.Text(c, v.Message).FontSize(12).TextColor(errorText).MaxLines(2)
			ui.Text(c, tail(v.Text, 60)).FontSize(13).TextColor(white).MaxLines(2)
			ui.Spacer(c)
			ui.Row(c).FillWidth().Justify(ui.Center).Gap(6).Children(func() {
				retryLabel := T("retryClean")
				if v.TranslationLanguage != "" {
					retryLabel = T("retryTranslation")
				}
				if v.CleanupFailed && panelButton(c, "retry", retryLabel) {
					go ctrl.RetryCleanup()
				}
				copyButton()
			})
		})
	default:
		// Final text is white; what may still change is dimmed and
		// marked as such.
		box.Children(func() {
			if v.Final != "" {
				ui.Text(c, tail(v.Final, 90)).FontSize(13).TextColor(white).MaxLines(4)
			}
			if v.Pending != "" {
				ui.Text(c, T("pendingMark", tail(v.Pending, 60))).FontSize(13).TextColor(dim).Italic().MaxLines(2)
			}
			ui.Spacer(c)
			if v.Cleanup {
				label := T("willClean")
				if v.TranslationLanguage != "" {
					label = T("translationWorking")
				}
				ui.Text(c, label).FontSize(11).TextColor(dim)
			}
		})
	}
}

// panelButton is the small translucent button of the recovery panels.
func panelButton(c *ui.Context, key, label string) bool {
	b := ui.Row(c.Key(key)).Padding(3, 6).Radius(5).Border(1, ui.RGBA(255, 255, 255, 0.27)).
		Label(label).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(ui.RGBA(255, 255, 255, 0.2))
	} else {
		b.Background(ui.RGBA(255, 255, 255, 0.1))
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(white).SingleLine()
	})
	return b.Clicked()
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
//   - Inserting: still dots.
//   - Done: a green check, or a dim dash when it has something to say.
//   - Failed: an exclamation mark; the dim dash when the text is only
//     waiting to be copied, which is not shown as an error.
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
			size := float32(2.5)
			if !still {
				d := math.Abs(float64(i - n/2))
				size = float32(2.5 + 2*(0.5+0.5*math.Sin(t*5-d*1.1)))
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
			check := (&ui.Path{}).MoveTo(cx-4.5, cy+0.4).LineTo(cx-1.6, cy+3.3).LineTo(cx+4.5, cy-3.2)
			p.StrokePath(check, 2.4, ui.RGB(120, 220, 140))
		} else {
			p.Line(cx-5, cy, cx+5, cy, 1.8, dim)
		}
	case session.Failed:
		cx := r.X + r.W/2
		if o.v.Retained && o.v.Text != "" {
			p.Line(cx-5, cy, cx+5, cy, 1.8, dim)
			break
		}
		p.Fill(ui.Rect{X: cx - 0.9, Y: cy - 6.9, W: 1.8, H: 8.8}, errorText, 0.9)
		p.Fill(ui.Rect{X: cx - 0.9, Y: cy + 4.1, W: 1.8, H: 2}, errorText, 0.9)
	}
}
