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
// waits on one buffered decision; editing/cancel and expiry cannot both win.
type sendPreviewWindow struct {
	a                        *app
	win                      *mygo.Window
	visible, pending         bool
	draft, expected, applied string
	offset                   int
	writable                 bool
	target                   *macos.Target
	end                      time.Time
	duration                 time.Duration
	decision                 chan bool
	generation               uint64
	area                     mygo.Rectangle
	panelHeight              int
	guardValue               string
	guardReadable            bool
	onEdit                   func(string)
	lastEscape               time.Time
}

func newSendPreview(a *app) *sendPreviewWindow {
	p := &sendPreviewWindow{a: a}
	p.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Width: 392, Height: 240, Hidden: true, Focusable: true, Content: ui.View(p.view)})
	p.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			p.resolve(false)
		}
	})
	return p
}
func (a *app) cancelInput() {
	mygo.RunOnMain(func() {
		if a.sendPreview != nil && a.sendPreview.visible {
			if time.Since(a.sendPreview.lastEscape) < 150*time.Millisecond {
				return
			}
			a.sendPreview.lastEscape = time.Now()
			if a.sendPreview.pending {
				a.sendPreview.resolve(false)
			} else {
				a.sendPreview.close()
			}
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
	p.resolve(false)
	p.visible = false
	p.generation++
	p.win.Hide()
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
		p.expected, p.offset, p.writable = target.EditableSnapshot(text)
		p.guardValue, p.guardReadable = target.ReadValue()
		p.draft, p.applied = text, text
		p.onEdit = onEdit
		p.decision = decision
		p.pending = true
		p.visible = true
		p.duration = delay
		p.end = time.Now().Add(delay)
		p.area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
		p.panelHeight = 0
		p.a.overlay.win.Hide()
		macos.ConfigureOverlay(p.win.NativeHandle())
		p.place(56)
		p.win.ShowInactive()
		gen := p.generation
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
	time.AfterFunc(20*time.Millisecond, func() {
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
				if !p.target.StillFocused() || (p.guardReadable && (!readable || value != p.guardValue)) {
					p.resolve(false)
				} else if !time.Now().Before(p.end) {
					p.resolve(true)
					return
				}
			}
			if p.pending {
				p.poll(ctx, gen)
			}
		})
	})
}
func (p *sendPreviewWindow) place(panel int) {
	if panel == p.panelHeight {
		return
	}
	p.panelHeight = panel
	h := panel + 16 + 40 + 16
	offset := max(0, p.a.settings.Get().OverlayBottomOffset)
	p.win.SetBounds(mygo.Rectangle{X: p.area.X + (p.area.Width-392)/2, Y: p.area.Y + p.area.Height - h - offset, Width: 392, Height: h})
}
func (p *sendPreviewWindow) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	c.OnShortcut(0, ui.KeyEscape, p.a.cancelInput)
	if !p.visible {
		return
	}
	theme := *c.Theme()
	theme.Dark = true
	theme.Text = ui.Hex("#FAFAFA")
	theme.TextMuted = ui.Hex("#A3A3A3")
	theme.Background = ui.Hex("#1B1B1B")
	theme.Surface = ui.Transparent
	theme.Border = ui.Transparent
	theme.ScrollbarWidth = 3
	theme.Scrollbar = ui.RGBA(255, 255, 255, .25)
	c.SetTheme(&theme)
	_, full := c.MeasureText(328, ui.Span{Text: p.draft + "\u200b", Size: 14})
	_, line := c.MeasureText(0, ui.Span{Text: "国Ag", Size: 14})
	h := int(math.Ceil(float64(min(max(full, line), line*5)))) + 32
	p.place(h)
	ui.Column(c).Fill().Padding(0, 16, 16, 16).Gap(16).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key("text-panel")).Width(360).Height(float32(h)).Padding(16).Radius(16).Background(ui.Hex("#1B1B1B")).Border(1, ui.RGBA(255, 255, 255, .10)).Children(func() {
			area := ui.TextArea(c.Key("final-text"), &p.draft).Label(T("sendFinalText")).Lines(1, 5).FillWidth().Height(float32(h - 32)).Padding(0).FontSize(14).Background(ui.Transparent).BorderWidth(0)
			if (area.Pressed() || area.Changed()) && p.pending {
				p.resolve(false)
			}
			if area.Changed() && !area.Composing() {
				if p.writable && p.target != nil {
					next := p.expected[:p.offset] + p.draft + p.expected[p.offset+len(p.applied):]
					if p.target.ReplaceValue(p.expected, next) {
						p.expected = next
						p.applied = p.draft
						if p.onEdit != nil {
							p.onEdit(p.draft)
						}
					} else {
						p.writable = false
					}
				}
			}
		})
		// The slot remains after cancellation; editing cannot shift the panel down.
		ui.Row(c.Key("send-slot")).Height(40).AlignItems(ui.Center).Children(func() {
			if !p.pending {
				if p.draft != p.applied {
					if ui.Button(c.Key("copy-edit"), T("copy")).Label(T("sendCopyEdit")).Height(32).Clicked() {
						mygo.Clipboard.WriteText(p.draft)
					}
				}
				return
			}
			remain := max(0, time.Until(p.end).Seconds())
			fraction := float32(remain / p.duration.Seconds())
			ui.Row(c.Key("send-pill")).Height(40).Padding(0, 6, 0, 18).Gap(12).Radius(20).Background(ui.Hex("#293365")).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(T("sendCountdown"), remain)).FontSize(12).TextColor(ui.Hex("#F7FAFF"))
				ui.Text(c, "Esc").FontSize(10).TextColor(ui.Hex("#BEC7E6"))
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
					painter.Line(cx-4, cy-4, cx+4, cy+4, 1.6, ui.Hex("#FAFAFA"))
					painter.Line(cx+4, cy-4, cx-4, cy+4, 1.6, ui.Hex("#FAFAFA"))
				})
				if b.Clicked() {
					p.resolve(false)
				}
			})
		})
	})
	if p.pending {
		c.After(16 * time.Millisecond)
	}
}
