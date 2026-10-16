package main

import (
	"context"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"math"
	"time"
	"voiceinput/internal/macos"
)

// All mutable preview state belongs to the UI thread. The insertion goroutine
// waits on one buffered decision; cancellation and expiry cannot both win.
type sendPreviewWindow struct {
	a                *app
	win              *mygo.Window
	visible, pending bool
	draft            string
	target           *macos.Target
	end              time.Time
	duration         time.Duration
	decision         chan bool
	generation       uint64
	guardValue       string
	guardReadable    bool
	lastEscape       time.Time
	stopOutside      func()
}

func newSendPreview(a *app) *sendPreviewWindow {
	p := &sendPreviewWindow{a: a, win: a.overlay.win}
	return p
}
func (a *app) cancelInput() {
	mygo.RunOnMain(func() {
		if a.sendPreview != nil && a.sendPreview.visible {
			if time.Since(a.sendPreview.lastEscape) < 150*time.Millisecond {
				return
			}
			a.sendPreview.lastEscape = time.Now()
			a.sendPreview.close()
			return
		}
		a.ctrl.Cancel()
	})
}
func (p *sendPreviewWindow) resolve(send bool) {
	if !p.pending {
		return
	}
	p.pending = false
	p.decision <- send
	if send {
		p.close()
	}
}
func (p *sendPreviewWindow) close() {
	if p.stopOutside != nil {
		p.stopOutside()
		p.stopOutside = nil
	}
	wasVisible := p.visible
	p.resolve(false)
	p.visible = false
	p.generation++
	if wasVisible {
		p.a.overlay.sendFinished = true
		p.a.overlay.followID++
		p.win.Hide()
	}
	if p.target != nil {
		p.target.Release()
		p.target = nil
	}
}

func (p *sendPreviewWindow) wait(ctx context.Context, text string, target *macos.Target, delay time.Duration, onEdit func(string)) bool {
	decision := make(chan bool, 1)
	p.win.Update(func() {
		p.close()
		if ctx.Err() != nil {
			decision <- false
			return
		}
		p.target = target.Retain()
		p.guardValue, p.guardReadable = target.ReadValue()
		p.draft = text
		p.decision = decision
		p.pending = true
		p.visible = true
		p.duration = delay
		p.end = time.Now().Add(delay)
		o := p.a.overlay
		o.sendFinished = false
		// Keep the existing window, bounds and bottom anchor throughout the run.
		if !p.win.IsVisible() {
			o.area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
			o.expanded = false
			o.place()
			macos.ConfigureOverlay(p.win.NativeHandle())
			p.win.ShowInactive()
		}
		gen := p.generation
		p.stopOutside = macos.WatchOutsideClicks(p.win.NativeHandle(), func() {
			p.win.Update(func() {
				if p.visible && p.generation == gen {
					p.close()
				}
			})
		})
		p.poll(ctx, gen)
	})
	select {
	case send := <-decision:
		return send
	case <-ctx.Done():
		p.win.Update(func() {
			if p.decision == decision {
				p.close()
			}
		})
		return false
	}
}
func (p *sendPreviewWindow) poll(ctx context.Context, gen uint64) {
	interval := 20 * time.Millisecond
	if !p.pending {
		interval = 100 * time.Millisecond
	}
	time.AfterFunc(interval, func() {
		p.win.Update(func() {
			if gen != p.generation || !p.visible {
				return
			}
			if p.pending && ctx.Err() != nil {
				p.close()
				return
			}
			if p.pending {
				value, readable := p.target.ReadValue()
				if !p.target.StillFocused() {
					p.close()
					return
				} else if p.guardReadable && (!readable || value != p.guardValue) {
					p.close()
					return
				} else if !time.Now().Before(p.end) {
					p.resolve(true)
					return
				}
			}
			if !p.pending && p.target != nil && !p.target.StillSelected() {
				p.close()
				return
			}
			p.poll(ctx, gen)
		})
	})
}
func (p *sendPreviewWindow) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	c.OnShortcut(0, ui.KeyEscape, p.a.cancelInput)
	if !p.visible {
		return
	}
	theme := *c.Theme()
	theme.Dark = true
	theme.Font = "PingFang SC"
	theme.Text = ui.Hex("#FAFAFA")
	theme.TextMuted = ui.Hex("#A3A3A3")
	// SetTheme also paints the window root: only the card may be opaque.
	theme.Background = ui.Transparent
	theme.Surface = ui.Transparent
	theme.Border = ui.Transparent
	theme.ScrollbarWidth = 3
	theme.Scrollbar = ui.RGBA(255, 255, 255, .25)
	c.SetTheme(&theme)
	c.Root().Background(ui.Transparent).FontWeight(400)
	ui.Column(c).Fill().Padding(0, 0, float32(p.a.overlay.room), 0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("send-slot")).Height(40).AlignItems(ui.Center).Children(func() {
			remain := max(0, time.Until(p.end).Seconds())
			fraction := float32(remain / p.duration.Seconds())
			ui.Row(c.Key("send-pill")).Height(40).Padding(0, 6, 0, 18).Gap(12).Radius(20).Gradient(ui.Hex("#343B85"), ui.Hex("#101B35"), 135).InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, .11)).Shadow(0, 10, 24, -8, ui.RGBA(20, 27, 56, .44)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(T("sendCountdown"), remain)).FontSize(12).TextColor(ui.Hex("#F7FAFF"))
				ui.Text(c, "Esc").FontSize(10).FontWeight(400).Padding(1, 4).Radius(4).Border(1, ui.RGBA(255, 255, 255, .19)).TextColor(ui.Hex("#BEC7E6"))
				b := ui.ButtonBase(c.Key("cancel-send")).Size(32, 32).Radius(16).Label(T("sendCancel"))
				b.Draw(func(painter *ui.Painter, r ui.Rect) {
					cx, cy := r.X+r.W/2, r.Y+r.H/2
					for _, part := range []struct {
						f     float32
						color ui.Color
					}{{1, ui.RGBA(255, 255, 255, .2)}, {fraction, ui.Hex("#9CEBD5")}} {
						path := &ui.Path{}
						for i := 0; i <= 64; i++ {
							angle := -math.Pi/2 + 2*math.Pi*float64(part.f)*float64(i)/64
							x, y := cx+13*float32(math.Cos(angle)), cy+13*float32(math.Sin(angle))
							if i == 0 {
								path.MoveTo(x, y)
							} else {
								path.LineTo(x, y)
							}
						}
						painter.StrokePath(path, 1.6, part.color)
					}
					cross := &ui.Path{}
					cross.MoveTo(cx-4, cy-4)
					cross.LineTo(cx+4, cy+4)
					cross.MoveTo(cx+4, cy-4)
					cross.LineTo(cx-4, cy+4)
					painter.StrokePath(cross, 1.6, ui.Hex("#FAFAFA"))
				})
				if b.Clicked() {
					p.close()
				}
			})
		})
	})
	if p.pending {
		c.After(16 * time.Millisecond)
	}
}
