package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
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
		return T(k)
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
	ui.Scroll(c).Fill().Padding(44, 28, 24).AlignItems(ui.Center).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(580).Gap(48).Children(func() {
			// What to press. The key and the headline share a line.
			ui.Column(c.Key("hero")).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Height(50).MinWidth(52).Padding(0, 15).Radius(11).Justify(ui.Center).AlignItems(ui.Center).
						Background(t.Background).BorderWidth(1, 1, 3, 1).BorderColor(strongLine(t)).Children(func() {
						ui.Text(c, key).FontSize(16).FontWeight(600).SingleLine()
					})
					ui.Text(c, T("homeReady", key)).FontSize(25).FontWeight(600)
				})
				hint := "homeToggle"
				if s.TriggerMode == config.TriggerHold {
					hint = "homeHold"
				}
				ui.Text(c, T(hint)).FontSize(14).TextColor(t.TextMuted)
				// A still picture of the pill that appears while speaking.
				ui.Box(c).Size(146, 34).Draw(drawPillPreview)
			})

			// What is set up; each row opens its part of the settings.
			ui.Column(c.Key("setup")).Children(func() {
				ui.Text(c, T("homeSetup")).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Padding(0, 0, 4)
				mode := "mToggle"
				if s.TriggerMode == config.TriggerHold {
					mode = "mHold"
				}
				m.setupRow(c, "shortcut", T("hShortcut"), key+" · "+T(mode), "dictation", nil)
				cleanup := T("off")
				if s.CleanupDefault {
					cleanup = m.a.modelLabel(providers.KindCleanup, s)
				}
				m.setupRow(c, "cleanup", T("cleanup"), cleanup, "dictation", func() {
					on := s.CleanupDefault
					if ui.Switch(c.Key("sw"), &on).Label(T("cleanup")).Changed() {
						m.a.setCleanup(on)
					}
				})
				m.setupRow(c, "asr", T("mAsr"), m.a.modelLabel(providers.KindASR, s), "models", nil)
				perms := T("granted")
				if macos.MicrophoneStatus() != "authorized" || !macos.AccessibilityTrusted(false) || !macos.InputMonitoringGranted(false) {
					perms = T("notGranted")
				}
				m.setupRow(c, "perms", T("gPerms"), perms, "perms", nil)
				ui.Divider(c)
			})

			// The latest dictations, which open in History.
			now := time.Now()
			c.After(untilNextMinute(now)) // the time labels turn with the minute
			records := m.a.history.List("")
			ui.Column(c.Key("recent")).Children(func() {
				ui.Row(c).Padding(0, 0, 4).Children(func() {
					ui.Text(c, T("homeRecent")).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1)
					if len(records) > 0 {
						all := ui.ButtonBase(c.Key("all")).Label(T("showAll"))
						all.Children(func() { ui.Text(c, T("showAll")).FontSize(12).TextColor(t.TextMuted) })
						if all.Clicked() {
							m.goTo("history")
						}
					}
				})
				if len(records) == 0 {
					ui.Divider(c)
					ui.Text(c, T("homeEmpty")).TextColor(t.TextMuted).Padding(12, 0)
				}
				for _, r := range records[:min(3, len(records))] {
					ui.Divider(c)
					row := ui.ButtonBase(c.Key(r.ID)).Gap(12).Padding(11, 0).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					row.Children(func() {
						ui.Text(c, timeLabel(r.StartedAt, now)).FontSize(12).TextColor(faintText(t)).MinWidth(108).SingleLine()
						text, _ := primaryText(r)
						if text == "" {
							text = T("emptyText")
						}
						ui.Text(c, text).Grow(1).SingleLine()
					})
					if row.Clicked() {
						m.showRecord(r.ID)
					}
				}
				ui.Divider(c)
			})
		})
	})
}

// setupRow is a line of the setup list: a name, its value, an optional
// control, and a chevron into the settings.
func (m *mainWindow) setupRow(c *ui.Context, key, label, value, category string, control func()) {
	t := c.Theme()
	ui.Divider(c)
	ui.Row(c.Key(key)).Gap(12).MinHeight(44).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Grow(1).SingleLine()
		ui.Text(c, value).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
		if control != nil {
			control()
		}
		if iconButton(c.Key("open"), icChevron, T("openIn", label)).Clicked() {
			m.a.prefs.open(category)
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
