package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"voiceinput/internal/macos"
)

const permissionGlow = 24
const permissionCardHeight = 72
const permissionPanelHeight = permissionCardHeight + 2*permissionGlow

var permissionClose = icon(`<path d="M4 4l8 8M12 4l-8 8"/>`)
var permissionGrip = icon(`<g fill="currentColor" stroke="none"><circle cx="6" cy="4" r="1"/><circle cx="10" cy="4" r="1"/><circle cx="6" cy="8" r="1"/><circle cx="10" cy="8" r="1"/><circle cx="6" cy="12" r="1"/><circle cx="10" cy="12" r="1"/></g>`)

// Both settings and onboarding use this single nonactivating native panel.
func (p *prefsWindow) showPermissionDrag(pane string) {
	if pane != "Privacy_Accessibility" && pane != "Privacy_ListenEvent" {
		return
	}
	p.hidePermissionDrag()
	macos.OpenPrivacySettings(pane)
	p.dragPane, p.dragActive, p.dragging, p.dragError = pane, true, false, false
	p.dragStarted, p.dragSeen, p.dragChecked, p.dragGranted = time.Now(), time.Time{}, time.Time{}, time.Time{}
	if p.dragWin == nil {
		p.dragWin = mygo.NewFlyout(mygo.FlyoutOptions{Tray: p.a.tray, Width: 592, Height: permissionPanelHeight, Hidden: true, Content: ui.View(p.permissionDragView)})
		p.dragWin.SetAlwaysOnTop(true)
		p.dragWin.OnClose(func(e *mygo.CloseEvent) {
			if !p.a.quitting {
				e.PreventDefault()
				p.hidePermissionDrag()
			}
		})
	}
	p.dragWin.SetIgnoreMouseEvents(false)
	// Do not show an unanchored card while System Settings is still opening.
	p.trackPermissionWindow()
	p.dragWin.Invalidate()
	// Native dragging runs in AppKit's tracking loop. Do not rely on the
	// source view drawing frames to observe release, cancellation or closure.
	generation := p.dragGeneration
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			done := false
			macos.OnMain(func() {
				if !p.dragActive || p.dragGeneration != generation || p.a.quitting {
					done = true
					return
				}
				p.pollPermissionDrag()
				done = !p.dragActive
			})
			if done {
				return
			}
		}
	}()
}

func (p *prefsWindow) hidePermissionDrag() {
	// Fence callbacks before hiding the old native source.
	p.dragGeneration++
	p.dragSession++
	p.dragActive, p.dragging = false, false
	p.dragReleased = time.Time{}
	if p.dragWin != nil {
		macos.HideAppFileDrag(p.dragWin.NativeHandle())
		p.dragWin.SetIgnoreMouseEvents(false)
		p.dragWin.Hide()
	}
}

func (p *prefsWindow) pollPermissionDrag() {
	now := time.Now()
	if p.dragging {
		if macos.PrimaryMouseDown() {
			p.dragReleased = time.Time{}
		} else if p.dragReleased.IsZero() {
			p.dragReleased = now
		} else if now.Sub(p.dragReleased) >= 500*time.Millisecond {
			// A lost completion must not strand a faded, unclickable card.
			p.dragSession++
			macos.HideAppFileDrag(p.dragWin.NativeHandle())
			p.dragging, p.dragError = false, true
			p.dragReleased = time.Time{}
			p.dragChecked = time.Time{}
		}
	}
	p.trackPermissionWindow()
	if !p.dragActive {
		return
	}
	if now.Sub(p.dragChecked) >= time.Second && !p.dragging {
		p.dragChecked = now
		granted := p.dragPane == "Privacy_Accessibility" && macos.AccessibilityTrusted(false) || p.dragPane == "Privacy_ListenEvent" && macos.InputMonitoringGranted(false)
		if granted && p.dragGranted.IsZero() {
			p.dragGranted = now
			p.a.refreshAll()
		}
	}
	if !p.dragGranted.IsZero() && now.Sub(p.dragGranted) > 900*time.Millisecond {
		p.hidePermissionDrag()
		return
	}
	p.dragWin.Invalidate()
}

// Keep geometry work bounded to 5 Hz. A missing frame is tolerated briefly
// during pane transitions; closing/minimizing Settings removes the helper.
func (p *prefsWindow) trackPermissionWindow() {
	f, ok := macos.SystemSettingsFrame()
	now := time.Now()
	if !ok {
		if (!p.dragSeen.IsZero() && now.Sub(p.dragSeen) > 1500*time.Millisecond) || (p.dragSeen.IsZero() && now.Sub(p.dragStarted) > 8*time.Second) {
			p.hidePermissionDrag()
		}
		return
	}
	p.dragSeen = now
	area := mygo.Screen.DisplayNearestPoint(mygo.Point{X: f.X + f.Width/2, Y: f.Y + f.Height/2}).WorkArea
	width := min(max(f.Width-216, 440), 720, area.Width-2*permissionGlow-16)
	// Coordinates below refer to the opaque card, not its transparent glow margin.
	x, y := f.X+f.Width-width, f.Y+f.Height+10
	p.dragDirection = 0 // arrow points up, toward the list
	if y+permissionCardHeight+permissionGlow > area.Y+area.Height {
		switch {
		case f.X+f.Width+10+width+permissionGlow <= area.X+area.Width:
			x, y, p.dragDirection = f.X+f.Width+10, f.Y+f.Height-permissionCardHeight, 1 // left
		case f.X-10-width-permissionGlow >= area.X:
			x, y, p.dragDirection = f.X-10-width, f.Y+f.Height-permissionCardHeight, 2 // right
		case f.Y-10-permissionCardHeight-permissionGlow >= area.Y:
			y, p.dragDirection = f.Y-10-permissionCardHeight, 3 // down
		default:
			// Maximized settings leaves no exterior space: stay on-screen at its bottom.
			y = area.Y + area.Height - permissionCardHeight - permissionGlow - 8
		}
	}
	x = max(area.X+permissionGlow+8, min(x, area.X+area.Width-width-permissionGlow-8))
	y = max(area.Y+permissionGlow+8, min(y, area.Y+area.Height-permissionCardHeight-permissionGlow-8))
	bounds := mygo.Rectangle{X: x - permissionGlow, Y: y - permissionGlow, Width: width + 2*permissionGlow, Height: permissionPanelHeight}
	if !p.dragWin.IsVisible() {
		p.dragWin.ShowInactive()
		p.dragBounds = mygo.Rectangle{}
	}
	if p.dragBounds != bounds && !p.dragging {
		p.dragWin.SetBounds(bounds)
		p.dragBounds = bounds
	}
}

func (p *prefsWindow) permissionDragView(c *ui.Context) {
	useTheme(c)
	c.Root().Background(ui.Transparent)
	if !p.dragActive {
		return
	}
	now := time.Now()
	c.After(50 * time.Millisecond)
	t := c.Theme()
	pulse := float32(.85)
	if !c.Preferences().ReduceMotion {
		pulse = .875 + .125*float32(math.Sin(now.Sub(p.dragStarted).Seconds()*2*math.Pi/4.8))
	}
	alpha := float32(1)
	if p.dragging {
		alpha = .38
	}
	ui.Box(c.Key("permission-shell")).FillWidth().Height(permissionPanelHeight).Padding(permissionGlow).Children(func() {
		ui.Row(c.Key("permission-card")).FillWidth().Height(permissionCardHeight).Gap(12).Padding(10, 12).AlignItems(ui.Center).Opacity(alpha).
			Label(T("permDragApp")).Draw(func(painter *ui.Painter, r ui.Rect) {
			indigo, mint := ui.RGBA(32, 40, 79, .38*pulse), ui.RGBA(105, 231, 195, .48*pulse)
			if t.Dark {
				indigo, mint = ui.RGBA(113, 135, 234, .38*pulse), ui.RGBA(105, 231, 195, .38*pulse)
			}
			painter.Shadow(r, 12, -8, 2, 22, 0, indigo)
			painter.Shadow(r, 12, 8, 3, 24, 0, mint)
			painter.Fill(r, cardColor(t), 12)
			if c.Preferences().HighContrast {
				painter.Stroke(r, t.Text, 12, 1)
			}
		}).Children(func() {
			if !p.dragGranted.IsZero() {
				macos.HideAppFileDrag(p.dragWin.NativeHandle())
				ui.Icon(c, icCheck).FontSize(24).TextColor(t.Success)
				ui.Text(c, T("permDragGranted")).Grow(1).FontSize(13).FontWeight(600)
			} else {
				ui.Box(c).Size(32, 36).Draw(func(painter *ui.Painter, r ui.Rect) {
					color := pick(t, "#20284f", "#a9b8ff")
					cx, cy := r.X+r.W/2, r.Y+r.H/2
					var path ui.Path
					// Rotate the same unboxed arrow toward the tracked Settings window.
					point := func(x, y float32) (float32, float32) {
						switch p.dragDirection {
						case 1:
							x, y = y, -x
						case 2:
							x, y = -y, x
						case 3:
							x, y = -x, -y
						}
						return cx + x, cy + y
					}
					x, y := point(0, 11)
					path.MoveTo(x, y)
					x, y = point(0, -11)
					path.LineTo(x, y)
					x, y = point(-8, -3)
					path.MoveTo(x, y)
					x, y = point(0, -11)
					path.LineTo(x, y)
					x, y = point(8, -3)
					path.LineTo(x, y)
					painter.StrokePath(&path, 2.5, color)
				})
				hint := "permDragHint"
				if p.dragDirection != 0 {
					hint = "permDragSide"
				}
				if p.bundlePath == "" {
					hint = "permDragUnavailable"
				} else if p.dragError {
					hint = "permDragFailed"
				}
				ui.Text(c, T(hint)).Grow(1).MinWidth(0).FontSize(13).FontWeight(600)
				if p.bundlePath != "" {
					generation, session := p.dragGeneration, p.dragSession+1
					ui.Row(c.Key("permission-app")).Shrink(0).Gap(10).Padding(7).Radius(10).Background(cardColor(t)).Border(1, t.Border).AlignItems(ui.Center).Label(T("permDragApp")).
						Draw(func(_ *ui.Painter, r ui.Rect) {
							err := macos.SetAppFileDrag(p.dragWin.NativeHandle(), p.bundlePath, float64(r.X), float64(r.Y), float64(r.W), float64(r.H), func() {
								p.dragSession = session
								p.dragging, p.dragError = true, false
								p.dragReleased = time.Time{}
								p.dragWin.Invalidate()
							}, func(copied bool) {
								p.dragWin.Update(func() {
									if !p.dragActive || generation != p.dragGeneration || session != p.dragSession {
										return
									}
									p.dragging = false
									p.dragReleased = time.Time{}
									if copied {
										p.hidePermissionDrag()
										p.a.refreshAll()
										return
									}
									p.dragChecked = time.Time{}
									p.dragWin.Invalidate()
								})
							})
							if err != nil {
								p.dragError = true
							}
						}).Children(func() {
						ui.Box(c).Size(30, 30).Draw(drawAppMark)
						ui.Text(c, "Voicemore").FontSize(13).FontWeight(600).SingleLine()
						ui.Icon(c, permissionGrip).FontSize(16).TextColor(t.TextMuted)
					})
				}
			}
			close := ui.ButtonBase(c.Key("permission-close")).Size(24, 24).Radius(6).Label(T("close"))
			if close.Hovered() {
				close.Background(t.SurfaceHover)
			}
			close.Children(func() { ui.Icon(c, permissionClose).FontSize(12).TextColor(t.TextMuted) })
			if close.Clicked() {
				p.hidePermissionDrag()
			}
		})
	})
}
