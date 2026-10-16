package main

import (
	"github.com/egoist/mygo/ui"
	"time"
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
		ui.Row(c.Key("add")).Padding(12, 16).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
			if time.Since(p.autoAppsAt) > 2*time.Second {
				p.autoApps = macos.RunningApplications()
				p.autoAppsAt = time.Now()
			}
			apps := p.autoApps
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
				options = append(options, selectOption{Value: a.BundleID, Label: a.Name, Group: T("applications"), Content: func(c *ui.Context) { appIdentity(c, a.BundleID, a.Name, true) }})
			}
			p.standardSelect(c, "send-add", T("addApplication"), "", "＋ "+T("addApplication"), 240, !s.Enabled, options, func(v string) {
				for _, a := range apps {
					if a.BundleID == v {
						p.change(func(s *config.Settings) {
							for _, r := range s.AutoSend.Apps {
								if r.BundleID == v {
									return
								}
							}
							s.AutoSend.Apps = append(s.AutoSend.Apps, config.AutoSendApp{BundleID: v, AppName: a.Name, Shortcut: "inherit"})
						})
						break
					}
				}
			})
		})
	})
}

// Bind initial presets to actual application identities on this Mac. The
// canonical ChatGPT bundle can differ from an app installed under that name.
// This only runs once; explicit user rules are never remapped later.
func (a *app) initializeAutoSendApplications() error {
	s := a.settings.Get()
	if s.AutoSend.ApplicationsInitialized {
		return nil
	}
	apps := macos.RunningApplications()
	for i, r := range s.AutoSend.Apps {
		for _, candidate := range apps {
			if candidate.BundleID == r.BundleID {
				s.AutoSend.Apps[i].AppName = candidate.Name
				break
			}
		}
		matches := []macos.Application{}
		found := false
		for _, candidate := range apps {
			if candidate.BundleID == r.BundleID {
				found = true
			}
			if candidate.Name == r.AppName {
				matches = append(matches, candidate)
			}
		}
		if !found && len(matches) == 1 {
			s.AutoSend.Apps[i].BundleID = matches[0].BundleID
			s.AutoSend.Apps[i].AppName = matches[0].Name
		}
	}
	s.AutoSend.ApplicationsInitialized = true
	return a.settings.Set(s)
}
