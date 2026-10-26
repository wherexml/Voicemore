package main

import (
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
)

// subControlW is the shared right-hand column of every subtitle setting.
const subControlW = 240

// subtitleSettings is Settings → Live Subtitles, after the mock's
// auto-send.html#settings/subtitles. Its choices are the subtitles' own:
// the dictation defaults of Models and Dictation are never touched here.
func (p *prefsWindow) subtitleSettings(c *ui.Context) {
	t := c.Theme()
	s := p.a.settings.Get().Subtitles.Normalized()
	change := func(fn func(s *config.Subtitles)) { p.a.changeSubtitles(fn) }

	ui.Row(c.Key("sub-head")).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, T("subRecognition")).FontSize(16).TextColor(t.Text).Grow(1).MinWidth(0)
		link := ui.ButtonBase(c.Key("sub-models")).Shrink(0).Label(T("subModelSettings"))
		link.Children(func() {
			ui.Text(c, T("subModelSettings")).FontSize(12).TextColor(pick(t, "#5458b5", "#7477d4")).SingleLine()
		})
		if link.Clicked() {
			p.cat = "models"
		}
	})
	card(c, "sub-recognition", func() {
		setting(c, "sub-source", nil, T("subSource"), "", func() {
			p.standardSelect(c, "sub-source", T("subSource"), "system", "", subControlW, false,
				[]selectOption{{Value: "system", Label: T("subSystemAudio")}}, func(string) {})
		})
		setting(c, "sub-engine", nil, T("subEngine"), "", func() {
			p.standardSelect(c, "sub-engine", T("subEngine"), s.Engine, T("notSet"), subControlW, false, []selectOption{
				{Value: config.SubtitleSherpa, Label: T("subEngineSherpa")},
				{Value: config.SubtitleQwen, Label: T("subEngineQwen")},
			}, func(v string) { change(func(s *config.Subtitles) { s.Engine = v }) })
		})
		setting(c, "sub-language", nil, T("subLanguage"), "", func() {
			if s.Engine == config.SubtitleSherpa {
				// The bilingual streaming model decides its language itself.
				p.standardSelect(c, "sub-language", T("subLanguage"), "model", "", subControlW, true,
					[]selectOption{{Value: "model", Label: T("subLangModel")}}, func(string) {})
				return
			}
			p.standardSelect(c, "sub-language", T("subLanguage"), s.Language, "", subControlW, false, []selectOption{
				{Value: "zh", Label: T("asrLang_zh")},
				{Value: "auto", Label: T("asrLang_auto")},
				{Value: "en", Label: T("asrLang_en")},
			}, func(v string) { change(func(s *config.Subtitles) { s.Language = v }) })
		})
		setting(c, "sub-target", nil, T("subTarget"), "", func() {
			opts := make([]selectOption, len(subtitleTargets))
			for i, l := range subtitleTargets {
				opts[i] = selectOption{Value: l.id, Label: l.label}
			}
			p.standardSelect(c, "sub-target", T("subTarget"), s.Target, "", subControlW, false, opts,
				func(v string) { change(func(s *config.Subtitles) { s.Target = v }) })
		})
	})

	ui.Text(c, T("subDisplay")).FontSize(16).TextColor(t.Text)
	card(c, "sub-display", func() {
		setting(c, "sub-orig", nil, T("subOrigSize"), "", func() {
			ui.Row(c).Width(subControlW).Justify(ui.End).Children(func() {
				subStepper(c, "sub-orig", T("subOrigSize"), s.OrigSize, config.SubtitleOrigMin, config.SubtitleOrigMax, s.HideOriginal, true,
					func(v int) { change(func(s *config.Subtitles) { s.OrigSize = v }); p.a.subtitles.fitDefaultIfOpen() })
			})
		})
		setting(c, "sub-trans", nil, T("subTransSize"), "", func() {
			ui.Row(c).Width(subControlW).Justify(ui.End).Children(func() {
				subStepper(c, "sub-trans", T("subTransSize"), s.TransSize, config.SubtitleTransMin, config.SubtitleTransMax, false, true,
					func(v int) { change(func(s *config.Subtitles) { s.TransSize = v }); p.a.subtitles.fitDefaultIfOpen() })
			})
		})
		setting(c, "sub-opacity", nil, T("subOpacity"), "", func() {
			ui.Row(c).Width(subControlW).Gap(10).AlignItems(ui.Center).Children(func() {
				v := float64(s.Opacity)
				if ui.StepSlider(c.Key("sub-opacity-slider"), &v, config.SubtitleOpacityMin, 100, 1).Grow(1).MinWidth(0).Label(T("subOpacity")).Changed() {
					change(func(s *config.Subtitles) { s.Opacity = int(math.Round(v)) })
				}
				ui.Text(c, strconv.Itoa(s.Opacity)+"%").FontSize(13).TextColor(t.TextMuted).Width(38).Shrink(0).SingleLine()
			})
		})
		setting(c, "sub-original", nil, T("subShowOriginal"), "", func() {
			on := !s.HideOriginal
			if ui.Switch(c.Key("sw"), &on).Label(T("subShowOriginal")).Changed() {
				change(func(s *config.Subtitles) { s.HideOriginal = !on })
				p.a.subtitles.fitDefaultIfOpen()
			}
		})
	})
	// The mock's solid black action, in both appearances.
	ui.Row(c.Key("sub-action")).Children(func() {
		b := ui.ButtonBase(c.Key("sub-open")).Height(36).Padding(0, 16).Radius(9).AlignItems(ui.Center).Justify(ui.Center).
			Background(ui.Hex("#000000")).Border(1, settingsGroupLine(c)).Label(T("subOpen"))
		if b.Hovered() {
			b.Background(ui.Hex("#262626"))
		}
		b.Children(func() {
			ui.Text(c, T("subOpen")).FontSize(13).FontWeight(500).TextColor(ui.Hex("#ffffff")).SingleLine()
		})
		if b.Clicked() {
			p.a.openSubtitles()
		}
	})
}

// fitDefaultIfOpen refits a window that exists; there is none to fit before
// subtitles are first opened.
func (w *subtitleWindow) fitDefaultIfOpen() {
	if w != nil {
		w.fitDefault()
	}
}
