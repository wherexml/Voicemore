package main

import (
	"slices"
	"strings"
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
		return "Fn"
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
	now := time.Now()
	c.After(untilNextMinute(now))
	stats := m.historySummary(m.a.history.List(""), now)
	records := m.filteredHistory(m.a.history.List(m.query), stats.start, now)
	// Keep content aligned at 28 DIP, but give the recent-list scrollbar its
	// own gutter near the window edge instead of overlaying record actions.
	ui.Column(c).Fill().Padding(26, 8, 0, 28).Gap(0).Children(func() {
		ui.Column(c.Key("home-header")).FillWidth().Padding(0, 20, 0, 0).Children(func() {
			m.homeToolbar(c)
			ui.Column(c).FillWidth().Margin(0, 0, 18).Padding(18, 0).BorderWidth(.5, 0, .5, 0).BorderColor(settingsGroupLine(c)).Children(func() { m.homeCharts(c, stats, now) })
			ui.Row(c).FillWidth().Wrap().Padding(6, 2, 10).BorderWidth(0, 0, .5, 0).BorderColor(settingsGroupLine(c)).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, T("v3Recent")).FontSize(15).FontWeight(600)
				if ui.Button(c, T("v2All")).Height(26).Clicked() {
					m.clearHistoryFilters()
				}
				for _, app := range stats.apps[:min(4, len(stats.apps))] {
					b := ui.ButtonBase(c.Key(app.key)).Height(26).Padding(0, 6).Radius(13).Gap(5).AlignItems(ui.Center).Label(app.name).Children(func() { appIdentity(c, app.bundle, app.name, false, true) })
					if m.hApp == app.key {
						b.Background(settingsGroupColor(c.Theme()))
					}
					if b.Clicked() {
						if m.hApp == app.key {
							m.hApp = ""
						} else {
							m.hApp = app.key
						}
					}
				}
				ui.Spacer(c)
				searchInput(c, "home-search", &m.query, 180)
			})
			m.historyFilterChips(c)
		})
		ui.Scroll(c.Key("recent")).Grow(1).FillWidth().MinHeight(0).Padding(0, 20, 0, 0).Children(func() {
			m.journal(c, records[:min(12, len(records))], now)
			if ui.Button(c, T("showAll")).Clicked() {
				m.goTo("history")
				m.hRange = 3
			}
		})
	})
}

func (m *mainWindow) homeToolbar(c *ui.Context) {
	s := m.a.settings.Get()
	p := m.a.prefs
	t := c.Theme()
	ui.Row(c.Key("toolbar")).FillWidth().Margin(0, 0, 20).Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
		phase := m.a.ctrl.View().Phase
		recording := phase == session.Recording || phase == session.Preparing
		label := T("fnDictation")
		if recording {
			label = T("homeFinish")
		}
		if m.starting {
			label = T("homeCountdown")
		}
		if homeControl(c.Key("dictate"), icMic, label, false, keyName(s.TriggerKey)).Height(32).Background(railText(t)).TextColor(cardColor(t)).Disabled(m.starting || m.a.updates.installing || (phase != session.Idle && !recording && phase != session.Done && phase != session.Failed)).Clicked() {
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
						if !m.a.quitting && !m.a.updates.installing && m.a.ctrl.View().Phase == session.Idle {
							go m.a.ctrl.Toggle()
						}
					})
				})
			}
		}
		keys := []string{macos.KeyFn, macos.KeyRightOption, macos.KeyRightCommand, macos.KeyRightControl}
		if !slices.Contains(keys, s.TriggerKey) {
			keys = append(keys, s.TriggerKey)
		}
		triggerOptions := []selectOption{}
		for _, key := range keys {
			triggerOptions = append(triggerOptions, selectOption{Value: "key:" + key, Label: keyName(key), Group: T("sKey"), Selected: key == s.TriggerKey, Disabled: s.TranslationKey != "" && sameShortcut(key, s.TranslationKey)})
		}
		for _, mode := range []string{config.TriggerToggle, config.TriggerHold} {
			name := T("modeToggleShort")
			if mode == config.TriggerHold {
				name = T("modeHoldShort")
			}
			triggerOptions = append(triggerOptions, selectOption{Value: "mode:" + mode, Label: name, Group: T("sMode"), Selected: mode == s.TriggerMode, Disabled: mode == config.TriggerHold && !macos.IsModifierKey(s.TriggerKey)})
		}
		triggerOptions = append(triggerOptions, selectOption{Value: "settings", Label: T("catDictation"), Action: true})
		m.homeSelect(c, "home-trigger", icKeyboard, keyName(s.TriggerKey)+" · "+T(map[bool]string{true: "modeHoldShort", false: "modeToggleShort"}[s.TriggerMode == config.TriggerHold]), "", 0, 220, triggerOptions, func(value string) {
			switch {
			case strings.HasPrefix(value, "key:"):
				key := strings.TrimPrefix(value, "key:")
				if key != s.TriggerKey {
					p.saveShortcut("dictation", key)
				}
			case strings.HasPrefix(value, "mode:"):
				p.change(func(s *config.Settings) { s.TriggerMode = strings.TrimPrefix(value, "mode:") })
			default:
				p.open("dictation")
			}
		})
		for _, kind := range []string{providers.KindASR, providers.KindCleanup} {
			label := m.a.modelLabel(kind, s)
			if kind == providers.KindCleanup {
				label = T("cleanup") + ": " + label
				if !s.CleanupDefault {
					label = T("cleanup") + ": " + T("off")
				}
			}
			options := []selectOption{}
			selected := ""
			if kind == providers.KindCleanup {
				options = append(options, selectOption{Value: "off", Label: T("off")})
				if !s.CleanupDefault {
					selected = "off"
				}
			}
			assigned := providers.Assigned(kind, s)
			eligible := providers.Eligible(kind, s, m.a.secret)
			for _, o := range eligible {
				id := o.Provider + "|" + o.Model
				if providers.Check(kind, s, m.a.secret) == nil && o.Provider == assigned.Provider && (o.Provider == providers.Local || o.Model == assigned.Model) && (kind != providers.KindCleanup || s.CleanupDefault) {
					selected = id
				}
				options = append(options, selectOption{Value: id, Label: modelText(o.Provider, o.Model), Group: providerName(o.Provider, s, kind), Content: func(c *ui.Context) {
					logo(c, providerLogo(o.Provider, kind, s), 22)
					ui.Text(c, modelText(o.Provider, o.Model)).FontSize(13).Grow(1).SingleLine()
				}})
			}
			options = append(options, selectOption{Value: "manage", Label: T("manage"), Action: true})
			m.homeSelect(c, "home-"+kind, map[string]*ui.SVG{providers.KindASR: icWave, providers.KindCleanup: icSparkle}[kind], label, selected, 175, 340, options, func(value string) {
				switch value {
				case "manage":
					p.open("models")
				case "off":
					p.change(func(s *config.Settings) { s.CleanupDefault = false })
				default:
					for _, o := range eligible {
						if o.Provider+"|"+o.Model == value {
							p.assign(kind, o.Provider, o.Model)
							if kind == providers.KindCleanup {
								p.change(func(s *config.Settings) { s.CleanupDefault = true })
							}
							break
						}
					}
				}
			})
		}
		names, uids, _, _ := p.microphoneOptions(c, s)
		mic := names[0]
		micOptions := make([]selectOption, len(uids))
		for i, uid := range uids {
			micOptions[i] = selectOption{Value: uid, Label: names[i]}
			if uid == s.InputDevice {
				mic = names[i]
			}
		}
		m.homeSelect(c, "home-mic", icMic, mic, s.InputDevice, 130, 300, micOptions, func(uid string) {
			if uid == s.InputDevice {
				return
			}
			device := ""
			for _, d := range p.mics {
				if d.UID == uid {
					device = d.Name
					break
				}
			}
			p.change(func(s *config.Settings) { s.InputDevice, s.InputDeviceName = uid, device })
		})
		if !permissionsGranted() {
			if ui.Button(c, T("gPerms")).TextColor(t.Danger).Height(30).Clicked() {
				p.open("perms")
			}
		}
	})
}

// homeSelect keeps the compact toolbar while sharing selection, focus and panel styling.
func (m *mainWindow) homeSelect(c *ui.Context, id string, glyph *ui.SVG, label, value string, width, menuWidth float32, options []selectOption, onChange func(string)) {
	m.a.prefs.standardSelect(c, id, label, value, label, 0, false, options, onChange, selectPresentation{
		MenuWidth:  menuWidth,
		MenuHeight: 390,
		Trigger: func(open bool) ui.Element {
			button := homeControl(c.Key(id+"-trigger"), glyph, label, true)
			if width > 0 {
				button.MaxWidth(width)
			}
			if open {
				button.Border(1, c.Theme().Accent)
			}
			return button
		},
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

// Match the approved compact HTML toolbar rather than the borderless settings buttons.
func homeControl(c *ui.Context, glyph *ui.SVG, label string, chevron bool, shortcut ...string) ui.Element {
	return ui.ButtonBase(c).Height(30).Padding(0, 10).Radius(8).Gap(6).AlignItems(ui.Center).Background(cardColor(c.Theme())).Border(.5, settingsGroupLine(c)).Label(label).Tooltip(label).Children(func() {
		ui.Icon(c, glyph).Size(14, 14)
		ui.Text(c, label).FontSize(12.5).FontWeight(400).SingleLine().MinWidth(0)
		if len(shortcut) > 0 {
			ui.Text(c, shortcut[0]).FontSize(11).Font("Menlo").Padding(1, 5).Radius(4).Background(pick(c.Theme(), "#525252", "#dedede"))
		}
		if chevron {
			ui.Icon(c, icDown).Size(10, 10)
		}
	})
}
