package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func (p *prefsWindow) remoteInput(c *ui.Context) {
	ui.Text(c, T("remoteTitle")).FontSize(16)
	ui.Text(c, T("remoteHelp")).FontSize(12).TextColor(c.Theme().TextMuted)
	s := p.a.settings.Get()
	card(c, "remote-input", func() {
		for i, rule := range s.RemoteInputs {
			ui.Column(c.Key(fmt.Sprint(i))).Padding(12).Gap(8).Children(func() {
				ui.Row(c).Gap(12).Children(func() {
					ui.Text(c, rule.AppName).Grow(1)
					choices := []string{T("remoteOff"), "Command+V (⌘V)", "Control+V (⌃V)"}
					chosen := choices[0]
					if rule.Shortcut == "cmd-v" {
						chosen = choices[1]
					}
					if rule.Shortcut == "ctrl-v" {
						chosen = choices[2]
					}
					if ui.Select(c.Key("mode"), &chosen, choices).Label(T("remoteShortcut")).Width(190).Changed() {
						p.change(func(s *config.Settings) {
							s.RemoteInputs[i].Shortcut = ""
							if chosen == choices[1] {
								s.RemoteInputs[i].Shortcut = "cmd-v"
							}
							if chosen == choices[2] {
								s.RemoteInputs[i].Shortcut = "ctrl-v"
							}
						})
					}
					if ui.Button(c.Key("remove"), T("remoteRemove")).Clicked() {
						p.change(func(s *config.Settings) { s.RemoteInputs = append(s.RemoteInputs[:i], s.RemoteInputs[i+1:]...) })
						p.drafts = map[string]*string{}
					}
				})
				setting(c, "scope", nil, T("remoteWindow"), T("remoteWindowHint"), func() {
					title := p.draft(fmt.Sprintf("remote-title-%d", i), rule.WindowTitle)
					if ui.TextInput(c.Key("title"), title).Label(T("remoteWindow")).Width(210).Changed() {
						p.change(func(s *config.Settings) { s.RemoteInputs[i].WindowTitle = *title })
					}
				})
				setting(c, "wait", nil, T("remoteWait"), "", func() {
					labels := []string{"0.5 s", "1 s", "1.5 s", "3 s", "5 s"}
					delays := []int{500, 1000, 1500, 3000, 5000}
					chosen := fmt.Sprintf("%g s", float64(rule.SyncDelayMs)/1000)
					if ui.Select(c.Key("delay"), &chosen, labels).Label(T("remoteWait")).Width(100).Changed() {
						for j, label := range labels {
							if chosen == label {
								p.change(func(s *config.Settings) { s.RemoteInputs[i].SyncDelayMs = delays[j] })
							}
						}
					}
				})
			})
		}
		ui.Row(c.Key("add")).Padding(12).Gap(10).Children(func() {
			if ui.Button(c.Key("refresh"), T("remoteRefresh")).Clicked() {
				p.remoteApps = macos.RunningApplications()
			}
			names := []string{T("remoteAdd")}
			for _, app := range p.remoteApps {
				names = append(names, app.Name+" · "+app.BundleID)
			}
			chosen := names[0]
			if ui.Select(c.Key("client"), &chosen, names).Label(T("remoteAdd")).Width(340).Changed() {
				for j, name := range names[1:] {
					if name == chosen {
						app := p.remoteApps[j]
						p.change(func(s *config.Settings) {
							s.RemoteInputs = append(s.RemoteInputs, config.RemoteInput{BundleID: app.BundleID, AppName: app.Name, SyncDelayMs: 1500})
						})
					}
				}
			}
		})
	})
}
