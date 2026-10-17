package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/session"
)

// The V2 capsule is mock/capsule-states-v2.html: the website demo's 40 px
// indigo pill with five 3 px bars (the last mint), a short state label and
// the words as they are heard inline after a divider. The pill widens
// smoothly with the words up to 20 em; beyond that the newest stay in view
// and the oldest fade at the edge. Cancel sits just outside the left edge:
// it slides out while the pointer is over the pill and withdraws 500 ms
// after it leaves. Above the pill a 300 px card shows the full transcript
// on demand, a notice, or what to do after a failure.
//
// Spacing, as the mock's tokens: inset 8 + pad 10 from the pill's edge to
// the mark; mark to label 9; label to divider 9, divider to words 11;
// symmetric pill padding; Close 8 outside.

const (
	overlayV2W   = 520 // room for the widest pill and Close on either side
	v2H          = 40
	v2Inset      = 8
	v2Pad        = 10
	v2Gap        = 9
	v2TextPad    = 11
	v2Ctl        = 24 // the Close circle
	v2Hit        = 32 // their hit targets, 4 px around the circles
	v2HitOut     = (v2Hit - v2Ctl) / 2
	v2CloseGap   = 8
	v2Mark       = 27 // the 27×18 slot of the bars, the star or a glyph
	v2LiveMax    = 240
	v2FontSize   = 12
	v2PanelW     = 300
	v2HintRoom   = 20 // space above Close for the Esc hint
	v2PanelGap   = 8
	v2PanelInner = v2PanelW - 2*14 - 2
	v2LeaveDelay = 500 * time.Millisecond
	v2ScrollMax  = 132 // the expanded transcript scrolls beyond this
)

var (
	v2Indigo    = ui.Hex("#343b85")
	v2IndigoEnd = ui.Hex("#101b35")
	v2Ink       = ui.Hex("#f7faff")
	v2Mint      = ui.Hex("#69e7c3")
	v2MintInk   = ui.Hex("#0f3a30")
	v2Muted     = ui.Hex("#aab2ce")
	v2ErrMark   = ui.Hex("#ffc1a8")
	v2CloseBg   = ui.Hex("#293365")
	v2CloseHov  = ui.Hex("#3b4782")
	v2PanelBg   = ui.RGBA(23, 32, 58, 0.94)
	v2PanelText = ui.Hex("#f6f8ff")
	v2PanelDim  = ui.Hex("#a3afce")
)

// v2State is what the V2 renderer keeps across frames of one dictation.
type v2State struct {
	motion  v2MotionState
	heard   bool      // the microphone has heard speech: Listening, not Waiting for speech
	hoverAt time.Time // when the pointer was last over the pill or Close
	panelH  int       // the measured height of the card, for the window
	// keyFocus is that Close had the keyboard focus, shown, in
	// the last frame: Close stays out while it does.
	keyFocus bool
	// scroll is the expanded transcript's; it follows the newest words
	// unless the user scrolled up from them.
	scroll ui.ScrollState
}

// The states of the mock, from the session's view.
const (
	v2Preparing = iota
	v2Listening
	v2Quiet
	v2Transcribing
	v2Cleaning
	v2Inserting
	v2Done
	v2Silence
	v2Clipboard
	v2Retained
	v2CleanupFail
	v2ServiceFail
	v2MicFail
	v2MicLost
	v2Failed
	v2Sending
)

var v2Labels = [...]string{"v2Preparing", "v2Listening", "v2Quiet", "v2Transcribing", "v2Cleaning",
	"v2Inserting", "v2Done", "v2Silence", "v2Clipboard", "v2Retained", "v2CleanupFail",
	"v2ServiceFail", "v2MicFail", "v2MicLost", "v2Failed"}

func (o *overlay) v2Kind() int {
	v := o.v
	switch v.Phase {
	case session.Preparing:
		return v2Preparing
	case session.Recording:
		if o.v2.heard || v.Final != "" || v.Pending != "" {
			return v2Listening
		}
		return v2Quiet
	case session.Finalizing:
		return v2Transcribing
	case session.Cleaning:
		return v2Cleaning
	case session.Inserting:
		return v2Inserting
	case session.Done:
		switch v.Message {
		case T("noSpeech"):
			return v2Silence
		case T("copiedFallback"):
			return v2Clipboard
		}
		return v2Done
	case session.Failed:
		mic := func(s string) bool { return s == T("micMissing") || s == T("micNoFrames") }
		switch {
		case v.CleanupFailed:
			return v2CleanupFail
		case v.Text != "" && v.Notice == "":
			return v2Retained
		case v.Text != "" && mic(v.Notice):
			return v2MicLost
		case v.Text != "":
			return v2ServiceFail
		case v.Message == T("micMissing"):
			return v2MicFail
		}
		return v2Failed
	}
	return v2Preparing
}

// The mark: bars while working, a mint star while tidying, then a glyph.
const (
	markBars = iota
	markStar
	markOK
	markDash
	markErr
)

func v2MarkOf(kind int) int {
	switch kind {
	case v2Cleaning:
		return markStar
	case v2Done, v2Clipboard:
		return markOK
	case v2Silence, v2Retained, v2MicLost, v2ServiceFail:
		// A failure that still holds words is neutral; one that holds none is marked.
		return markDash
	case v2CleanupFail, v2MicFail, v2Failed:
		return markErr
	}
	return markBars
}

// v2Inline is the text shown after the divider, in the phases that show it.
func (o *overlay) v2Inline(kind int) string {
	switch kind {
	case v2Listening, v2Transcribing, v2Cleaning:
		return o.v.Final + o.v.Pending
	}
	return ""
}

func (o *overlay) viewV2(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	v := o.v
	still := c.Preferences().ReduceMotion
	now := c.Now()
	alpha := o.opacity(now)
	if still {
		alpha = o.fadeTo
	}
	if now.Sub(o.fadeAt) < 220*time.Millisecond {
		c.After(16 * time.Millisecond)
	}
	for _, l := range v.Levels {
		if session.Voiced(l) {
			o.v2.heard = true
		}
	}
	kind := o.v2Kind()
	sending := o.v2Sending()
	if sending {
		kind = v2Sending
		c.OnShortcut(0, ui.KeyEscape, o.a.cancelInput)
	}
	_, label, _ := o.v2Caption(kind)
	canExpand := !sending && (kind == v2Listening || kind == v2Quiet || kind == v2Transcribing || kind == v2Cleaning)
	canClose := !o.closing && !sending && v.Phase != session.Inserting
	if canClose {
		// Keep pointer visibility correct even with Reduce Motion enabled
		// or while a terminal notice is otherwise idle.
		c.After(100 * time.Millisecond)
	}
	rise := float32(8) * (1 - alpha)
	if still {
		rise = 0
	}

	ui.Column(c).Fill().Padding(0, 0, float32(o.room), 0).Justify(ui.End).AlignItems(ui.Center).
		Gap(v2PanelGap).Opacity(alpha).Translate(0, rise).Children(func() {
		if pk := o.panelKind(); pk != panelNone && !sending {
			o.panelV2(c, pk, kind)
		}
		// Close, the pill, and on the right the same room again, so the
		// pill stays centred under the card.
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			// Close's hit target reaches 4 px towards the pill: the gap
			// between them stays the 8 px from the circle, and bridges
			// the pointer's way to it.
			wrap := ui.Row(c.Key("v2wrap")).AlignItems(ui.Center).Gap(v2CloseGap - v2HitOut)
			// Keyboard focus shows Close as hovering does; a click, which
			// focuses without showing it, does not keep it out.
			hovered := o.v2PointerOver(wrap) || o.v2.keyFocus
			if hovered {
				o.v2.hoverAt = now
			}
			reveal := canClose && (hovered || now.Sub(o.v2.hoverAt) < v2LeaveDelay)
			if canClose && !hovered && reveal {
				c.After(v2LeaveDelay - now.Sub(o.v2.hoverAt))
			}
			wrap.Children(func() {
				closeFocus := o.v2Close(c, reveal, canClose)
				o.v2Pill(c, kind, label, canExpand)
				o.v2.keyFocus = closeFocus
			})
			ui.Box(c).Size(v2Hit+v2CloseGap-v2HitOut, v2H)
		})
	})

	if !still && (sending || now.Sub(o.v2.motion.changedAt) < v2MorphDuration) {
		c.After(16 * time.Millisecond)
	}
	switch v.Phase {
	case session.Recording, session.Preparing, session.Finalizing, session.Cleaning, session.Inserting:
		if !still {
			c.After(16 * time.Millisecond)
		}
	}
}

// A reused nonactivating panel can retain the UI engine's last pointer
// position when it hides or moves. Read the desktop pointer instead of
// letting a stale Hovered() keep Close visible throughout the next session.
func (o *overlay) v2PointerOver(wrap ui.Element) bool {
	if o.win == nil {
		return wrap.Hovered() // headless renderer
	}
	// Also check while stationary: a panel can appear under the pointer,
	// and mouse-exit events are not guaranteed while the panel is hidden.
	r := wrap.Bounds()
	b := o.win.ContentBounds()
	p := mygo.Screen.CursorScreenPoint()
	x, y := float32(p.X-b.X), float32(p.Y-b.Y)
	return r.W > 0 && r.H > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// v2Close is the cancel just outside the pill's left edge: nothing is
// inserted, whatever arrives later. On a notice or a failure it closes,
// as Escape does. Once writing has begun there is no cancel.
//
// It is a button the keyboard reaches: focused from the keyboard it shows
// as hovering does. It never asks for the focus itself, and the overlay is
// a nonactivating panel, so the target app keeps its input. It reports
// whether it has the keyboard focus, shown.
func (o *overlay) v2Close(c *ui.Context, reveal, can bool) bool {
	ctrl := o.a.ctrl
	if !can {
		// Writing has begun: nothing to press, nothing to focus.
		ui.Box(c.Key("v2closeoff")).Size(v2Hit, v2Hit)
		return false
	}
	b := ui.ButtonBase(c.Key("v2close")).Size(v2Hit, v2Hit).FocusRing(false).
		Label(T("ovCancel"))
	focus := b.FocusVisible()
	target := float32(0)
	if reveal || focus {
		target = 1
	}
	r := b.Animate("reveal", target, 200*time.Millisecond)
	showHint := r > 0.5 && (o.v2PointerOver(b) || focus)
	bg := v2CloseBg
	if r > 0.5 && b.Hovered() {
		bg = v2CloseHov
	}
	b.Opacity(r).Translate(12*(1-r), 0).Scale(0.85+0.15*r, 0.85+0.15*r).Draw(func(p *ui.Painter, rc ui.Rect) {
		cx, cy := rc.X+rc.W/2, rc.Y+rc.H/2
		circle := ui.Rect{X: cx - v2Ctl/2, Y: cy - v2Ctl/2, W: v2Ctl, H: v2Ctl}
		p.Shadow(circle, v2Ctl/2, 0, 3, 10, 0, ui.RGBA(20, 27, 56, 0.14))
		p.Fill(circle, bg, v2Ctl/2)
		if focus {
			p.FocusRing(circle, [4]float32{v2Ctl / 2, v2Ctl / 2, v2Ctl / 2, v2Ctl / 2})
		}
		const d = 3
		var x ui.Path
		x.MoveTo(cx-d, cy-d).LineTo(cx+d, cy+d).MoveTo(cx+d, cy-d).LineTo(cx-d, cy+d)
		p.StrokePath(&x, 1.6, v2Ink)
		if showHint {
			span := ui.Span{Text: "Esc", Size: 10, Weight: 500, Color: v2Ink}
			w, h := p.MeasureText(0, span)
			hint := ui.Rect{X: cx - (w+10)/2, Y: circle.Y - h - 9, W: w + 10, H: h + 4}
			p.Fill(hint, v2CloseBg, 4)
			p.RichText(cx-w/2, hint.Y+2, 0, span)
		}
	})
	if r > 0.5 || focus {
		b.Cursor(ui.CursorPointer)
		if b.Clicked() && !o.closing {
			go ctrl.Cancel()
		}
	}
	return focus
}

// v2Pill builds the status and live transcript. The trigger key ends recording.
func (o *overlay) v2Pill(c *ui.Context, kind int, label string, canExpand bool) {
	now, still := c.Now(), c.Preferences().ReduceMotion
	m := &o.v2.motion
	sending := o.v2Sending()
	key, text, widthText := o.v2Caption(kind)
	m.caption(key, text, now, still)
	labelW, _ := c.MeasureText(0, ui.Span{Text: widthText, Size: v2FontSize, Weight: 500})
	labelW = float32(math.Ceil(float64(labelW)))
	if sending {
		labelW = max(84, labelW)
	}
	inline := o.v2Inline(kind)
	if inline != "" {
		m.inline = inline
		m.inlineWidth, _ = c.MeasureText(0, ui.Span{Text: inline, Size: v2FontSize, Weight: 400})
	}
	fixed := float32(2*v2Inset+2*v2Pad+v2Mark+v2Gap) + labelW
	textCap := max(0, min(v2LiveMax, overlayV2W-2*(v2Hit+v2CloseGap-v2HitOut)-fixed-(v2Gap+1+v2TextPad)))
	liveTarget, liveAlpha, markTarget, toolsTarget := float32(0), float32(0), float32(v2Mark+v2Gap), float32(0)
	if inline != "" {
		liveTarget = v2Gap + 1 + v2TextPad + min(m.inlineWidth+1, textCap)
		liveAlpha = 1
	}
	if kind == v2Transcribing || kind == v2Cleaning {
		liveAlpha = .5
	}
	if inline == "" {
		liveAlpha = 0
	}
	if sending {
		markTarget = 0
		toolsTarget = 66
	}
	pill := ui.Row(c.Key("v2pill")).Height(v2H).Radius(v2H/2).Padding(0, v2Inset).AlignItems(ui.Center).
		Gradient(v2Indigo, v2IndigoEnd, 135).InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, .11)).
		Shadow(0, 10, 24, -8, ui.RGBA(20, 27, 56, .44)).Label(T("v2Group"))
	m.labelWidth = pill.AnimateWith("label-width", labelW, v2MorphDuration, v2MorphEase)
	m.markWidth = pill.AnimateWith("mark-width", markTarget, v2MorphDuration, v2MorphEase)
	m.liveWidth = pill.AnimateWith("live-width", liveTarget, v2MorphDuration, v2MorphEase)
	m.liveAlpha = pill.AnimateWith("live-alpha", liveAlpha, 240*time.Millisecond, ui.EaseInOut)
	m.toolsWidth = pill.AnimateWith("tools-width", toolsTarget, v2MorphDuration, v2MorphEase)
	toolsAlpha := float32(0)
	if sending {
		toolsAlpha = 1
	}
	m.toolsAlpha = pill.AnimateWith("tools-alpha", toolsAlpha, 240*time.Millisecond, ui.EaseInOut)
	m.toolsReveal = pill.AnimateWith("tools-reveal", toolsAlpha, 400*time.Millisecond, v2MorphEase)
	mark := v2MarkOf(kind)
	if mark == markBars && !sending {
		m.barKind = kind
	}
	for i := range m.markWeight {
		target := float32(0)
		if i == mark && !sending {
			target = 1
		}
		m.markWeight[i] = pill.AnimateWith(struct{ Mark int }{i}, target, 220*time.Millisecond, ui.EaseInOut)
		m.markReveal[i] = pill.AnimateWith(struct{ MarkTransform int }{i}, target, 360*time.Millisecond, v2MorphEase)
	}
	// Recording uses real, smoothed audio levels; their displayed heights flatten
	// continuously when finalizing, rather than restarting as a different waveform.
	dt := now.Sub(o.levelAt).Seconds()
	o.levelAt = now
	for i, target := range o.v.Levels {
		if !session.Voiced(target) {
			target = 0
		}
		tau := .09
		if target > o.levels[i] {
			tau = .035
		}
		blend := float32(1 - math.Exp(-min(dt, .05)/tau))
		if still {
			blend = 1
		}
		o.levels[i] += (target - o.levels[i]) * blend
	}
	for i := range m.barHeight {
		target := float32(6)
		duration := 360 * time.Millisecond
		if m.barKind == v2Listening || m.barKind == v2Quiet {
			target = 5 + 13*min(1, o.levels[session.Levels-5+i])
			duration = 90 * time.Millisecond
		}
		m.barHeight[i] = pill.AnimateWith(struct{ Bar int }{i}, target, duration, v2MorphEase)
	}
	m.kind = kind
	pill.DrawOver(func(_ *ui.Painter, r ui.Rect) { m.bounds = r; m.paintAt = now })
	pill.Children(func() {
		body := ui.Row(c.Key("v2body")).Height(v2H).Radius(v2H/2).Padding(0, v2Pad).AlignItems(ui.Center).Label(label)
		if canExpand {
			body.Role(ui.RoleButton).Expanded(o.expanded).Cursor(ui.CursorPointer)
			if body.Clicked() {
				go o.win.Update(func() { o.expanded = !o.expanded; o.place() })
			}
		}
		body.Children(func() {
			ui.Box(c.Key("v2mark")).Size(m.markWidth, 18).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				r.W = v2Mark
				for i, a := range m.markWeight {
					if a < .001 {
						continue
					}
					k := m.barKind
					switch i {
					case markStar:
						k = v2Cleaning
					case markOK:
						k = v2Done
					case markDash:
						k = v2Retained
					case markErr:
						k = v2Failed
					}
					reveal := m.markReveal[i]
					scale := .8 + .2*reveal
					p.Transform(ui.Scaling(scale, scale).Then(ui.Translation(0, 3*(1-reveal))), r.X+r.W/2, r.Y+r.H/2, func() { o.v2DrawMark(c, p, r, k, a) })
				}
			})
			ui.Box(c.Key("v2label")).Size(m.labelWidth, v2H).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) { m.drawCaption(p, r, now) })
			live := ui.Box(c.Key("v2live")).Size(m.liveWidth, v2H).Shrink(0)
			shiftTarget := max(0, m.inlineWidth-min(m.inlineWidth+1, textCap))
			shift := live.Animate("shift", shiftTarget, 160*time.Millisecond)
			live.Draw(func(p *ui.Painter, r ui.Rect) {
				if r.W < .5 || m.liveAlpha < .001 || m.inline == "" {
					return
				}
				a := min(1, r.W/(v2Gap+1+v2TextPad)) * m.liveAlpha
				cy := r.Y + r.H/2
				p.Clip(r, 0, func() {
					p.Fill(ui.Rect{X: r.X + v2Gap, Y: cy - 7, W: 1, H: 14}, ui.RGBA(247, 250, 255, .22*a), 0)
					span := ui.Span{Text: m.inline, Size: v2FontSize, Weight: 400, Color: ui.RGBA(247, 250, 255, .84*a)}
					_, h := p.MeasureText(0, span)
					box := ui.Rect{X: r.X + v2Gap + 1 + v2TextPad, Y: r.Y, W: max(0, r.W-(v2Gap+1+v2TextPad)), H: r.H}
					p.Clip(box, 0, func() { p.RichText(box.X-shift, cy-h/2, 0, span) })
					if shift > .5 {
						p.FillGradient(ui.Rect{X: box.X, Y: cy - h/2, W: 18, H: h}, ui.LinearGradient{From: ui.Hex("#262d66").Alpha(a), To: ui.RGBA(38, 45, 102, 0), Angle: 90}, 0)
					}
				})
			})
		})
		o.v2SendTools(c, m.toolsWidth, m.toolsAlpha, m.toolsReveal)
	})
}

// v2DrawMark paints the 27×18 slot: five bars while working, the mint star
// while tidying, then a check, a dash or an exclamation mark.
func (o *overlay) v2DrawMark(c *ui.Context, p *ui.Painter, r ui.Rect, kind int, alpha float32) {
	still := c.Preferences().ReduceMotion
	t := float64(c.Now().UnixMilli()) / 1000
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	bar := func(i int, h, a float32) {
		col := ui.RGBA(247, 250, 255, a*alpha)
		if i == 4 {
			col = ui.RGBA(105, 231, 195, a*alpha)
		}
		h = o.v2.motion.barHeight[i]
		x := r.X + float32(i)*6
		p.Fill(ui.Rect{X: x, Y: cy - h/2, W: 3, H: h}, col, 1.5)
	}
	switch v2MarkOf(kind) {
	case markBars:
		switch kind {
		case v2Listening, v2Quiet:
			// The newest five levels, smoothed between audio callbacks; in
			// silence the bars settle into short still strokes.
			for i := range 5 {
				l := o.levels[session.Levels-5+i]
				if l > 0.005 {
					bar(i, 5+13*min(1, l), 1)
				} else {
					bar(i, 5, 0.7)
				}
			}
		case v2Transcribing, v2Inserting:
			// 6 px strokes pulsing once a second, 0.12 s apart.
			for i := range 5 {
				a := float32(1)
				if !still {
					a = float32(0.3 + 0.7*(0.5-0.5*math.Cos(2*math.Pi*(t-0.12*float64(i)))))
				}
				bar(i, 6, a)
			}
		default: // Preparing: dim, still bars
			for i := range 5 {
				bar(i, 6, 0.4)
			}
		}
	case markStar:
		v2DrawStar(p, r, t, still, alpha)
	case markOK:
		k := float32(1)
		check := (&ui.Path{}).MoveTo(cx+(3.5-8)*k, cy+(8.4-8)*k).LineTo(cx+(6.5-8)*k, cy+(11.4-8)*k).LineTo(cx+(12.5-8)*k, cy+(5-8)*k)
		p.StrokePath(check, 1.8, v2Mint.Alpha(alpha))
	case markDash:
		p.Line(cx-4, cy, cx+4, cy, 1.8, v2Muted.Alpha(alpha))
	case markErr:
		p.Fill(ui.Rect{X: cx - 0.9, Y: cy - 5.9, W: 1.8, H: 7.1}, v2ErrMark.Alpha(alpha), 0.9)
		p.Fill(ui.Rect{X: cx - 0.9, Y: cy + 3.5, W: 1.8, H: 1.8}, v2ErrMark.Alpha(alpha), 0.9)
	}
}

// v2DrawStar is the tidying star: one mint four-point star, 16 px tall at
// rest. Each 1.6 s it turns in from −15°, opens from 78% to full size with
// a soft glow, eases back slightly, then gathers again. With less motion
// it stands still at full size, without the glow.
func v2DrawStar(p *ui.Painter, r ui.Rect, t float64, still bool, weight float32) {
	ox, oy := r.X+(r.W-27)/2, r.Y+(r.H-18)/2
	star := func() *ui.Path {
		pt := func(x, y float32) (float32, float32) { return ox + x, oy + y }
		var s ui.Path
		s.MoveTo(pt(13.5, 1))
		x1, y1 := pt(14.1, 6.6)
		x2, y2 := pt(15.9, 8.4)
		x3, y3 := pt(21.5, 9)
		s.CubeTo(x1, y1, x2, y2, x3, y3)
		x1, y1 = pt(15.9, 9.6)
		x2, y2 = pt(14.1, 11.4)
		x3, y3 = pt(13.5, 17)
		s.CubeTo(x1, y1, x2, y2, x3, y3)
		x1, y1 = pt(12.9, 11.4)
		x2, y2 = pt(11.1, 9.6)
		x3, y3 = pt(5.5, 9)
		s.CubeTo(x1, y1, x2, y2, x3, y3)
		x1, y1 = pt(11.1, 8.4)
		x2, y2 = pt(12.9, 6.6)
		x3, y3 = pt(13.5, 1)
		s.CubeTo(x1, y1, x2, y2, x3, y3)
		return s.Close()
	}()
	cx, cy := ox+13.5, oy+9
	if still {
		p.FillPath(star, v2Mint.Alpha(weight))
		return
	}
	// The mock's keyframes: 0% and 100% gathered, 42% open and glowing,
	// 68% eased back.
	type key struct{ at, scale, rot, alpha, glow float64 }
	keys := [...]key{{0, 0.78, -15, 0.7, 0}, {0.42, 1, 0, 1, 1}, {0.68, 0.93, 4, 0.92, 0.6}, {1, 0.78, -15, 0.7, 0}}
	u := math.Mod(t/1.6, 1)
	a, b := keys[0], keys[1]
	for i := 1; i < len(keys); i++ {
		if u <= keys[i].at {
			a, b = keys[i-1], keys[i]
			break
		}
	}
	e := (u - a.at) / (b.at - a.at)
	e = e * e * (3 - 2*e)
	mix := func(x, y float64) float32 { return float32(x + (y-x)*e) }
	scale, rot, alpha, glow := mix(a.scale, b.scale), mix(a.rot, b.rot), mix(a.alpha, b.alpha), mix(a.glow, b.glow)
	if glow > 0.01 {
		p.Transform(ui.Scaling(scale*1.22, scale*1.22).Then(ui.Rotation(rot)), cx, cy, func() {
			p.FillPath(star, ui.RGBA(105, 231, 195, 0.28*glow*weight))
		})
	}
	p.Transform(ui.Scaling(scale, scale).Then(ui.Rotation(rot)), cx, cy, func() {
		p.FillPath(star, ui.RGBA(105, 231, 195, alpha*weight))
	})
}

// v2PanelHeight is the card's height for the window: as measured by the
// last frame, or an estimate before the first.
func (o *overlay) v2PanelHeight(kind int) int {
	if o.v2.panelH > 0 {
		return o.v2.panelH
	}
	switch kind {
	case panelNotice:
		if o.v.Phase == session.Done || o.v.Phase == session.Failed {
			return 46
		}
		return 66
	case panelPreview:
		return 110
	}
	return 136
}

// panelV2 is the card above the pill: the transcript as it arrives, one
// centred notice, or a failure's kept words with what can be done.
func (o *overlay) panelV2(c *ui.Context, pk, kind int) {
	v := o.v
	ctrl := o.a.ctrl
	lineH := func(size float32) float32 {
		_, h := c.MeasureText(0, ui.Span{Text: "Xg", Size: size})
		return h
	}
	textH := func(s string, size float32, lines int) float32 {
		_, h := c.MeasureText(v2PanelInner, ui.Span{Text: s, Size: size})
		if lines > 0 {
			h = min(h, float32(lines)*lineH(size))
		}
		return h
	}
	const body, small = 12.5, 10.5
	card := ui.Column(c.Key("v2panel")).Width(v2PanelW).Radius(14).Background(v2PanelBg).
		Border(1, ui.RGBA(255, 255, 255, 0.12)).Shadow(0, 14, 30, -12, ui.RGBA(20, 27, 56, 0.44))
	var h float32
	caption := func(s string) {
		ui.Text(c, s).FontSize(small).TextColor(v2PanelDim).MaxLines(2)
	}
	actions := func(buttons func()) {
		ui.Row(c).Gap(6).Children(buttons)
	}
	switch {
	case pk == panelNotice && (v.Phase == session.Done || v.Phase == session.Failed):
		text := v.Message
		h = 22 + 2 + textH(text, body, 3)
		card.Padding(11, 16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, text).FillWidth().FontSize(body).TextColor(v2PanelText).TextAlign(ui.Center).MaxLines(3)
		})
	case pk == panelNotice || pk == panelPreview:
		// The full transcript on demand: final words white, what may still
		// change dimmed.
		capKey := "v2LiveCap"
		if kind == v2Cleaning {
			capKey = "v2CleaningCap"
		}
		// The whole transcript, in a box that scrolls once it is taller
		// than v2ScrollMax and follows the newest words unless the user
		// scrolled up from them.
		final, pending := v.Final, v.Pending
		all := final + pending
		h = 25 + 2 + lineH(small) + 6
		if all == "" {
			h += lineH(body)
		}
		var scrollH float32
		if all != "" {
			_, full := c.MeasureText(v2PanelInner, ui.Span{Text: all, Size: body})
			scrollH = min(full, v2ScrollMax)
			h += scrollH
		}
		card.Padding(12, 14, 13, 14).Children(func() {
			caption(T(capKey))
			ui.Box(c).Height(6)
			if all == "" {
				ui.Text(c, T("v2NoWords")).FontSize(body).TextColor(v2PanelDim)
				return
			}
			if o.v2.scroll.Y >= o.v2.scroll.MaxY {
				o.v2.scroll.Y = math.MaxFloat32
			}
			ui.Scroll(c.Key("v2scroll")).Height(scrollH).FillWidth().TrackScroll(&o.v2.scroll).Children(func() {
				ui.RichText(c, ui.Span{Text: final, Color: v2PanelText}, ui.Span{Text: pending, Color: v2PanelDim}).
					FillWidth().FontSize(body).Selectable()
			})
		})
	default:
		// Kept words: neutral, the full text safe to copy; a failed tidy
		// offers trying again or the original.
		capText := T("v2RetainedCap")
		switch {
		case v.CleanupFailed:
			capText = v.Message
		case v.Notice != "":
			capText = v.Notice
		case !v.Retained && v.Message != "":
			capText = v.Message
		}
		h = 25 + 2 + textH(capText, small, 2) + 6 + textH(v.Text, body, 3) + 11 + 26
		card.Padding(12, 14, 13, 14).Children(func() {
			caption(capText)
			ui.Box(c).Height(6)
			ui.Text(c, v.Text).FillWidth().FontSize(body).TextColor(v2PanelText).MaxLines(3)
			ui.Box(c).Height(11)
			actions(func() {
				if v.CleanupFailed {
					if v2Button(c, "retry", T("v2Retry"), true) {
						go ctrl.RetryCleanup()
					}
					if v2Button(c, "raw", T("v2UseRaw"), false) {
						go ctrl.InsertOriginal()
					}
					if v2Button(c, "copy", T("copyText"), false) {
						go ctrl.CopyFailed()
					}
					return
				}
				if v2Button(c, "copy", T("copyText"), true) {
					go ctrl.CopyFailed()
				}
			})
		})
	}
	// Size the window to what the card measured, after this frame.
	if need := int(math.Ceil(float64(h))); need != o.v2.panelH {
		o.v2.panelH = need
		go o.win.Update(o.place)
	}
}

// v2Button is a 26 px action of the card; the first is mint.
func v2Button(c *ui.Context, key, label string, primary bool) bool {
	b := ui.Row(c.Key(key)).Height(26).Padding(0, 11).Radius(7).AlignItems(ui.Center).
		Label(label).Cursor(ui.CursorPointer)
	hover := b.Hovered()
	col := v2PanelText
	switch {
	case primary && hover:
		b.Background(ui.Hex("#86eed0"))
	case primary:
		b.Background(v2Mint)
	case hover:
		b.Background(ui.RGBA(255, 255, 255, 0.13)).Border(1, ui.RGBA(255, 255, 255, 0.18))
	default:
		b.Background(ui.RGBA(255, 255, 255, 0.07)).Border(1, ui.RGBA(255, 255, 255, 0.18))
	}
	weight := 400
	if primary {
		col, weight = v2MintInk, 600
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(11.5).FontWeight(weight).TextColor(col).SingleLine()
	})
	return b.Clicked()
}

func init() {
	for k, v := range map[string][3]string{
		"sCapsule":       {"Dictation capsule", "聽寫膠囊", "听写胶囊"},
		"sCapsuleHint":   {"Classic is the earlier black capsule. A change applies from the next dictation.", "經典為原先的黑色膠囊；變更自下一次聽寫起生效。", "经典为原先的黑色胶囊；更改从下一次听写起生效。"},
		"capV2":          {"Indigo", "靛藍", "靛蓝"},
		"capClassic":     {"Classic", "經典", "经典"},
		"v2Preparing":    {"Preparing", "準備中", "准备中"},
		"v2Listening":    {"Listening", "正在聆聽", "正在聆听"},
		"v2Quiet":        {"Waiting for speech", "等待說話", "等待说话"},
		"v2Transcribing": {"Transcribing", "正在轉錄", "正在转录"},
		"v2Cleaning":     {"Tidying up", "正在整理", "正在整理"},
		"v2Inserting":    {"Inserting", "正在寫入", "正在写入"},
		"v2Done":         {"Inserted", "已寫入", "已写入"},
		"v2Silence":      {"No speech heard", "未聽到語音", "未听到语音"},
		"v2Clipboard":    {"Copied", "已複製文字", "已复制文字"},
		"v2Retained":     {"Text kept", "文字已保留", "文字已保留"},
		"v2CleanupFail":  {"Tidy failed", "整理未完成", "整理未完成"},
		"v2ServiceFail":  {"Recognition stopped", "轉錄中斷", "转录中断"},
		"v2MicFail":      {"No microphone", "找不到麥克風", "找不到麦克风"},
		"v2MicLost":      {"Microphone lost", "麥克風已斷開", "麦克风已断开"},
		"v2Failed":       {"Couldn’t dictate", "無法聽寫", "无法听写"},
		"v2Group":        {"Dictation status", "聽寫狀態", "听写状态"},
		"v2LiveCap":      {"Live transcript", "即時轉寫", "实时转写"},
		"v2CleaningCap":  {"Original transcript", "原始轉寫", "原始转写"},
		"v2NoWords":      {"No words yet", "尚無文字", "尚无文字"},
		"v2RetainedCap":  {"Couldn’t insert", "未能寫入", "未能写入"},
		"v2Retry":        {"Try again", "重試整理", "重试整理"},
		"v2UseRaw":       {"Use original", "使用原文", "使用原文"},
	} {
		texts[k] = v
	}
}
