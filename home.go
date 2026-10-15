package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/session"
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
	p := m.a.prefs
	width, _ := m.win.ContentSize()
	columns := 4
	if width < 900 {
		columns = 2
	}
	tileWidth := float32(min(width-160, 1080)) / float32(columns)
	key := keyName(s.TriggerKey)
	now := time.Now()
	c.After(untilNextMinute(now))
	c.After(time.Second)
	ui.Scroll(c).Fill().Padding(18, 32, 24).AlignItems(ui.Center).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(1100).Gap(18).Children(func() {
			ui.Row(c.Key("hero")).FillWidth().Padding(18).Gap(22).AlignItems(ui.Center).Radius(16).LinearGradient(ui.LinearGradient{From: pick(t, "#ededf4", "#252630"), To: cardColor(t), Angle: 110, End: 0.65}).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.16)).Children(func() {
				keycap(c, key, 54, 17)
				ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
					ui.Text(c, T("homeReady", key)).FontSize(22).FontWeight(600)
					hint := "homeToggle"
					if s.TriggerMode == config.TriggerHold {
						hint = "homeHold"
					}
					if m.starting {
						hint = "homeCountdown"
					}
					ui.Text(c, T(hint)).FontSize(13).TextColor(softText(t))
				})
				phase := m.a.ctrl.View().Phase
				recording := phase == session.Recording || phase == session.Preparing
				label := T("homeTry")
				if recording {
					label = T("homeFinish")
				}
				if ui.PrimaryButton(c.Key("dictate"), "").Label(label).Height(36).Radius(10).Padding(6, 16).Children(func() { ui.Icon(c, icMic).Size(14, 14); ui.Text(c, label).SingleLine() }).Disabled(m.starting || m.a.updates.installing || (phase != session.Idle && !recording && phase != session.Done && phase != session.Failed)).Clicked() {
					if recording {
						go m.a.ctrl.Toggle()
					} else {
						m.starting = true
						if phase == session.Done || phase == session.Failed {
							m.a.ctrl.Dismiss()
						}
						time.AfterFunc(3*time.Second, func() {
							m.win.Update(func() {
								m.starting = false
								phase := m.a.ctrl.View().Phase
								if !m.a.quitting && !m.a.updates.installing && phase == session.Idle {
									go m.a.ctrl.Toggle()
								}
							})
						})
					}
				}
			})
			ui.Row(c.Key("tiles")).FillWidth().Wrap().Radius(12).Background(fillColor(t)).Children(func() {
				tile := func(id string, svg *ui.SVG, title, value, cat string, control func()) {
					col := ui.Column(c.Key(id)).Grow(1).Basis(tileWidth).MinWidth(0).Padding(14, 16, 16).Gap(8)
					if id != "perms" {
						col.BorderWidth(0, 0.5, 0, 0).BorderColor(pick(t, "#dddddd", "#333333"))
					}
					if m.tileHover == nil {
						m.tileHover = map[string]time.Time{}
					}
					if col.Hovered() {
						if m.tileHover[id].IsZero() {
							m.tileHover[id] = now
						}
					} else {
						delete(m.tileHover, id)
					}
					elapsed := now.Sub(m.tileHover[id])
					showSettings := col.FocusWithin() || (col.Hovered() && elapsed >= 500*time.Millisecond)
					if col.Hovered() && !showSettings {
						c.After(500*time.Millisecond - elapsed)
					}
					col.Children(func() {
						ui.Row(c).Height(22).AlignItems(ui.Center).Gap(7).Children(func() {
							ui.Icon(c, svg).Size(14, 14).TextColor(t.TextMuted)
							ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
							settings := journalAction(c.Key("open"), icChevron, T("openIn", title)).Size(22, 22)
							if !showSettings {
								settings.Opacity(0)
							}
							if settings.Clicked() {
								p.open(cat)
							}
						})
						ui.Text(c, value).Height(24).FontSize(15).FontWeight(600).SingleLine().Tooltip(value)
						ui.Row(c).FillWidth().MinWidth(0).Height(28).Gap(6).AlignItems(ui.Center).Children(control)
					})
				}
				mode := 0
				if s.TriggerMode == config.TriggerHold {
					mode = 1
				}
				tile("shortcut", icKeyboard, T("hShortcut"), key+" · "+T([]string{"mToggle", "mHold"}[mode]), "dictation", func() {
					p.triggerKey(c, s, true)
					if segmentedSize(c.Key("mode"), &mode, true, T("modeToggleShort"), T("modeHoldShort")).Disabled(!macos.IsModifierKey(s.TriggerKey)).Changed() {
						p.change(func(s *config.Settings) { s.TriggerMode = []string{config.TriggerToggle, config.TriggerHold}[mode] })
					}
				})
				value := T("off")
				if s.CleanupDefault {
					value = m.a.modelLabel(providers.KindCleanup, s)
				}
				tile("cleanup", icSparkle, T("cleanup"), value, "dictation", func() {
					ui.Row(c).FillWidth().Height(28).Padding(0, 5, 0, 10).AlignItems(ui.Center).Radius(7).Background(cardColor(t)).Children(func() {
						ui.Text(c, T("sCleanDefault")).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
						m.a.cleanupSwitch(c)
					})
				})
				tile("asr", icMic, T("mAsr"), m.a.modelLabel(providers.KindASR, s), "models", func() { names, uids, _, _ := p.microphoneOptions(c, s); p.microphoneSelect(c, s, names, uids, 0) })
				perm := T("notGranted")
				if permissionsGranted() {
					perm = T("granted")
				}
				tile("perms", icShield, T("gPerms"), perm, "perms", func() {
					if ui.Button(c, T("gPerms")).FillWidth().Height(28).Padding(0, 8).Clicked() {
						p.open("perms")
					}
				})
			})
			ui.Column(c.Key("recent")).FillWidth().Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Padding(0, 0, 12).Children(func() {
					ui.Text(c, T("homeRecent")).FontSize(15).FontWeight(600).Grow(1)
					if ui.ButtonBase(c.Key("all")).Label(T("showAll")).Padding(6).Children(func() { ui.Text(c, T("showAll")).FontSize(12) }).Clicked() {
						m.clearHistoryFilters()
						m.hRange = 3
						m.goTo("history")
					}
				})
				ui.Divider(c)
				records := m.a.history.List("")
				m.journal(c, records[:min(4, len(records))], now)
			})
		})
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
