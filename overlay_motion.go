package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

const v2MorphDuration = 480 * time.Millisecond

// The approved HTML uses cubic-bezier(.32,0,.2,1). Solve its time axis,
// rather than using a cubic ease-out that makes large contractions start fast.
func v2MorphEase(x float32) float32 {
	return v2Bezier(x, .32, 0, .2, 1)
}
func v2Bezier(x, x1, y1, x2, y2 float32) float32 {
	if x <= 0 || x >= 1 {
		return min(1, max(0, x))
	}
	curve := func(t, a, b float32) float32 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi := float32(0), float32(1)
	for range 16 {
		mid := (lo + hi) / 2
		if curve(mid, x1, x2) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return curve((lo+hi)/2, y1, y2)
}

type v2Caption struct {
	text     string
	at       time.Time
	leaving  bool
	alpha, y float32 // the displayed values captured when interrupted
}

func (l v2Caption) sample(now time.Time) (float32, float32) {
	if l.leaving {
		t := min(1, max(0, float32(now.Sub(l.at)/time.Millisecond)/160))
		e := v2Bezier(t, .25, .1, .25, 1)
		return l.alpha * (1 - e), l.y - 4*e
	}
	t := min(1, max(0, float32(now.Sub(l.at)/time.Millisecond-80)/300))
	e := v2MorphEase(t)
	return e, 5 * (1 - e)
}

type v2MotionState struct {
	markReveal                                   [5]float32
	toolsReveal                                  float32
	captionKey                                   string
	captions                                     []v2Caption
	inline                                       string // outgoing words survive until their space has folded away
	inlineWidth                                  float32
	barKind                                      int
	barHeight                                    [5]float32
	markWeight                                   [5]float32
	bounds                                       ui.Rect
	labelWidth, liveWidth, markWidth, toolsWidth float32
	liveAlpha, toolsAlpha                        float32
	kind                                         int
	paintAt                                      time.Time
	changedAt                                    time.Time
}

func (m *v2MotionState) caption(key, text string, now time.Time, still bool) {
	if still {
		m.captionKey = key
		m.captions = []v2Caption{{text: text, at: now.Add(-time.Second)}}
		return
	}
	if key == m.captionKey {
		for i := range m.captions {
			if !m.captions[i].leaving {
				m.captions[i].text = text
			}
		}
		return
	}
	layers := make([]v2Caption, 0, 4)
	for _, old := range m.captions {
		a, y := old.sample(now)
		if a > .01 {
			layers = append(layers, v2Caption{text: old.text, at: now, leaving: true, alpha: a, y: y})
		}
	}
	m.captionKey = key
	m.changedAt = now
	m.captions = append(layers, v2Caption{text: text, at: now})
}

func (o *overlay) v2Sending() bool {
	p := o.a.sendPreview
	return p != nil && (p.visible || p.exiting)
}
func (p *sendPreviewWindow) remaining() float64 {
	if p.exiting {
		return p.exitRemain
	}
	return max(0, time.Until(p.end).Seconds())
}

// One persistent subtree draws both dictation and auto-send. The engine retains
// each animation's displayed value when a new target interrupts it.
func (o *overlay) v2SendTools(c *ui.Context, width, alpha, reveal float32) {
	p := o.a.sendPreview
	active := p != nil && p.visible && p.pending
	ui.Box(c.Key("v2sendtools")).Size(width, 32).Shrink(0).Clip().Opacity(alpha).
		Translate(6*(1-reveal), 0).Scale(.92+.08*reveal, .92+.08*reveal).Children(func() {
		ui.Row(c).Size(66, 32).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c.Key("v2sendesc")).Size(26, 20).Radius(4).
				Border(1, ui.RGBA(255, 255, 255, .19)).Draw(func(painter *ui.Painter, r ui.Rect) {
				span := ui.Span{Text: "Esc", Size: 10, Weight: 400, Color: ui.Hex("#bec7e6")}
				w, h := painter.MeasureText(0, span)
				painter.RichText(r.X+(r.W-w)/2, r.Y+(r.H-h)/2, 0, span)
			})
			b := ui.ButtonBase(c.Key("v2cancel-send")).Size(32, 32).Radius(16).FocusRing(false).
				Label(T("sendCancel")).Disabled(!active)
			if active {
				b.Cursor(ui.CursorPointer)
			}
			b.Draw(func(painter *ui.Painter, r ui.Rect) {
				cx, cy := r.X+r.W/2, r.Y+r.H/2
				fraction := float32(1)
				if p != nil && p.duration > 0 {
					fraction = min(1, float32(p.remaining()/p.duration.Seconds()))
				}
				for _, part := range []struct {
					f   float32
					col ui.Color
				}{{1, ui.RGBA(255, 255, 255, .2)}, {fraction, v2Mint}} {
					path := &ui.Path{}
					for i := 0; i <= 64; i++ {
						a := -math.Pi/2 + 2*math.Pi*float64(part.f)*float64(i)/64
						x, y := cx+13*float32(math.Cos(a)), cy+13*float32(math.Sin(a))
						if i == 0 {
							path.MoveTo(x, y)
						} else {
							path.LineTo(x, y)
						}
					}
					painter.StrokePath(path, 1.6, part.col)
				}
				cross := (&ui.Path{}).MoveTo(cx-4, cy-4).LineTo(cx+4, cy+4).MoveTo(cx+4, cy-4).LineTo(cx-4, cy+4)
				painter.StrokePath(cross, 1.6, v2Ink)
			})
			if active && b.Clicked() {
				p.close()
			}
		})
	})
}

func (m *v2MotionState) drawCaption(p *ui.Painter, r ui.Rect, now time.Time) {
	p.Clip(r, 0, func() {
		for _, layer := range m.captions {
			a, y := layer.sample(now)
			if a < .001 {
				continue
			}
			span := ui.Span{Text: layer.text, Size: v2FontSize, Weight: 500, Color: v2Ink.Alpha(a)}
			_, h := p.MeasureText(0, span)
			p.RichText(r.X, r.Y+(r.H-h)/2+y, 0, span)
		}
	})
}

func (o *overlay) v2Caption(kind int) (key, text string, widthText string) {
	if o.v2Sending() {
		return T("sendCountdown"), fmt.Sprintf(T("sendCountdown"), o.a.sendPreview.remaining()), fmt.Sprintf(T("sendCountdown"), 0.0)
	}
	text = T(v2Labels[kind])
	if kind == v2Cleaning && o.v.TranslationLanguage != "" {
		text = T("translationWorking")
	}
	return text, text, text
}
