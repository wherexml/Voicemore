package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

// privateApps lists the apps whose dictations are inserted but never kept
// (internal/session/privacy.go). Apps are added from the running ones, by
// Bundle ID, with the picker of auto-send (mock/language-privacy.html).
func (p *prefsWindow) privateApps(c *ui.Context) {
	s := p.a.settings.Get()
	card(c, "private-apps", func() {
		setting(c, "about", nil, T("privateApps"), T("privateAppsHint"), func() {})
		for i, app := range s.PrivateApps {
			ui.Row(c.Key(app.BundleID)).Padding(12, 16).Gap(12).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
					appIdentity(c, app.BundleID, app.AppName, true)
				})
				if ui.ButtonBase(c.Key("remove")).Width(28).Height(28).Label(T("remoteRemove") + " " + app.AppName).Children(func() { ui.Text(c, "×").FontSize(20).TextColor(c.Theme().TextMuted) }).Clicked() {
					p.change(func(s *config.Settings) {
						if i < len(s.PrivateApps) && s.PrivateApps[i].BundleID == app.BundleID {
							s.PrivateApps = append(s.PrivateApps[:i], s.PrivateApps[i+1:]...)
						}
					})
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
				if a.BundleID == "" || s.PrivateBundle(a.BundleID) {
					continue
				}
				a := a
				options = append(options, selectOption{Value: a.BundleID, Label: a.Name, Group: T("applications"), Content: func(c *ui.Context) { appIdentity(c, a.BundleID, a.Name, true) }})
			}
			p.standardSelect(c, "private-add", T("addApplication"), "", "＋ "+T("addApplication"), 240, false, options, func(v string) {
				for _, a := range apps {
					if a.BundleID == v {
						p.change(func(s *config.Settings) {
							if !s.PrivateBundle(v) {
								s.PrivateApps = append(s.PrivateApps, config.PrivateApp{BundleID: v, AppName: a.Name})
							}
						})
						break
					}
				}
			})
		})
	})
}

// asrLanguageSelect is the speech language of the on-device model. A change
// applies from the next dictation or retranscription.
func (p *prefsWindow) asrLanguageSelect(c *ui.Context, s config.Settings) {
	group := T("asrLanguage")
	opts := []selectOption{
		{Value: "zh", Label: T("asrLang_zh"), Group: group},
		{Value: config.ASRLanguageAuto, Label: T("asrLang_auto"), Group: group},
		{Value: config.ASRLanguageEnglish, Label: T("asrLang_en"), Group: group},
	}
	labelled(c, "language", group, "", func() {
		p.standardSelect(c, "asr-language", group, config.LocalASRLanguage(s.ASRLanguage), T("asrLang_zh"), 260, false, opts, func(v string) {
			p.change(func(s *config.Settings) {
				if v == "zh" {
					v = config.ASRLanguageChinese // the empty value: Chinese, as before
				}
				s.ASRLanguage = v
			})
		})
	})
}
