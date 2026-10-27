package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"voiceinput/internal/session"
)

// The bubble capsule is mock/capsule-states-bubble.html: the V2 renderer's
// states, behaviours and Close, laid out in two rows. Above, a 32 px indigo
// chip holds a small companion and the state label; below, a speech bubble
// with its tail towards the companion holds the words as they are heard.
// The bubble widens with the words up to 360 px and then wraps; it shows
// the newest two lines, the older fading at its top edge. A click opens it
// to six lines that scroll, in place of V2's card. A failure that kept
// words shows them, and what can be done, in the bubble.

const (
	bubbleChipH     = 32
	bubbleBuddy     = 28
	bubbleLabelLeft = 7
	bubbleLabelPad  = bubbleLabelLeft + 12
	bubbleMinW      = 60
	bubbleMaxW      = 360
	bubblePadX      = 12
	bubblePadY      = 8
	bubbleTailGap   = 8 // chip to bubble: 2 px of air, then the 6 px tail
	bubbleFont      = 13
	bubbleLines     = 2
	bubbleOpenLines = 6
	bubbleKeptLines = 3
	bubbleRadius    = 16
)

var (
	bubbleTop    = ui.Hex("#2d3576")
	bubbleEnd    = ui.Hex("#1b2448")
	bubbleChipBg = ui.Hex("#262d6c") // the ring around the companion's badge
	buddyFrom    = ui.Hex("#7a83e2")
	buddyTo      = ui.Hex("#454da8")
	buddyEar     = ui.Hex("#252c66")
)

// What the bubble holds.
const (
	bubbleNone   = iota
	bubbleDots   // waiting for words
	bubbleWords  // the transcript, newest at the end
	bubbleNotice // what a finished or failed dictation has to say
	bubbleKept   // words not inserted, with what can be done
)

// bubbleContent is what the bubble shows, kept while it folds away.
type bubbleContent struct {
	mode           int
	final, pending string
	w, viewH       float32 // the bubble's width and its text's height
	extra          float32 // the kept words' caption and actions
}

func (o *overlay) bubbleMode(kind int) (mode int, final, pending string) {
	v := o.v
	if o.v2Sending() {
		if d := o.a.sendPreview.draft; d != "" {
			return bubbleWords, d, ""
		}
		return bubbleNone, "", ""
	}
	switch kind {
	case v2Preparing:
		return bubbleDots, "", ""
	case v2Listening, v2Quiet, v2Transcribing, v2Cleaning, v2Inserting:
		switch {
		case v.Final+v.Pending != "":
			return bubbleWords, v.Final, v.Pending
		case kind == v2Listening || kind == v2Quiet:
			return bubbleDots, "", ""
		}
		return bubbleNone, "", ""
	}
	switch o.panelKind() {
	case panelRetained, panelRecover:
		return bubbleKept, v.Text, ""
	case panelNotice:
		// The label already says that nothing was heard, or that the words
		// are on the clipboard.
		if kind != v2Silence && kind != v2Clipboard && v.Message != "" && v.Message != T(v2Labels[kind]) {
			return bubbleNotice, v.Message, ""
		}
	}
	return bubbleNone, "", ""
}

// bubbleChip is the top row: the companion and the label, and while
// auto-send counts down, Esc and the countdown's cancel.
func (o *overlay) bubbleChip(c *ui.Context, kind int, label string, canExpand bool) {
	now, still := c.Now(), c.Preferences().ReduceMotion
	m := &o.v2.motion
	sending := o.v2Sending()
	key, text, widthText := o.v2Caption(kind)
	m.caption(key, text, now, still)
	labelW := float32(0)
	if widthText != "" {
		w, _ := c.MeasureText(0, ui.Span{Text: widthText, Size: v2FontSize, Weight: 500})
		labelW = float32(math.Ceil(float64(w))) + bubbleLabelPad
	}
	toolsTarget, toolsAlpha := float32(0), float32(0)
	if sending {
		toolsTarget, toolsAlpha = 66, 1
	}
	o.smoothLevels(now, still)

	chip := ui.Row(c.Key("bchip")).Height(bubbleChipH).Radius(bubbleChipH/2).Padding(0, 2).AlignItems(ui.Center).
		Gradient(v2Indigo, v2IndigoEnd, 135).InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, .11)).
		Shadow(0, 10, 24, -8, ui.RGBA(20, 27, 56, .44)).Label(T("v2Group"))
	m.labelWidth = chip.AnimateWith("label-width", labelW, v2MorphDuration, v2MorphEase)
	m.toolsWidth = chip.AnimateWith("tools-width", toolsTarget, v2MorphDuration, v2MorphEase)
	m.toolsAlpha = chip.AnimateWith("tools-alpha", toolsAlpha, 240*time.Millisecond, ui.EaseInOut)
	m.toolsReveal = chip.AnimateWith("tools-reveal", toolsAlpha, 400*time.Millisecond, v2MorphEase)
	m.kind = kind
	chip.DrawOver(func(_ *ui.Painter, r ui.Rect) { m.bounds = r; m.paintAt = now })
	chip.Children(func() {
		body := ui.Row(c.Key("bbody")).Height(bubbleChipH).AlignItems(ui.Center).Label(label)
		if canExpand {
			body.Role(ui.RoleButton).Expanded(o.expanded).Cursor(ui.CursorPointer)
			if body.Clicked() {
				go o.win.Update(o.toggleBubble)
			}
		}
		body.Children(func() {
			ui.Box(c.Key("buddy")).Size(bubbleBuddy, bubbleBuddy).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				face := buddyFaceOf(kind)
				level := float32(0)
				if face == faceListen {
					for _, l := range o.levels[session.Levels-3:] {
						level = max(level, l)
					}
				}
				drawBuddy(p, r, face, v2MarkOf(kind), min(1, level), float64(now.UnixMilli())/1000, still)
			})
			ui.Box(c.Key("blabel")).Size(m.labelWidth, bubbleChipH).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
				r.X, r.W = r.X+bubbleLabelLeft, r.W-bubbleLabelLeft
				if r.W > 0 {
					m.drawCaption(p, r, now)
				}
			})
		})
		o.v2SendTools(c, m.toolsWidth, m.toolsAlpha, m.toolsReveal)
	})
}

// toggleBubble opens or closes the bubble to its six lines, on the UI
// thread after the frame that was clicked.
func (o *overlay) toggleBubble() {
	o.expanded = !o.expanded
	o.v2.bubbleScroll.Y = math.MaxFloat32
	o.place()
}

// bubbleRow is the bottom row: the tail and the bubble. It is always built,
// at no height when there is nothing to show, so its sizes animate from
// where they are.
func (o *overlay) bubbleRow(c *ui.Context, kind int, canExpand bool) {
	now, still := c.Now(), c.Preferences().ReduceMotion
	lineH := func(size float32) float32 {
		_, h := c.MeasureText(0, ui.Span{Text: "Xg", Size: size})
		return h
	}
	line, capLine := lineH(bubbleFont), lineH(10.5)

	mode, final, pending := o.bubbleMode(kind)
	expanded := o.expanded && canExpand && mode == bubbleWords
	b := &o.v2.bubble
	if mode != bubbleNone {
		next := bubbleContent{mode: mode, final: final, pending: pending, w: bubbleMinW, viewH: line}
		all := final + pending
		switch mode {
		case bubbleWords, bubbleNotice:
			tw, _ := c.MeasureText(0, ui.Span{Text: all, Size: bubbleFont})
			next.w = min(bubbleMaxW, max(bubbleMinW, float32(math.Ceil(float64(tw)))+1+2*bubblePadX))
		case bubbleKept:
			next.w = bubbleMaxW
			next.extra = capLine + 4 + 10 + 26
		}
		if mode != bubbleDots {
			_, full := c.MeasureText(next.w-2*bubblePadX, ui.Span{Text: all, Size: bubbleFont})
			lines := bubbleLines
			switch {
			case expanded:
				lines = bubbleOpenLines
			case mode != bubbleWords:
				lines = bubbleKeptLines
			}
			next.viewH = min(full, float32(lines)*line)
		}
		*b = next
	}
	// What folds away keeps its words and its size, and only fades.
	height, shown := float32(0), float32(0)
	if mode != bubbleNone {
		height, shown = bubbleTailGap+2*bubblePadY+b.viewH+b.extra, 1
	}

	row := ui.Column(c.Key("brow")).AlignItems(ui.Center).Shrink(0)
	rowH := row.AnimateWith("height", height, v2MorphDuration, v2MorphEase)
	w := row.AnimateWith("width", b.w, v2MorphDuration, v2MorphEase)
	viewH := row.AnimateWith("view", b.viewH, v2MorphDuration, v2MorphEase)
	alpha := row.AnimateWith("shown", shown, 240*time.Millisecond, ui.EaseInOut)
	if still {
		rowH, w, viewH, alpha = height, b.w, b.viewH, shown
	}
	if !still && (rowH != height || w != b.w || viewH != b.viewH || alpha != shown) {
		c.After(16 * time.Millisecond)
	}
	// The window holds the larger of where the bubble is and where it goes,
	// so it is never cut while it shrinks.
	if need := int(math.Ceil(float64(max(height, rowH)))); need != o.v2.bubbleH {
		o.v2.bubbleH = need
		go o.win.Update(o.place)
	}

	inner := b.w - 2*bubblePadX
	row.Height(rowH).Opacity(alpha).Translate(0, -6*(1-alpha)).Children(func() {
		ui.Box(c.Key("btail")).Size(14, bubbleTailGap).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
			x, y := r.X, r.Y+2
			var tail ui.Path
			tail.MoveTo(x, y+6)
			tail.CubeTo(x+4, y+6, x+5.2, y, x+7, y)
			tail.CubeTo(x+8.8, y, x+10, y+6, x+14, y+6)
			p.FillPath(tail.Close(), bubbleTop)
		})
		bubble := ui.Column(c.Key("bubble")).Width(w).Height(2*bubblePadY+viewH+b.extra).Shrink(0).
			Radius(bubbleRadius).Padding(bubblePadY, bubblePadX).Clip().
			Gradient(bubbleTop, bubbleEnd, 180).InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, .11)).
			Shadow(0, 10, 24, -8, ui.RGBA(20, 27, 56, .44))
		if canExpand && b.mode == bubbleWords {
			bubble.Role(ui.RoleButton).Label(T("v2LiveCap")).Expanded(o.expanded).Cursor(ui.CursorPointer)
			if bubble.Clicked() {
				go o.win.Update(o.toggleBubble)
			}
		}
		o.v2.bubbleHover = mode != bubbleNone && o.v2PointerOver(bubble)
		bubble.Children(func() {
			switch b.mode {
			case bubbleDots:
				ui.Box(c.Key("bdots")).Size(max(0, w-2*bubblePadX), viewH).Draw(func(p *ui.Painter, r ui.Rect) {
					drawBubbleDots(p, r, kind == v2Preparing || still, float64(now.UnixMilli())/1000)
				})
			case bubbleWords:
				o.bubbleTranscript(c, b, inner, viewH, expanded, kind)
			case bubbleNotice:
				ui.Text(c, b.final).Width(inner).FontSize(bubbleFont).TextColor(v2PanelText).MaxLines(bubbleKeptLines)
			case bubbleKept:
				capText, capCol := o.keptCaption(), v2PanelDim
				if o.v.CleanupFailed {
					capCol = v2ErrMark
				}
				ui.Text(c, capText).Width(inner).FontSize(10.5).TextColor(capCol).MaxLines(1)
				ui.Box(c).Height(4)
				ui.Text(c, b.final).Width(inner).FontSize(bubbleFont).TextColor(v2PanelText).MaxLines(bubbleKeptLines)
				ui.Box(c).Height(10)
				o.keptActions(c)
			}
		})
	})
}

// bubbleTranscript is the transcript in the bubble: final words bright,
// what may still change dimmed. Closed, it always shows the newest lines
// and fades the top edge when older ones are above; open, it follows the
// newest unless the user scrolled up from them.
func (o *overlay) bubbleTranscript(c *ui.Context, b *bubbleContent, inner, viewH float32, expanded bool, kind int) {
	s := &o.v2.bubbleScroll
	if !expanded || s.Y >= s.MaxY {
		s.Y = math.MaxFloat32
	}
	view := ui.Scroll(c.Key("bscroll")).Width(inner).Height(viewH).Shrink(0).TrackScroll(s)
	if kind == v2Transcribing || kind == v2Cleaning {
		view.Opacity(.6)
	}
	view.Children(func() {
		ui.RichText(c, ui.Span{Text: b.final, Color: v2Ink.Alpha(.92)}, ui.Span{Text: b.pending, Color: v2Muted}).
			FillWidth().FontSize(bubbleFont)
	})
	if !expanded {
		view.DrawOver(func(p *ui.Painter, r ui.Rect) {
			if s.MaxY > .5 {
				p.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 14}, ui.LinearGradient{From: bubbleTop, To: bubbleTop.Alpha(0), Angle: 180}, 0)
			}
		})
	}
}

// drawBubbleDots is three 5 px dots that bounce in turn while words are
// awaited, as a chat shows someone typing; still and dim while preparing.
func drawBubbleDots(p *ui.Painter, r ui.Rect, still bool, t float64) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	for i := range 3 {
		dy, a := float32(0), float32(.35)
		if still {
			a = .25
		} else if u := math.Mod(t/1.2-float64(i)*.125, 1); u < .6 {
			bump := float32(math.Sin(math.Pi * u / .6))
			dy, a = -3*bump, .35+.6*bump
		}
		x := cx + float32(i-1)*9
		p.Fill(ui.Rect{X: x - 2.5, Y: cy - 2.5 + dy, W: 5, H: 5}, v2Ink.Alpha(a), 2.5)
	}
}

// The companion's faces.
const (
	faceIdle   = iota // eyes open, blinking now and then
	faceSleepy        // just woken: eyes shut
	faceListen        // the mouth and the ears' light follow the voice
	faceThink         // looking up and to the right
	faceHappy         // smiling eyes and mouth
	faceFlat          // a flat mouth: nothing to worry about
	faceSad           // a turned-down mouth
)

func buddyFaceOf(kind int) int {
	switch kind {
	case v2Preparing:
		return faceSleepy
	case v2Listening, v2Quiet:
		return faceListen
	case v2Transcribing, v2Cleaning:
		return faceThink
	case v2Done, v2Clipboard:
		return faceHappy
	case v2Silence, v2Retained, v2ServiceFail, v2MicLost:
		return faceFlat
	case v2CleanupFail, v2MicFail, v2Failed:
		return faceSad
	}
	return faceIdle
}

// quad adds a quadratic curve to a path as the cubic that draws it.
func quad(path *ui.Path, x0, y0, qx, qy, x1, y1 float32) {
	path.MoveTo(x0, y0)
	path.CubeTo(x0+2*(qx-x0)/3, y0+2*(qy-y0)/3, x1+2*(qx-x1)/3, y1+2*(qy-y1)/3, x1, y1)
}

// drawBuddy paints the companion in a 28×28 box, from the mock's SVG: a
// round head with headphone ears and an antenna, a mint mouth, and on the
// tidying star or a badge. mark is v2MarkOf's: a check or an exclamation
// mark shows as a badge.
func drawBuddy(p *ui.Painter, r ui.Rect, face, mark int, level float32, t float64, still bool) {
	alpha := float32(1)
	if face == faceSleepy {
		alpha = .75
	}
	x, y := r.X, r.Y-level*.9
	rect := func(rx, ry, w, h float32) ui.Rect { return ui.Rect{X: x + rx, Y: y + ry, W: w, H: h} }

	// Antenna: dim, and pulsing while listening.
	var ant ui.Path
	ant.MoveTo(x+14, y+5.2).LineTo(x+14, y+3)
	p.StrokePath(&ant, 1.2, v2Muted.Alpha(alpha))
	antA := float32(.5)
	if face == faceListen {
		antA = 1
		if !still {
			antA = float32(.775 + .225*math.Cos(2*math.Pi*t/1.1))
		}
	}
	p.Fill(rect(12.4, .8, 3.2, 3.2), v2Mint.Alpha(antA*alpha), 1.6)

	// Ears, lit by the voice while listening.
	for _, ex := range []float32{1.6, 23} {
		p.Fill(rect(ex, 11.5, 3.4, 7), buddyEar.Alpha(alpha), 1.7)
		if face == faceListen {
			p.Fill(rect(ex, 11.5, 3.4, 7), v2Mint.Alpha(.15+.85*level), 1.7)
		}
	}
	p.FillGradient(rect(4, 5, 20, 20), ui.LinearGradient{From: buddyFrom.Alpha(alpha), To: buddyTo.Alpha(alpha), Angle: 135}, 10)

	// Eyes.
	eye := v2Ink.Alpha(alpha)
	switch face {
	case faceHappy:
		for _, ex := range []float32{9, 15.8} {
			var arc ui.Path
			quad(&arc, x+ex, y+14.9, x+ex+1.6, y+12.6, x+ex+3.2, y+14.9)
			p.StrokePath(&arc, 1.4, eye)
		}
	case faceSleepy:
		p.Line(x+9.3, y+14.6, x+11.9, y+14.6, 1.4, eye)
		p.Line(x+16.1, y+14.6, x+18.7, y+14.6, 1.4, eye)
	default:
		open := float32(1)
		if !still {
			if u := math.Mod(t, 4.2) / 4.2; u > .93 {
				k := (u - .93) / .07
				open = float32(1 - .88*(1-math.Abs(2*k-1)))
			}
		}
		dx, dy := float32(0), float32(0)
		if face == faceThink {
			dx, dy = .9, -1
		}
		h := 3.8 * open
		for _, ex := range []float32{10.6, 17.4} {
			p.Fill(rect(ex+dx-1.45, 14.2+dy-h/2, 2.9, h), eye, min(1.45, h/2))
		}
	}

	// Mouth.
	switch face {
	case faceHappy:
		var smile ui.Path
		quad(&smile, x+11.3, y+19, x+14, y+21.8, x+16.7, y+19)
		p.StrokePath(&smile, 1.4, v2Mint.Alpha(alpha))
	case faceSad:
		var sad ui.Path
		quad(&sad, x+11.6, y+20.9, x+14, y+18.8, x+16.4, y+20.9)
		p.StrokePath(&sad, 1.4, v2Muted.Alpha(alpha))
	case faceFlat:
		p.Fill(rect(12, 19.6, 4, 1.2), v2Muted.Alpha(alpha), .6)
	default:
		h := 1.6 + 4.4*level
		p.Fill(rect(12, 20-h/2, 4, h), v2Mint.Alpha(alpha), .8)
	}

	// The tidying star, small, above the head; the badges stay put.
	if face == faceThink && mark == markStar {
		cx, cy := r.X+23.5, r.Y+4.5
		p.Transform(ui.Scaling(.45, .45), cx, cy, func() {
			v2DrawStar(p, ui.Rect{X: cx - 13.5, Y: cy - 9, W: 27, H: 18}, t, still, 1)
		})
	}
	bx, by := r.X+22.6, r.Y+22.6
	switch mark {
	case markOK:
		p.Fill(ui.Rect{X: bx - 5, Y: by - 5, W: 10, H: 10}, bubbleChipBg, 5)
		p.Fill(ui.Rect{X: bx - 4.3, Y: by - 4.3, W: 8.6, H: 8.6}, v2Mint, 4.3)
		check := (&ui.Path{}).MoveTo(bx-1.9, by+.1).LineTo(bx-.6, by+1.4).LineTo(bx+1.8, by-1.2)
		p.StrokePath(check, 1.4, v2MintInk)
	case markErr:
		p.Fill(ui.Rect{X: bx - 5, Y: by - 5, W: 10, H: 10}, bubbleChipBg, 5)
		p.Fill(ui.Rect{X: bx - 4.3, Y: by - 4.3, W: 8.6, H: 8.6}, v2ErrMark, 4.3)
		p.Fill(ui.Rect{X: bx - .6, Y: by - 2.6, W: 1.2, H: 3}, ui.Hex("#5a2416"), .6)
		p.Fill(ui.Rect{X: bx - .6, Y: by + 1.4, W: 1.2, H: 1.2}, ui.Hex("#5a2416"), .6)
	}
}

func init() {
	for k, v := range map[string][3]string{
		"capBubble": {"Bubble", "氣泡", "气泡"},
	} {
		texts[k] = v
	}
}
