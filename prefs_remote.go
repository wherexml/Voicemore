package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"strconv"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

func (p *prefsWindow) remoteInput(c *ui.Context) {
	ui.Text(c, T("remoteTitle")).FontSize(16)
	ui.Text(c, T("remoteHelp")).FontSize(12).TextColor(c.Theme().TextMuted)
	s := p.a.settings.Get()
	card(c, "remote-input", func() {
		for i, rule := range s.RemoteInputs {
			border := float32(0)
			if i > 0 {
				border = 1
			}
			ui.Column(c.Key(fmt.Sprint(i))).Padding(16, 18).Gap(12).BorderWidth(border, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
				row := func(id, label, hint string, control func(), remove bool) {
					ui.Row(c.Key(id)).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Column(c).Grow(1).Gap(3).Children(func() {
							ui.Text(c, label).FontSize(13)
							if hint != "" {
								ui.Text(c, hint).FontSize(11).TextColor(c.Theme().TextMuted)
							}
						})
						ui.Column(c).Width(220).Children(control)
						ui.Box(c).Width(52).Children(func() {
							if remove && ui.Button(c, T("remoteRemove")).Padding(0, 8).FontSize(13).Height(40).FillWidth().Clicked() {
								p.change(func(s *config.Settings) { s.RemoteInputs = append(s.RemoteInputs[:i], s.RemoteInputs[i+1:]...) })
								p.drafts = map[string]*string{}
							}
						})
					})
				}
				row("head", rule.AppName, "", func() {
					opts := []selectOption{{Value: "", Label: T("remoteOff"), Group: T("remoteShortcut")}, {Value: "cmd-v", Label: "Command+V (⌘V)", Group: T("remoteShortcut")}, {Value: "ctrl-v", Label: "Control+V (⌃V)", Group: T("remoteShortcut")}}
					p.standardSelect(c, "remote-mode-"+strconv.Itoa(i), T("remoteShortcut"), rule.Shortcut, T("remoteOff"), 220, false, opts, func(v string) { p.change(func(s *config.Settings) { s.RemoteInputs[i].Shortcut = v }) })
				}, true)
				row("scope", T("remoteWindow"), T("remoteWindowHint"), func() {
					title := p.draft(fmt.Sprintf("remote-title-%d", i), rule.WindowTitle)
					if ui.TextInput(c.Key("title"), title).Label(T("remoteWindow")).Width(220).Height(40).Changed() {
						p.change(func(s *config.Settings) { s.RemoteInputs[i].WindowTitle = *title })
					}
				}, false)
				row("wait", T("remoteWait"), "", func() {
					opts := []selectOption{}
					for _, n := range []int{500, 1000, 1500, 3000, 5000} {
						opts = append(opts, selectOption{Value: strconv.Itoa(n), Label: fmt.Sprintf("%g s", float64(n)/1000), Group: T("remoteWait")})
					}
					p.standardSelect(c, "remote-delay-"+strconv.Itoa(i), T("remoteWait"), strconv.Itoa(rule.SyncDelayMs), "1.5 s", 220, false, opts, func(v string) {
						n, _ := strconv.Atoi(v)
						p.change(func(s *config.Settings) { s.RemoteInputs[i].SyncDelayMs = n })
					})
				}, false)
			})
		}
		ui.Row(c.Key("add")).Padding(12, 18).Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("refresh"), T("remoteRefresh")).Height(40).Clicked() {
				p.remoteApps = macos.RunningApplications()
			}
			ui.Box(c).Grow(1)
			opts := []selectOption{}
			for _, a := range p.remoteApps {
				exists := false
				for _, r := range s.RemoteInputs {
					if r.BundleID == a.BundleID {
						exists = true
						break
					}
				}
				if !exists {
					opts = append(opts, selectOption{Value: a.BundleID, Label: a.Name, Group: T("applications")})
				}
			}
			p.standardSelect(c, "remote-add", T("remoteAdd"), "", T("remoteAdd"), 284, false, opts, func(v string) {
				for _, a := range p.remoteApps {
					if a.BundleID == v {
						p.change(func(s *config.Settings) {
							for _, r := range s.RemoteInputs {
								if r.BundleID == v {
									return
								}
							}
							s.RemoteInputs = append(s.RemoteInputs, config.RemoteInput{BundleID: v, AppName: a.Name, SyncDelayMs: 1500})
						})
						break
					}
				}
			})
		})
	})
}
