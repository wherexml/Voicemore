package main

import (
	"github.com/egoist/mygo/ui"
	"strconv"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func sendOptions(inherit bool) []selectOption {
	opts := []selectOption{}
	if inherit {
		opts = append(opts, selectOption{Value: "inherit", Label: T("sendInherit"), Group: T("sendShortcut")})
	}
	for _, v := range []struct{ key, label string }{{"off", T("sendOff")}, {"enter", "Enter ↵"}, {"cmd-enter", "Command + Enter ⌘↵"}, {"ctrl-enter", "Control + Enter ⌃↵"}} {
		opts = append(opts, selectOption{Value: v.key, Label: v.label, Group: T("sendShortcut")})
	}
	return opts
}
func (p *prefsWindow) autoSend(c *ui.Context) {
	ui.Text(c, T("autoSend")).FontSize(16)
	s := p.a.settings.Get().AutoSend
	card(c, "auto-send", func() {
		setting(c, "enabled", nil, T("sendAfterInput"), "", func() {
			enabled := s.Enabled
			if ui.Switch(c, &enabled).Label(T("sendAfterInput")).Changed() {
				p.change(func(s *config.Settings) { s.AutoSend.Enabled = enabled })
			}
		})
		ui.Row(c.Key("delay")).Padding(12, 16).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, T("sendDelay")).Grow(1)
			opts := []selectOption{{Value: "0", Label: T("sendImmediately"), Group: T("sendDelay")}}
			for _, n := range []int{1000, 2000, 3000, 5000} {
				opts = append(opts, selectOption{Value: strconv.Itoa(n), Label: strconv.Itoa(n/1000) + " s", Group: T("sendDelay")})
			}
			p.standardSelect(c, "send-delay", T("sendDelay"), strconv.Itoa(s.Delay()), "2 s", 220, !s.Enabled, opts, func(v string) {
				n, _ := strconv.Atoi(v)
				p.change(func(s *config.Settings) { s.AutoSend.DelayMs = &n })
			})
			ui.Box(c).Width(28)
		})
		ui.Row(c.Key("all")).Padding(12, 16).Gap(12).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
			ui.Icon(c, icAllApps).FontSize(24).TextColor(c.Theme().TextMuted)
			ui.Text(c, T("allApps")).FontSize(13).Grow(1)
			p.standardSelect(c, "send-default", T("allApps"), s.Default, "Enter ↵", 220, !s.Enabled, sendOptions(false), func(v string) { p.change(func(s *config.Settings) { s.AutoSend.Default = v }) })
			ui.Box(c).Width(28)
		})
		for i, rule := range s.Apps {
			ui.Row(c.Key(rule.BundleID)).Padding(12, 16).Gap(12).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
				appIdentity(c, rule.BundleID, rule.AppName, true)
				key := rule.Shortcut
				if key == "" {
					key = "inherit"
				}
				p.standardSelect(c, "send-"+rule.BundleID, rule.AppName, key, T("sendInherit"), 220, !s.Enabled, sendOptions(true), func(v string) { p.change(func(s *config.Settings) { s.AutoSend.Apps[i].Shortcut = v }) })
				if ui.ButtonBase(c.Key("remove")).Width(28).Height(28).Label(T("remoteRemove") + " " + rule.AppName).Disabled(!s.Enabled).Children(func() { ui.Text(c, "×").FontSize(20).TextColor(c.Theme().TextMuted) }).Clicked() {
					p.change(func(s *config.Settings) { s.AutoSend.Apps = append(s.AutoSend.Apps[:i], s.AutoSend.Apps[i+1:]...) })
				}
			})
		}
		apps := config.AIApplicationPresets()
		if p.sendApps == nil {
			p.sendApps = macos.SelectableApplications()
		}
		seen := map[string]bool{}
		for _, a := range apps {
			seen[a.BundleID] = true
		}
		for _, a := range p.sendApps {
			if !seen[a.BundleID] {
				seen[a.BundleID] = true
				apps = append(apps, config.AutoSendApp{BundleID: a.BundleID, AppName: a.Name, Shortcut: "inherit"})
			}
		}
		options := []selectOption{}
		for _, a := range apps {
			exists := false
			for _, r := range s.Apps {
				if r.BundleID == a.BundleID {
					exists = true
					break
				}
			}
			if exists {
				continue
			}
			a := a
			options = append(options, selectOption{Value: a.BundleID, Label: a.AppName, Group: T("applications"), Content: func(c *ui.Context) { appIdentity(c, a.BundleID, a.AppName, true) }})
		}
		if len(options) == 0 {
			if open := p.pickOpen["send-add"]; open != nil {
				*open = false
			}
			p.pickFocus["send-add"] = false
			return
		}
		ui.Row(c.Key("add")).Padding(12, 16).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
			p.standardSelect(c, "send-add", T("addApplication"), "", "＋ "+T("addApplication"), 240, !s.Enabled, options, func(v string) {
				for _, a := range apps {
					if a.BundleID == v {
						p.change(func(s *config.Settings) {
							for _, r := range s.AutoSend.Apps {
								if r.BundleID == v {
									return
								}
							}
							s.AutoSend.Apps = append(s.AutoSend.Apps, config.AutoSendApp{BundleID: v, AppName: a.AppName, Shortcut: "inherit"})
						})
						break
					}
				}
			}, selectPresentation{SearchPlaceholder: T("searchApplications")})
		})
	})
}

// Refresh display names by exact Bundle ID; names never change rule identity.
func (a *app) initializeAutoSendApplications() error {
	s := a.settings.Get()
	changed := s.AutoSend.MigrateAIApplications()
	apps := macos.RunningApplications()
	for i, rule := range s.AutoSend.Apps {
		for _, candidate := range apps {
			if candidate.BundleID == rule.BundleID && candidate.Name != rule.AppName {
				s.AutoSend.Apps[i].AppName = candidate.Name
				changed = true
			}
		}
	}
	if !s.AutoSend.ApplicationsInitialized {
		s.AutoSend.ApplicationsInitialized = true
		changed = true
	}
	if !changed {
		return nil
	}
	return a.settings.Set(s)
}
