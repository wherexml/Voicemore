package main

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/store"
)

// The home page says what to press, shows what is set up and lists the
// latest dictations. Everything on it is the app's real state: the
// trigger of the settings, the default models as they can serve now, the
// permissions as macOS reports them, and the records of the history.

var keyNames = map[string]string{
	macos.KeyRightOption: "rOption", macos.KeyRightCommand: "rCommand", macos.KeyRightControl: "rControl",
}

// keyName is a trigger key as the interface names it.
func keyName(key string) string {
	if key == macos.KeyFn {
		return "fn"
	}
	if k, ok := keyNames[key]; ok {
		symbol := map[string]string{macos.KeyRightOption: "⌥", macos.KeyRightCommand: "⌘", macos.KeyRightControl: "⌃"}[key]
		return symbol + " " + T(k)
	}
	return key
}

// modelLabel is the default model of a kind in words: its name when it can
// serve, and otherwise why it cannot.
func (a *app) modelLabel(kind string, s config.Settings) string {
	err := providers.Check(kind, s, a.secret)
	switch err {
	case nil:
		return providers.ModelName(kind, s)
	case providers.ErrNotSet:
		return T("notSet")
	}
	return T("unconfigured", providerName(providers.Assigned(kind, s).Provider, s, kind))
}

// providerName is a provider as the interface names it.
func providerName(id string, s config.Settings, kind ...string) string {
	if id == providers.Local && len(kind) > 0 && kind[0] == providers.KindASR {
		return T("builtinAsr")
	}
	id = providers.Brand(id, s)
	if id == providers.Local {
		return "oMLX"
	}
	if e, ok := providers.Lookup(id); ok {
		return T(e.Name)
	}
	if c, ok := providers.Custom(id, s); ok {
		return c.Name
	}
	return id
}

func (m *mainWindow) homeView(c *ui.Context) {
	t := c.Theme()
	s := m.a.settings.Get()
	key := keyName(s.TriggerKey)
	ui.Scroll(c).Fill().Padding(0, 28, 20).AlignItems(ui.Center).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(860).Children(func() {
			// Dictation is the page: the key, what pressing it brings up,
			// and one sentence. No frame; the space around it is the emphasis.
			ui.Column(c.Key("hero")).FillWidth().AlignItems(ui.Center).Padding(64, 16, 60).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
					keycap(c, key, 60, 18)
					ui.Box(c).Size(34, 4).Draw(drawDottedLink)
					// A still picture of the pill that appears while speaking.
					ui.Box(c).Size(146, 34).Draw(drawPillPreview)
				})
				ui.Box(c).Height(34)
				ui.Text(c, T("homeReady", key)).FontSize(34).FontWeight(600)
				ui.Box(c).Height(14)
				hint := "homeToggle"
				if s.TriggerMode == config.TriggerHold {
					hint = "homeHold"
				}
				ui.Text(c, T(hint)).FontSize(15).TextColor(softText(t)).MaxWidth(480)
			})

			// Setup and Recent, side by side: plain rows under one rule each,
			// four to a column and of one height, so the columns end together.
			now := time.Now()
			c.After(untilNextMinute(now)) // the time labels turn with the minute
			records := m.a.history.List("")
			ui.Row(c).Gap(44).AlignItems(ui.Start).Children(func() {
				// What is set up; each row opens its part of the settings.
				ui.Column(c.Key("setup")).Grow(1).Basis(0).MinWidth(0).Children(func() {
					sectionHead(c, T("homeSetup"), nil)
					mode := "mToggle"
					if s.TriggerMode == config.TriggerHold {
						mode = "mHold"
					}
					m.setupRow(c, "shortcut", icKeyboard, T("hShortcut"), key+" · "+T(mode), "general", nil)
					ui.Divider(c)
					m.setupRow(c, "cleanup", icSparkle, T("cleanup"), "", "models", func() {
						m.a.cleanupSwitch(c)
					})
					ui.Divider(c)
					m.setupRow(c, "asr", icMic, T("mAsr"), m.a.modelLabel(providers.KindASR, s), "models", nil)
					ui.Divider(c)
					perms := T("granted")
					if macos.MicrophoneStatus() != "authorized" || !macos.AccessibilityTrusted(false) || !macos.InputMonitoringGranted(false) {
						perms = T("notGranted")
					}
					m.setupRow(c, "perms", icShield, T("gPerms"), perms, "perms", nil)
				})

				// The latest dictations, which open in History.
				ui.Column(c.Key("recent")).Grow(1).Basis(0).MinWidth(0).Children(func() {
					sectionHead(c, T("homeRecent"), func() {
						if len(records) > 0 {
							all := ui.ButtonBase(c.Key("all")).Label(T("showAll"))
							all.Children(func() { ui.Text(c, T("showAll")).FontSize(12).FontWeight(500).TextColor(softText(t)) })
							if all.Clicked() {
								m.goTo("history")
							}
						}
					})
					if len(records) == 0 {
						ui.Text(c, T("homeEmpty")).TextColor(t.TextMuted).Padding(14, 2)
					}
					for i, r := range records[:min(len(m.recentOpen), len(records))] {
						if i > 0 {
							ui.Divider(c)
						}
						m.recentRow(c, i, r, now)
					}
				})
			})
		})
	})
}

// sectionHead is the heading of one of Home's two lists: a small label
// over a rule, with an optional action at its right.
func sectionHead(c *ui.Context, title string, action func()) {
	t := c.Theme()
	ui.Row(c).MinHeight(28).Padding(0, 2, 6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(strongLine(t)).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(t.TextMuted).Grow(1)
		if action != nil {
			action()
		}
	})
}

// keycap draws a key as the interface shows it: a raised tile with a
// heavier lower edge.
func keycap(c *ui.Context, key string, height, font float32) {
	t := c.Theme()
	if len([]rune(key)) > 3 { // a named key, as "Right Option", reads smaller
		font *= 0.76
	}
	ui.Row(c).Height(height).MinWidth(height+4).Padding(0, height*0.3).Radius(height*0.25).Justify(ui.Center).AlignItems(ui.Center).
		Background(cardColor(t)).BorderWidth(1, 1, 3, 1).BorderColor(strongLine(t)).Children(func() {
		ui.Text(c, key).FontSize(font).FontWeight(700).SingleLine()
	})
}

// drawDottedLink joins the keycap and the pill: a row of dots.
func drawDottedLink(p *ui.Painter, r ui.Rect) {
	const d, gap = 2, 4
	for x := r.X; x+d <= r.X+r.W; x += d + gap {
		p.Fill(ui.Rect{X: x, Y: r.Y + (r.H-d)/2, W: d, H: d}, ui.RGBA(128, 127, 120, 0.7), d/2)
	}
}

// setupRow is a line of the setup list: an icon, a name, its value, an
// optional control, and a chevron into the settings.
func (m *mainWindow) setupRow(c *ui.Context, key string, svg *ui.SVG, label, value, category string, control func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(12).Height(46).Padding(0, 2).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, svg).FontSize(16).TextColor(t.TextMuted)
		ui.Text(c, label).SingleLine()
		ui.Spacer(c)
		if value != "" {
			ui.Text(c, value).FontSize(12.5).TextColor(softText(t)).MinWidth(0).SingleLine()
		}
		if control != nil {
			control()
		}
		open := ui.ButtonBase(c.Key("open")).Label(T("openIn", label)).Tooltip(T("openIn", label)).
			Size(26, 26).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icChevron).FontSize(14).TextColor(t.TextMuted)
		})
		if open.Clicked() {
			m.a.prefs.open(category)
		}
	})
}

// recentRow is one of the latest dictations on Home: its text, which opens
// it in History, and at the right one slot of a fixed width that shows the
// time, or, while the pointer or the keyboard is on the row, a button that
// copies the text shown. The two are separate buttons side by side.
func (m *mainWindow) recentRow(c *ui.Context, i int, r store.Record, now time.Time) {
	t := c.Theme()
	const feedbackDuration = 1500 * time.Millisecond
	for id, at := range m.recentCopied {
		if now.Sub(at) >= feedbackDuration {
			delete(m.recentCopied, id)
		}
	}
	copiedAt, copied := m.recentCopied[r.ID]
	if copied {
		c.After(feedbackDuration - now.Sub(copiedAt))
	}
	text, _ := primaryText(r)
	shown := text
	if shown == "" {
		shown = T("emptyText")
	}
	ui.Row(c.Key(r.ID)).Gap(10).Height(46).Padding(0, 2).AlignItems(ui.Center).Children(func() {
		open := ui.ButtonBase(c.Key("open")).Grow(1).MinWidth(0).Height(46).Justify(ui.Start).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Label(shown).Bind(&m.recentOpen[i])
		open.Children(func() { ui.Text(c, shown).FontSize(13.5).Grow(1).SingleLine() })
		if open.Clicked() {
			m.showRecord(r.ID)
		}
		slot := ui.ButtonBase(c.Key("copy")).Width(108).Height(28).Radius(6).Justify(ui.End).AlignItems(ui.Center).
			Label(T("copyOf", shown)).Tooltip(T("copy")).Bind(&m.recentCopy[i])
		active := text != "" && (open.Hovered() || slot.Hovered() || m.recentOpen[i].Focused(c) || m.recentCopy[i].Focused(c))
		if copied {
			slot.Label(T("copied")).Tooltip(T("copied"))
		}
		slot.Children(func() {
			if copied {
				// A small ease-out pop, inside a fixed slot so the row never shifts.
				size := float32(14)
				if elapsed := now.Sub(copiedAt); elapsed < 180*time.Millisecond && !c.Preferences().ReduceMotion {
					progress := max(float32(0), float32(elapsed)/float32(180*time.Millisecond))
					size -= 4 * (1 - progress) * (1 - progress)
					c.After(16 * time.Millisecond)
				}
				ui.Box(c).Size(14, 14).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, icCheck).FontSize(size).TextColor(t.Success)
				})
			} else if active {
				ui.Icon(c, icCopy).FontSize(14).TextColor(softText(t))
			} else {
				ui.Text(c, timeLabel(r.StartedAt, now)).FontSize(11.5).TextColor(faintText(t)).SingleLine()
			}
		})
		if slot.Clicked() && text != "" {
			// Copies what the row shows, and stays on Home.
			mygo.Clipboard.WriteText(text)
			if m.recentCopied == nil {
				m.recentCopied = make(map[string]time.Time)
			}
			m.recentCopied[r.ID] = time.Now()
			c.After(0)
		}
	})
}

// drawPillPreview paints the recording pill at rest: charcoal with a
// hairline edge, small end controls and a short waveform in the middle.
func drawPillPreview(p *ui.Painter, r ui.Rect) {
	charcoal := ui.RGB(42, 44, 50)
	p.Fill(r, ui.RGBA(20, 22, 28, 0.5), r.H/2)
	p.Fill(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, charcoal, (r.H-1)/2)
	cy := r.Y + r.H/2
	// Cancel: a faint circle with an X of two diagonal strokes.
	lx := r.X + 6 + 11
	p.Fill(ui.Rect{X: lx - 11, Y: cy - 11, W: 22, H: 22}, ui.RGBA(255, 255, 255, 0.1), 11)
	var x ui.Path
	x.MoveTo(lx-2.4, cy-2.4).LineTo(lx+2.4, cy+2.4).MoveTo(lx+2.4, cy-2.4).LineTo(lx-2.4, cy+2.4)
	p.StrokePath(&x, 1.3, ui.RGBA(255, 255, 255, 0.82))
	// Confirm: an off-white circle with a check.
	rx := r.X + r.W - 6 - 11
	p.Fill(ui.Rect{X: rx - 11, Y: cy - 11, W: 22, H: 22}, ui.RGBA(255, 255, 255, 0.92), 11)
	check := (&ui.Path{}).MoveTo(rx-2.7, cy+0.3).LineTo(rx-0.8, cy+2.2).LineTo(rx+2.8, cy-2)
	p.StrokePath(check, 1.3, charcoal)
	// The waveform: thin bars of fixed heights, centered.
	heights := []float32{4, 7, 11, 6, 14, 9, 15, 8, 12, 5, 9, 4}
	const w, gap = 2, 3
	left := r.X + r.W/2 - (float32(len(heights))*(w+gap)-gap)/2
	for i, h := range heights {
		p.Fill(ui.Rect{X: left + float32(i)*(w+gap), Y: cy - h/2, W: w, H: h}, ui.RGBA(255, 255, 255, 0.8), 1)
	}
}

// A non-disabled wrapper preserves hover help around a disabled switch.
func (a *app) cleanupSwitch(c *ui.Context) {
	a.ensureCleanupReady()
	c.After(15 * time.Second)
	on := a.settings.Get().CleanupDefault
	hint := T("cleanupNotConfigured")
	if a.cleanupReady {
		hint = T("cleanup")
	}
	if a.cleanupChecking {
		hint = T("cleanupChecking")
	}
	ui.Box(c.Key("cleanupHint")).Tooltip(hint).Children(func() {
		if ui.Switch(c.Key("sw"), &on).Label(T("cleanup")).Disabled(!a.cleanupReady && !on).Changed() {
			a.setCleanup(on)
		}
	})
}
