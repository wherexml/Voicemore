package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/cloudasr"
	"voiceinput/internal/config"
	"voiceinput/internal/localasr"
	"voiceinput/internal/providers"
	"voiceinput/internal/session"
)

// The Models category, as mock/index.html has it: the two default models,
// then the providers of each kind as cards. oMLX has a card in both
// kinds: they are the two existing local configurations, each with its own
// address and model, shown where they belong and never merged.
//
// Everything here is real. A check is a request to the address shown, and
// "configured" only means the fields are filled in. Alibaba Cloud and
// Volcengine speak their own protocols (internal/cloudasr); the models and
// access modes whose protocol their documents do not state are listed but
// cannot be made the default, each with the reason beside it.

// dialogRef says what the open editor edits.
type dialogRef struct {
	sort string // "cloud", "local" or "custom"
	id   string // a catalog ID, or a custom provider's ID ("" for a new one)
	kind string // providers.KindASR or providers.KindCleanup
}

// customDraft is a custom provider being added or edited. It is applied
// by Save, and survives closing the editor until then.
type customDraft struct {
	name, url, key, add                                string
	thinkingMode, thinkingParams, testModel, probeText string
	advanced                                           bool
	models                                             []string
	enabled                                            bool
	err                                                string // a text key
}

var kindLabels = map[string]string{providers.KindASR: "mAsr", providers.KindCleanup: "mCleanup"}

// assign sets the default of a kind.
func (p *prefsWindow) assign(kind, provider, model string) {
	if kind == providers.KindCleanup && providers.ManagedVoice(p.a.settings.Get()) {
		return
	}
	if provider == providers.Local {
		model = ""
	}
	p.change(func(s *config.Settings) {
		if kind == providers.KindASR {
			s.ASRAssign = config.Assignment{Provider: provider, Model: model}
		} else {
			s.CleanupAssign = config.Assignment{Provider: provider, Model: model}
		}
	})
}

// unassign clears the default of a kind when it is on a provider, and on
// a model of it when one is named. Nothing takes its place: choosing
// another model is the user's decision. It reports whether it cleared.
func unassign(s *config.Settings, kind, provider, model string) bool {
	a := &s.CleanupAssign
	if kind == providers.KindASR {
		a = &s.ASRAssign
	}
	if a.Provider != provider || (model != "" && a.Model != model) {
		return false
	}
	*a = config.Assignment{}
	return true
}

func (p *prefsWindow) modelsView(c *ui.Context) {
	t := c.Theme()
	s := p.a.settings.Get()
	ui.Column(c.Key("defaults")).Radius(14).Background(settingsGroupColor(t)).Border(1, settingsGroupLine(c)).Padding(16, 18, 18).Gap(14).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icSparkle).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, T("defaultModels")).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(22).AlignItems(ui.Start).Children(func() {
			p.defaultBlock(c, providers.KindASR, s)
			p.defaultBlock(c, providers.KindCleanup, s)
		})
	})
	p.group(c, providers.KindASR, "gAsrProv", s)
	p.group(c, providers.KindCleanup, "gLlmProv", s)
}

// defaultBlock is one of the two defaults: a label, the picker, and under
// it the provider with a way to its settings.
func (p *prefsWindow) defaultBlock(c *ui.Context, kind string, s config.Settings) {
	t := c.Theme()
	a := providers.Assigned(kind, s)
	err := providers.Check(kind, s, p.a.secret)
	label := p.a.modelLabel(kind, s)
	ui.Column(c.Key("def-" + kind)).Grow(1).Basis(0).MinWidth(0).Gap(7).Children(func() {
		ui.Text(c, T(kindLabels[kind])).FontSize(12.5).FontWeight(600)

		options := []selectOption{}
		selected := ""
		for _, o := range providers.Eligible(kind, s, p.a.secret) {
			o := o
			id := o.Provider + "|" + o.Model
			if err == nil && o.Provider == a.Provider && (o.Provider == providers.Local || o.Model == a.Model) {
				selected = id
			}
			options = append(options, selectOption{Value: id, Label: modelText(o.Provider, o.Model), Group: providerName(o.Provider, s, kind), Content: func(c *ui.Context) {
				logo(c, providerLogo(o.Provider, kind, s), 22)
				ui.Text(c, modelText(o.Provider, o.Model)).FontSize(13).Grow(1).SingleLine()
			}})
		}
		p.standardSelect(c, "model-"+kind, T(kindLabels[kind]), selected, label, 0, false, options, func(value string) {
			for _, o := range providers.Eligible(kind, s, p.a.secret) {
				if o.Provider+"|"+o.Model == value {
					p.assign(kind, o.Provider, o.Model)
					break
				}
			}
		})

		ui.Row(c).Gap(7).AlignItems(ui.Center).MinHeight(18).Children(func() {
			if a.Provider == "" {
				ui.Text(c, T("notSet")).FontSize(12).TextColor(t.TextMuted)
			} else {
				logo(c, providerLogo(a.Provider, kind, s), 16)
				ui.Text(c, providerName(a.Provider, s, kind)).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
			ui.Spacer(c)
			manage := ui.ButtonBase(c.Key("manage")).Label(T("manage"))
			manage.Children(func() { ui.Text(c, T("manage")).FontSize(12).TextColor(t.TextMuted).Underline() })
			if manage.Clicked() && a.Provider != "" {
				p.openProvider(a.Provider, kind, s) // to the settings of the provider in use
			}
		})
	})
}

// modelText is a model as shown: a catalog model's own name when it has
// one, else its ID.
func modelText(provider, id string) string {
	if e, ok := providers.Lookup(provider); ok {
		for _, m := range e.Models {
			if m.ID == id && m.Label != "" {
				return T(m.Label)
			}
		}
	}
	return id
}

// group is a kind's providers: a heading with "Add provider", and the
// cards two to a row.
func (p *prefsWindow) group(c *ui.Context, kind, title string, s config.Settings) {
	t := c.Theme()
	ui.Box(c).Height(10) // with the column's gap: about 30 px between categories
	ui.Row(c.Key("head-" + kind)).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, T(title)).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1)
		if labelButton(c.Key("add"), icPlus, T("addProvider")).Clicked() {
			p.openCustom("", kind, s)
		}
	})
	ui.Box(c).Height(5) // about 15 px from the heading to the cards
	ui.Grid(c.Key("cards-" + kind)).Columns(2).Gap(12).Children(func() {
		for _, e := range providers.Catalog {
			if e.Kind != kind {
				continue
			}
			on, filled := providers.Enabled(e.ID, s), providers.Configured(e.ID, s, p.a.secret)
			state, dot, summary := T("stOffLong"), t.Border, T("configure")
			modeOK, _ := providers.ModeUsable(e.ID, s)
			switch {
			case on && !modeOK:
				state, dot = T("stUnavailable"), t.Warning // this access mode, not the provider
			case on && filled:
				state, dot, summary = T("stConfigured"), t.Success, T("nModels", len(providers.Models(e.ID, s)))
			case on:
				state, dot = T("stNeedsKey"), t.Warning
			}
			if p.providerCard(c, e.ID, T(e.Name), state, dot, summary) {
				p.openProvider(e.ID, kind, s)
			}
		}
		state, dot, summary := T("stNeedsSetup"), t.Warning, T("configure")
		if providers.LocalReady(kind, s) {
			state, dot, summary = T("stConfigured"), t.Success, providers.LocalModel(kind, s)
		}
		cardID, cardName := providers.Local, "oMLX"
		if kind == providers.KindASR {
			cardID, cardName = "builtin-asr", T("builtinAsr")
			st := p.a.localASR.Snapshot()
			state, summary, dot = localASRState(st), localasr.Model, t.Warning
			if st.Phase == "ready" {
				dot = t.Success
			}
		}
		if p.providerCard(c, cardID, cardName, state+" · "+T("localTag"), dot, summary) {
			p.openProvider(providers.Local, kind, s)
		}
		for _, cp := range s.Providers.Custom {
			if cp.Kind != kind {
				continue
			}
			state, dot, summary := T("stOffLong"), t.Border, T("configure")
			switch {
			case providers.CustomReady(cp):
				state, dot, summary = T("stConfigured"), t.Success, T("nModels", len(cp.Models))
			case cp.Enabled:
				state, dot = T("stNeedsSetup"), t.Warning
			}
			if p.providerCard(c, cp.ID, providerName(cp.ID, s), state+" · "+T("customTag"), dot, summary) {
				p.openProvider(cp.ID, kind, s)
			}
		}
	})
}

// providerCard is a provider on the overview: its mark, its name, a quiet
// state, and a footer that says what opening it is for.
func (p *prefsWindow) providerCard(c *ui.Context, id, name, state string, dot ui.Color, summary string) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("card-"+id)).Radius(14).Background(settingsGroupColor(t)).Border(1, settingsGroupLine(c)).AlignItems(ui.Stretch).
		MinHeight(118).Clip().Label(T("providerOf", name) + " — " + state)
	if b.Hovered() {
		b.Border(1, t.Accent)
	}
	b.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Grow(1).Gap(14).Padding(16, 16, 14).AlignItems(ui.Center).Children(func() {
				logo(c, providers.Brand(id, p.a.settings.Get()), 36)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, name).FontSize(14).FontWeight(600).SingleLine()
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(7, 7).Radius(3.5).Background(dot)
						ui.Text(c, state).FontSize(12).TextColor(t.TextMuted).SingleLine()
					})
				})
			})
			ui.Row(c).Gap(8).Padding(9, 16).AlignItems(ui.Center).Background(fillColor(t)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				ui.Text(c, summary).FontSize(12).TextColor(t.TextMuted).Grow(1).SingleLine()
				ui.Icon(c, icChevron).FontSize(11).TextColor(faintText(t))
			})
		})
	})
	return b.Clicked()
}

// openProvider opens the editor of what a card stands for.
func (p *prefsWindow) openProvider(id, kind string, s config.Settings) {
	switch {
	case id == providers.Local:
		p.dlg = dialogRef{sort: "local", kind: kind}
	default:
		if _, ok := providers.Lookup(id); !ok {
			p.openCustom(id, kind, s)
			return
		}
		p.dlg = dialogRef{sort: "cloud", id: id, kind: kind}
	}
	p.dlgOpen = true
}

func customSlot(id, kind string) string {
	if id == "" {
		return "new:" + kind
	}
	return id
}

// openCustom opens the editor of a custom provider, or of a new one, on
// the draft left from last time if there is one.
func (p *prefsWindow) openCustom(id, kind string, s config.Settings) {
	slot := customSlot(id, kind)
	if p.custom[slot] == nil {
		d := &customDraft{enabled: true, thinkingMode: "server"}
		if cp, ok := providers.Custom(id, s); ok {
			d = &customDraft{name: providerName(cp.ID, s), url: cp.URL, models: slices.Clone(cp.Models), enabled: cp.Enabled, thinkingMode: cp.ThinkingMode, thinkingParams: cp.ThinkingParams}
			if d.thinkingMode == "" {
				d.thinkingMode = "server"
			}
		}
		p.custom[slot] = d
	}
	p.dlg = dialogRef{sort: "custom", id: id, kind: kind}
	p.dlgOpen = true
}

// dialog is the editor of a provider, over the window. Escape and a press
// outside close it and nothing else; what it holds stays in the settings
// or in its draft.
func (p *prefsWindow) dialog(c *ui.Context) {
	ui.Modal(c, &p.dlgOpen, func() {
		s := p.a.settings.Get()
		ui.Column(c.Key(p.dlg.sort + "/" + p.dlg.id + "/" + p.dlg.kind)).Width(500).Gap(14).Children(func() {
			switch p.dlg.sort {
			case "local":
				if p.dlg.kind == providers.KindASR {
					p.localASR(c, s)
				} else {
					p.localCleanup(c, s)
				}
			case "custom":
				p.customEditor(c, s)
			default:
				p.cloudEditor(c, s)
			}
		})
	})
	// "Add model", above the provider's editor. Escape and a press outside
	// close it alone; the text typed stays for next time.
	ui.Modal(c, &p.addOpen, func() {
		t := c.Theme()
		s := p.a.settings.Get()
		add := p.draft("ds.add", "")
		confirm := func() {
			model := strings.TrimSpace(*add)
			known := slices.ContainsFunc(providers.Models(providers.DeepSeek, s), func(m providers.Model) bool { return strings.EqualFold(m.ID, model) })
			switch {
			case model == "":
				p.addErr = "errEmptyModel"
			case known:
				p.addErr = "errDupModel"
			default:
				p.change(func(s *config.Settings) { s.Providers.DeepSeekModels = append(s.Providers.DeepSeekModels, model) })
				*add, p.addErr, p.addOpen = "", "", false
			}
		}
		ui.Column(c.Key("add-model")).Width(340).Gap(12).Children(func() {
			ui.Text(c, T("addModel")).FontSize(14).FontWeight(600)
			in := groupTextInput(c.Key("in"), add, true).Placeholder(T("mModelId")).Label(T("mModelId")).AutoFocus()
			if in.Changed() {
				p.addErr = ""
			}
			if in.Submitted() {
				confirm()
			}
			if p.addErr != "" {
				ui.Text(c, T(p.addErr)).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("cancel"), T("cancelBtn")).Clicked() {
					p.addOpen, p.addErr = false, ""
				}
				if ui.PrimaryButton(c.Key("ok"), T("add")).Clicked() {
					confirm()
				}
			})
		})
	})
}

// dialogHead is the top of an editor: the mark, the name, an optional
// "enabled" switch, and close.
func (p *prefsWindow) dialogHead(c *ui.Context, provider, title string, enabled *bool, changed func()) {
	ui.Row(c.Key("head")).Gap(12).AlignItems(ui.Center).Children(func() {
		logo(c, providers.Brand(provider, p.a.settings.Get()), 28)
		ui.Text(c, title).FontSize(14).FontWeight(600).Grow(1).SingleLine()
		if enabled != nil {
			if ui.Switch(c.Key("on"), enabled).Label(T("enabled")).Tooltip(T("enabled")).Changed() {
				changed()
			}
		}
		if iconButton(c.Key("close"), icClear, T("close")).Clicked() {
			p.dlgOpen = false
		}
	})
}

// labelled is a field of an editor under its label, with an optional note.
func labelled(c *ui.Context, key, label, note string, control func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).Gap(5).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(control)
		if note != "" {
			ui.Text(c, note).FontSize(11.5).TextColor(faintText(t))
		}
	})
}

// secretField edits one credential. The stored value is never put in the
// field: typing replaces it, Save stores what was typed in the app's
// encrypted local store, and the cross removes the stored value. Nothing here
// writes a credential to settings.json, to the history or to a log.
// edited is called whenever the field's stored value is about to change;
// saved, when not nil, only after "Save key" stored a key successfully.
func (p *prefsWindow) secretField(c *ui.Context, account, label string, edited, saved func()) {
	stored := p.a.secret(account) != ""
	draft := p.draft("secret:"+account, "")
	placeholder := T("mKeyNone")
	if stored {
		placeholder = T("mKeyStored")
	}
	in := groupTextInput(c.Key("in"), draft, true).Placeholder(placeholder).Label(label).Grow(1)
	if !p.shown[account] {
		in.Password()
	}
	in.Changed()
	svg, tip := icEye, T("showKey")
	if p.shown[account] {
		svg, tip = icEyeOff, T("hideKey")
	}
	if iconButton(c.Key("show"), svg, tip).Clicked() {
		p.shown[account] = !p.shown[account]
	}
	store := func(value, okKey string) {
		*draft, p.shown[account] = "", false
		if edited != nil {
			edited()
		}
		go func() { // keep durable file I/O off the UI thread
			err := p.a.setSecret(account, value)
			p.win.Update(func() {
				if err != nil {
					p.say("keyNotSaved", err.Error())
				} else {
					p.say(okKey)
					if value != "" && saved != nil {
						saved() // an explicit, successful Save: never a removal
					}
				}
			})
			p.a.main.refresh()
		}()
	}
	if ui.Button(c.Key("save"), T("saveKey")).Disabled(*draft == "").Clicked() {
		store(*draft, "keySaved")
	}
	if iconButton(c.Key("clear"), icClear, T("clearKey")).Disabled(!stored && *draft == "").Clicked() {
		store("", "keyCleared")
	}
}

var fieldLabels = map[string]string{"apiKey": "mKey", "appId": "appId", "token": "accessToken"}

// volcKeyAccount is where Volcengine's API key is stored: the account of
// the current ("new") access mode, the only one the settings show.
const volcKeyAccount = "provider.volc." + providers.VolcNew + ".apiKey"

// volcUseKey moves settings still in the legacy access mode to the
// API-key mode. It is called for one thing only: the user pressed "Save
// key" in Volcengine's editor and the key was stored. That is so whether
// or not the key differs from one already stored. Until then the legacy
// credentials serve as before; opening the editor, typing, or removing a
// key changes nothing.
func (p *prefsWindow) volcUseKey() {
	if providers.VolcMode(p.a.settings.Get()) != providers.VolcNew {
		p.change(func(s *config.Settings) { s.Providers.VolcMode = providers.VolcNew })
		p.conn(providers.Volc).invalidate()
	}
}

// defaultControl is "Set as default", or the mark of being it. It is
// disabled until the provider can serve.
func (p *prefsWindow) defaultControl(c *ui.Context, key, kind, provider, model string, s config.Settings, can bool) {
	t := c.Theme()
	a := providers.Assigned(kind, s)
	if a.Provider == provider && (provider == providers.Local || a.Model == model) {
		ui.Text(c, T("isDefault")).FontSize(11).TextColor(t.Accent).SingleLine()
		return
	}
	if ui.Button(c.Key(key), T("setDefault")).Disabled(!can || (kind == providers.KindCleanup && providers.ManagedVoice(s))).Clicked() {
		p.assign(kind, provider, model)
	}
}

// cloudEditor is a catalog provider: Alibaba Cloud, Volcengine, DeepSeek.
func (p *prefsWindow) cloudEditor(c *ui.Context, s config.Settings) {
	t := c.Theme()
	id, kind := p.dlg.id, p.dlg.kind
	if id == providers.Sub2API {
		kind = providers.KindASR
	}
	e, _ := providers.Lookup(id)
	k := p.conn(id)
	on := providers.Enabled(id, s)
	p.dialogHead(c, id, T(e.Name), &on, func() {
		cleared := false
		p.change(func(s *config.Settings) {
			s.Providers.Enabled[id] = on
			if !on {
				cleared = unassign(s, kind, id, "") // turned off: its default is cleared, never replaced
			}
		})
		k.invalidate()
		if cleared {
			p.say("assignCleared", T(e.Name))
		}
	})
	modeOK, _ := providers.ModeUsable(id, s)
	if id == providers.Sub2API {
		endpoint := p.draft("sub2api.endpoint", providers.Sub2APIBase(s))
		labelled(c, "endpoint", T("endpoint"), "", func() {
			if groupTextInput(c.Key("in"), endpoint, true).Label(T("endpoint")).Grow(1).Changed() {
				k.invalidate()
				p.change(func(s *config.Settings) { s.Providers.Sub2APIEndpoint = strings.TrimSpace(*endpoint) })
			}
		})
	}
	if id == providers.Volc {
		// Volcengine is set up with an API key and a billing plan, and
		// nothing else: the access-mode choice, the legacy App ID and token
		// fields and Agent Plan are no longer shown. Credentials saved in
		// the legacy mode keep serving until "Save key" is pressed here.
		if providers.VolcMode(s) != providers.VolcNew {
			ui.Text(c, T("volcLegacyNote")).FontSize(12).TextColor(t.Warning).Padding(10, 12).Radius(8).Background(noticeColor(t))
		}
		labelled(c, "resource", T("volcResource"), "", func() {
			resources := []string{cloudasr.VolcHourly, cloudasr.VolcConcurrent, cloudasr.VolcClassicHourly}
			i := slices.Index(resources, providers.VolcResource(s))
			if segmented(c.Key("seg"), &i, T("vHourly"), T("vConcurrent"), T("vClassicHourly")).Changed() {
				p.change(func(s *config.Settings) {
					s.Providers.VolcResource = resources[i]
				})
				k.invalidate()
			}
		})
	}
	if id == providers.Aliyun {
		base := p.draft("aliyun.base", providers.AliyunBase(s))
		note := T("aliyunBaseNote")
		if !providers.ValidSocketURL(*base) {
			note = T("eBadSocketURL")
		}
		labelled(c, "base", T("endpoint"), note, func() {
			if groupTextInput(c.Key("in"), base, true).Label(T("endpoint")).Grow(1).Changed() {
				k.invalidate()
				p.change(func(s *config.Settings) { s.Providers.AliyunBase = strings.TrimSpace(*base) })
			}
		})
	}
	fields := providers.Fields(id, s)
	if id == providers.Volc {
		fields = []string{"apiKey"} // the one field shown, whatever mode the saved settings are in
	}
	for _, f := range fields {
		account := providers.Account(id, s, f)
		if id == providers.Volc {
			account = volcKeyAccount
		}
		var saved func()
		if id == providers.Volc {
			saved = p.volcUseKey
		}
		labelled(c, "secret-"+account, T(fieldLabels[f]), "", func() { p.secretField(c, account, T(fieldLabels[f]), k.invalidate, saved) })
	}
	if id != providers.Sub2API {
		ui.Text(c, T("mKeyHint")).FontSize(11.5).TextColor(faintText(t))
	}

	if id == providers.DeepSeek {
		endpoint := p.draft("ds.endpoint", providers.Endpoint(s))
		preview := T("eBadURL")
		if providers.ValidURL(*endpoint) {
			preview = T("sendsTo", providers.ChatURL(*endpoint))
		}
		labelled(c, "endpoint", T("endpoint"), preview, func() {
			if groupTextInput(c.Key("in"), endpoint, true).Label(T("endpoint")).Grow(1).Changed() {
				k.invalidate()
				p.change(func(s *config.Settings) { s.Providers.DeepSeekEndpoint = strings.TrimSpace(*endpoint) })
			}
		})
	}
	ui.Row(c.Key("links")).Gap(14).Children(func() {
		for _, l := range [][2]string{{"getKey", e.KeyURL}, {"docs", e.DocsURL}} {
			link := ui.ButtonBase(c.Key(l[0])).Label(T(l[0]))
			link.Children(func() { ui.Text(c, T(l[0])).FontSize(12).TextColor(t.TextMuted).Underline() })
			if link.Clicked() {
				mygo.Shell.OpenExternal(l[1])
			}
		}
	})

	// The models. A default can only be one of a provider that can serve.
	can := modeOK && on && providers.Configured(id, s, p.a.secret)
	modelNote := ""
	if id != providers.Sub2API {
		modelNote = T("refNote")
	}
	labelled(c, "models", T("gModels"), modelNote, func() {
		ui.Column(c).Grow(1).Radius(10).Background(settingsGroupColor(t)).Border(1, settingsGroupLine(c)).Gap(1).Dividers(1, t.Border).Children(func() {
			for _, m := range providers.Models(id, s) {
				ui.Row(c.Key(m.ID)).Gap(8).Padding(8, 12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, modelText(id, m.ID)).FontSize(12.5).Grow(1).Selectable()
					if m.Recommended {
						ui.Text(c, T("recommended")).FontSize(11).TextColor(t.TextMuted).SingleLine()
					}
					if m.Custom {
						ui.Text(c, T("custom")).FontSize(11).TextColor(t.TextMuted).SingleLine()
					}
					if ok, why := providers.ModelUsable(id, m.ID); ok {
						p.defaultControl(c, "default", kind, id, m.ID, s, can)
					} else {
						// Listed, but not offered: the reason stands in place of the button.
						ui.Text(c, T(why)).FontSize(11).TextColor(t.Warning).MaxWidth(220)
					}
					if m.Custom && iconButton(c.Key("rm"), icTrash, T("removeModel", m.ID)).Clicked() {
						cleared := false
						p.change(func(s *config.Settings) {
							s.Providers.DeepSeekModels = slices.DeleteFunc(s.Providers.DeepSeekModels, func(x string) bool { return x == m.ID })
							cleared = unassign(s, kind, id, m.ID)
						})
						if cleared {
							p.say("assignCleared", m.ID)
						}
					}
				})
			}
		})
	})
	if kind == providers.KindASR {
		// A real check: the handshake, with the credentials, and the start
		// of a session for one model. No audio is sent. It can only say
		// that the provider accepted the session, not how well it hears.
		model := s.ASRAssign.Model
		if s.ASRAssign.Provider != id {
			model = ""
			for _, m := range providers.Models(id, s) {
				if ok, _ := providers.ModelUsable(id, m.ID); ok {
					model = m.ID
					break
				}
			}
		}
		ui.Row(c.Key("test")).Gap(8).AlignItems(ui.Center).Children(func() {
			if labelButton(c.Key("btn"), icLink, T("testConn")).Disabled(k.loading != nil || !can || model == "").Clicked() {
				service := providers.Dialer(id, model, s, p.a.secret)
				name := modelText(id, model)
				p.run(k, "testing", func(ctx context.Context) (bool, func() string, func()) {
					stream, err := service.Dial(ctx, nil)
					if err != nil {
						return false, func() string { return explain(err) }, nil
					}
					stream.Close()
					return true, func() string { return T("probeOk", name) }, nil
				})
			}
			connStatus(c, k)
		})
		return
	}

	// DeepSeek: more model IDs, in a dialog of their own, and a real check
	// of the endpoint and key.
	ui.Row(c.Key("add")).Children(func() {
		if labelButton(c.Key("btn"), icPlus, T("addModel")).Clicked() {
			p.addOpen, p.addErr = true, ""
		}
	})
	ui.Row(c.Key("test")).Gap(8).AlignItems(ui.Center).Children(func() {
		if labelButton(c.Key("btn"), icLink, T("testConn")).Disabled(k.loading != nil).Clicked() {
			opts := cleanup.Options{BaseURL: providers.APIBase(providers.Endpoint(s)), APIKey: p.a.secret(providers.Account(id, s, "apiKey"))}
			model := ""
			if s.CleanupAssign.Provider == id {
				model = s.CleanupAssign.Model
			}
			p.run(k, "testing", func(ctx context.Context) (bool, func() string, func()) {
				// GET /models only: no text is sent and nothing is inferred.
				models, err := cleanup.ListModels(ctx, opts)
				switch {
				case err != nil:
					return false, func() string { return explain(err) }, nil
				case model != "" && !slices.Contains(models, model):
					return false, func() string { return T("testNotListed", model, len(models)) }, nil
				case model != "":
					return true, func() string { return T("testOk", model) }, nil
				}
				return true, func() string { return T("fetchOk", len(models)) }, nil
			})
		}
		connStatus(c, k)
	})
}

// localASR manages the app-owned model, runtime and worker.
func (p *prefsWindow) localASR(c *ui.Context, s config.Settings) {
	t := c.Theme()
	state := p.a.localASR.Snapshot()
	p.dialogHead(c, "builtin-asr", T("builtinAsr")+" · "+T("mAsr"), nil, nil)
	ui.Text(c, T("builtinNote")).FontSize(11.5).TextColor(t.TextMuted)
	labelled(c, "model", T("mModel"), "", func() { ui.Text(c, localasr.Model).FontSize(12.5).Selectable() })
	p.asrLanguageSelect(c, s)
	ui.Text(c, T("builtinSize", float64(localasr.ModelBytes())/1e9)).FontSize(11.5).TextColor(t.TextMuted)
	ui.Text(c, T("builtinIdle")).FontSize(11.5).TextColor(t.TextMuted)
	source := p.draft("asr.source", localasr.DefaultSource)
	if !state.Installed || state.Phase == "error" {
		labelled(c, "source", T("builtinSource"), "", func() {
			groupTextInput(c.Key("in"), source, true).Label(T("builtinSource")).Grow(1).Disabled(state.Busy())
		})
	}
	activate := func() {
		p.change(func(s *config.Settings) {
			s.ASRManaged = true
			s.ASRURL, s.ASRModel = localasr.URL, localasr.Model
			s.ASRAssign = config.Assignment{Provider: providers.Local}
		})
	}
	ui.Row(c.Key("actions")).Gap(8).AlignItems(ui.Center).Children(func() {
		if !state.Installed || state.Phase == "error" {
			if labelButton(c.Key("copy"), icPlus, T("builtinCopy")).Disabled(state.Busy() || strings.TrimSpace(*source) == "").Clicked() {
				activate()
				p.a.localASR.Deploy(strings.TrimSpace(*source))
			}
			if labelButton(c.Key("download"), icDown, T("builtinDownload")).Disabled(state.Busy()).Clicked() {
				activate()
				p.a.localASR.Deploy("")
			}
		}
		if state.Installed && state.Phase != "ready" && !state.Busy() {
			if labelButton(c.Key("start"), icPlay, T("builtinStart")).Clicked() {
				activate()
				p.a.localASR.Start()
			}
		}
		if state.Busy() || state.Phase == "ready" {
			label := T("builtinStop")
			if state.Busy() {
				label = T("cancelBtn")
			}
			if ui.Button(c.Key("stop"), label).Disabled(state.Phase == "stopping" || p.a.ctrl.View().Phase == session.Recording || p.a.ctrl.View().Phase == session.Finalizing).Clicked() {
				p.a.localASR.Stop()
			}
		}
		ui.Spacer(c)
		p.defaultControl(c, "default", providers.KindASR, providers.Local, "", s, state.Phase == "ready")
	})
	ui.Row(c.Key("status")).Gap(8).Children(func() {
		if state.Busy() {
			ui.Spinner(c)
		}
		col := t.TextMuted
		if state.Phase == "ready" {
			col = t.Success
		}
		if state.Phase == "error" {
			col = t.Danger
		}
		ui.Text(c, localASRState(state)).FontSize(12).TextColor(col)
	})
	if state.Phase == "installing" && state.Total > 0 {
		ui.Text(c, fmt.Sprintf("%.0f%% · %.2f / %.2f GB", float64(state.Done)*100/float64(state.Total), float64(state.Done)/1e9, float64(state.Total)/1e9)).FontSize(12)
	}
	if state.Detail != "" {
		ui.Text(c, state.Detail).FontSize(11.5).TextColor(t.TextMuted).Selectable()
	}
	ui.Text(c, T("builtinLocation", p.a.localASR.Root)).FontSize(11).TextColor(t.TextMuted).Selectable()
}

func localASRState(s localasr.State) string {
	keys := map[string]string{"absent": "builtinAbsent", "installing": "builtinInstalling", "starting": "builtinStarting", "ready": "builtinReady", "stopping": "builtinStopping", "stopped": "builtinStopped", "error": "builtinError"}
	if key, ok := keys[s.Phase]; ok {
		return T(key)
	}
	return s.Phase
}
func providerLogo(id, kind string, s config.Settings) string {
	if id == providers.Local && kind == providers.KindASR {
		return "builtin-asr"
	}
	return providers.Brand(id, s)
}

// inspectASR is a check of a speech service: GET /health only. No session
// is opened and no audio is sent.
func inspectASR(url string, learned func(model string)) func(ctx context.Context) (bool, func() string, func()) {
	return func(ctx context.Context) (bool, func() string, func()) {
		info, err := asr.Inspect(ctx, url)
		if err != nil {
			return false, func() string { return explain(err) }, nil
		}
		var apply func()
		if info.Model != "" && learned != nil {
			apply = func() { learned(info.Model) }
		}
		return info.Ready, func() string {
			state, name := T("asrReady"), info.Model
			if info.Busy {
				state = T("asrBusyNow")
			} else if !info.Ready {
				state = T("asrNotReady")
			}
			if name == "" {
				name = T("asrNoName")
			}
			return T("asrOk", state, name)
		}, apply
	}
}

// localCleanup is the local text cleanup service: an OpenAI-compatible API
// with its own address, key and model, used only when cleanup is on.
func (p *prefsWindow) localCleanup(c *ui.Context, s config.Settings) {
	t := c.Theme()
	k := p.conn("local.cleanup")
	p.dialogHead(c, providers.Local, "oMLX · "+T("mCleanup"), nil, nil)
	ui.Text(c, T("helpClean")).FontSize(11.5).TextColor(t.TextMuted)
	url := p.draft("clean.url", s.CleanupURL)
	model := p.draft("clean.model", s.CleanupModel)
	labelled(c, "url", T("mUrl"), "", func() {
		if groupTextInput(c.Key("in"), url, true).Label(T("mCleanup") + " — " + T("mUrl")).Grow(1).Changed() {
			k.invalidate()
			p.models = nil // another address: the list no longer applies
			p.change(func(s *config.Settings) { s.CleanupURL = strings.TrimSpace(*url) })
		}
	})
	labelled(c, "key", T("mKey"), T("mKeyHint"), func() { p.secretField(c, cleanupKeyAccount, T("mKey"), k.invalidate, nil) })
	labelled(c, "model", T("mModelId"), "", func() {
		// Suggestions are the models the service listed, without those
		// whose names say they are not text models. Any ID can be typed.
		var suggestions []string
		for _, id := range p.models {
			if cleanup.KindOf(id) == cleanup.KindText {
				suggestions = append(suggestions, id)
			}
		}
		if len(suggestions) == 0 {
			suggestions = []string{config.DefaultCleanupModel}
		}
		if ui.Autocomplete(c.Key("in"), model, suggestions).Label(T("mCleanup") + " — " + T("mModelId")).Grow(1).Changed() {
			k.invalidate()
			p.change(func(s *config.Settings) { s.CleanupModel = strings.TrimSpace(*model) })
		}
	})
	opts := func() cleanup.Options {
		key := *p.draft("secret:"+cleanupKeyAccount, "") // a key being typed can be tried before it is saved
		if key == "" {
			key = p.a.secret(cleanupKeyAccount)
		}
		return cleanup.Options{BaseURL: strings.TrimSpace(*url), Model: strings.TrimSpace(*model), APIKey: key}
	}
	busy := k.loading != nil
	ui.Row(c.Key("actions")).Gap(8).AlignItems(ui.Center).Children(func() {
		if labelButton(c.Key("fetch"), icRefresh, T("fetchModels")).Disabled(busy).Clicked() {
			o := opts()
			p.run(k, "fetching", func(ctx context.Context) (bool, func() string, func()) {
				models, err := cleanup.ListModels(ctx, o)
				if err != nil {
					return false, func() string { return explain(err) }, nil
				}
				return true, func() string { return T("fetchOk", len(models)) }, func() { p.models, p.listOpen = models, true }
			})
		}
		if labelButton(c.Key("test"), icLink, T("testConn")).Disabled(busy).Clicked() {
			o := opts()
			p.run(k, "testing", func(ctx context.Context) (bool, func() string, func()) {
				if o.Model == "" {
					return false, func() string { return T("testNoModel") }, nil
				}
				// GET /models only: no text is sent and nothing is inferred.
				res, err := cleanup.Check(ctx, o)
				if err != nil {
					return false, func() string { return explain(err) }, nil
				}
				learned := func() { p.models = res.Models }
				if !res.Listed {
					return false, func() string { return T("testNotListed", o.Model, len(res.Models)) }, learned
				}
				return true, func() string { return T("testOk", o.Model) }, learned
			})
		}
		ui.Spacer(c)
		p.defaultControl(c, "default", providers.KindCleanup, providers.Local, "", s, providers.LocalReady(providers.KindCleanup, s))
	})
	ui.Row(c.Key("status")).Gap(8).Children(func() { connStatus(c, k) })
	if len(p.models) > 0 {
		ui.Collapsible(c.Key("inventory"), T("mAll", len(p.models)), &p.listOpen, func() {
			ui.Scroll(c).MaxHeight(180).Gap(4).Padding(6, 0).Children(func() {
				for _, id := range p.models {
					ui.Row(c.Key(id)).Gap(8).Children(func() {
						ui.Text(c, id).FontSize(12).Grow(1).Selectable()
						if id == strings.TrimSpace(*model) {
							ui.Text(c, T("mInUse")).FontSize(11).TextColor(t.Accent).SingleLine()
						}
						if kind := cleanup.KindOf(id); kind != cleanup.KindText {
							ui.Text(c, T(kindNames[kind])).FontSize(11).TextColor(t.TextMuted).SingleLine()
						}
					})
				}
				ui.Text(c, T("kGuess")).FontSize(11).TextColor(t.TextMuted)
			})
		})
	}
}

var kindNames = map[string]string{
	cleanup.KindEmbedding: "kEmbedding", cleanup.KindReranker: "kReranker",
	cleanup.KindSpeech: "kSpeech", cleanup.KindDraft: "kDraft", cleanup.KindOther: "kOther",
}

// customEditor adds or edits a provider of the user's own: a name, an
// address, an optional key, model IDs and whether it is on.
func (p *prefsWindow) customEditor(c *ui.Context, s config.Settings) {
	t := c.Theme()
	id, kind := p.dlg.id, p.dlg.kind
	slot := customSlot(id, kind)
	d := p.custom[slot]
	if d == nil { // the draft went with a Save or a Cancel in this same frame
		return
	}
	k := p.conn("custom:" + slot)
	title := T("addProvider")
	if id != "" {
		title = T("editProvider")
	}
	p.dialogHead(c, id, title+" · "+T(kindLabels[kind]), &d.enabled, func() {})
	// What kind of service this must be, said before anything is typed.
	note := "customLlmNote"
	if kind == providers.KindASR {
		note = "customAsrNote"
	}
	ui.Text(c, T(note)).FontSize(11.5).TextColor(t.TextMuted)
	labelled(c, "name", T("provName"), "", func() {
		if groupTextInput(c.Key("in"), &d.name, true).Label(T("provName")).Grow(1).Changed() {
			d.err = ""
		}
	})
	labelled(c, "url", T("mUrl"), "", func() {
		if groupTextInput(c.Key("in"), &d.url, true).Placeholder("https://").Label(T("mUrl")).Grow(1).Changed() {
			d.err = ""
			k.invalidate()
			d.probeText = ""
		}
	})
	account := providers.CustomAccount(id)
	stored := id != "" && p.a.secret(account) != ""
	placeholder := T("mKeyNone")
	if stored {
		placeholder = T("mKeyStored")
	}
	labelled(c, "key", T("keyOptional"), T("mKeyHint"), func() {
		in := groupTextInput(c.Key("in"), &d.key, true).Placeholder(placeholder).Label(T("keyOptional")).Grow(1)
		if !p.shown[slot] {
			in.Password()
		}
		if in.Changed() {
			k.invalidate()
			d.probeText = ""
		}
		svg, tip := icEye, T("showKey")
		if p.shown[slot] {
			svg, tip = icEyeOff, T("hideKey")
		}
		if iconButton(c.Key("show"), svg, tip).Clicked() {
			p.shown[slot] = !p.shown[slot]
		}
	})
	labelled(c, "models", T("gModels"), "", func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			if len(d.models) == 0 {
				ui.Text(c, T("noModelsYet")).FontSize(11.5).TextColor(faintText(t))
			}
			for _, m := range d.models {
				ui.Row(c.Key(m)).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m).FontSize(12.5).Grow(1).Selectable()
					if iconButton(c.Key("rm"), icClear, T("removeModel", m)).Clicked() {
						d.models = slices.DeleteFunc(d.models, func(x string) bool { return x == m })
						k.invalidate()
						d.probeText = ""
					}
				})
			}
			ui.Row(c.Key("add")).Gap(6).Children(func() {
				in := groupTextInput(c.Key("in"), &d.add, true).Placeholder(T("addModelId")).Label(T("addModelId")).Grow(1)
				submitted := in.Submitted()
				if labelButton(c.Key("btn"), icPlus, T("add")).Clicked() || submitted {
					model := strings.TrimSpace(d.add)
					switch {
					case model == "":
						d.err = "errEmptyModel"
					case slices.ContainsFunc(d.models, func(x string) bool { return strings.EqualFold(x, model) }):
						d.err = "errDupModel"
					default:
						d.models, d.add, d.err = append(d.models, model), "", ""
						k.invalidate()
						d.probeText = ""
					}
				}
			})
		})
	})

	if kind == providers.KindCleanup {
		ui.Collapsible(c.Key("advanced"), T("customAdvanced"), &d.advanced, func() {
			labelled(c, "thinking-mode", T("thinkingMode"), "", func() {
				p.standardSelect(c, "custom-thinking-mode", T("thinkingMode"), d.thinkingMode, "", 260, false, []selectOption{
					{Value: "server", Label: T("thinkingServer")}, {Value: "deepseek", Label: T("thinkingDeepSeek")}, {Value: "omlx", Label: T("thinkingOmlx")}, {Value: "custom", Label: T("thinkingCustom")},
				}, func(mode string) { d.thinkingMode = mode; d.err = ""; d.probeText = ""; k.invalidate() })
			})
			if d.thinkingMode == "custom" {
				ui.Text(c, T("thinkingJSON")).FontSize(12)
				if ui.TextArea(c.Key("thinking-json"), &d.thinkingParams).Label(T("thinkingJSON")).Placeholder(`{"thinking":{"type":"disabled"}}`).Lines(3, 6).FillWidth().Changed() {
					d.err = ""
					d.probeText = ""
					k.invalidate()
				}
				if _, err := cleanup.ThinkingParams(d.thinkingMode, d.thinkingParams); err != nil {
					ui.Text(c, T("errThinkingParams")).FontSize(11.5).TextColor(t.Danger)
				}
			}
		})
	}
	if d.err != "" {
		ui.Text(c, T(d.err)).FontSize(12).TextColor(t.Danger)
	}

	// A real check of the address: the model list for a language model, the
	// health document for a speech service. Neither sends text or audio.
	ui.Row(c.Key("test")).Gap(8).AlignItems(ui.Center).Children(func() {
		if labelButton(c.Key("btn"), icLink, T("testConn")).Disabled(k.loading != nil).Clicked() {
			url := strings.TrimSpace(d.url)
			if kind == providers.KindASR {
				p.run(k, "testing", inspectASR(url, nil))
			} else {
				key := d.key
				if key == "" && id != "" {
					key = p.a.secret(account)
				}
				o := cleanup.Options{BaseURL: providers.APIBase(url), APIKey: key}
				p.run(k, "testing", func(ctx context.Context) (bool, func() string, func()) {
					models, err := cleanup.ListModels(ctx, o)
					if err != nil {
						return false, func() string { return explain(err) }, nil
					}
					return true, func() string { return T("fetchOk", len(models)) }, nil
				})
			}
		}
		connStatus(c, k)
	})

	if kind == providers.KindCleanup {
		if !slices.Contains(d.models, d.testModel) {
			d.testModel = ""
			if len(d.models) > 0 {
				d.testModel = d.models[0]
			}
		}
		opts := make([]selectOption, 0, len(d.models))
		for _, m := range d.models {
			opts = append(opts, selectOption{Value: m, Label: m})
		}
		labelled(c, "test-model", T("thinkingTestModel"), "", func() {
			p.standardSelect(c, "custom-test-model", T("thinkingTestModel"), d.testModel, T("noModelsYet"), 260, len(opts) == 0, opts, func(m string) { d.testModel = m; d.probeText = ""; k.invalidate() })
		})
		if labelButton(c.Key("probe"), icLink, T("thinkingTest")).Disabled(k.loading != nil || d.testModel == "").Clicked() {
			_, err := cleanup.ThinkingParams(d.thinkingMode, d.thinkingParams)
			if err != nil {
				d.err = "errThinkingParams"
			} else {
				key := d.key
				if key == "" && id != "" {
					key = p.a.secret(account)
				}
				o := cleanup.Options{BaseURL: providers.APIBase(strings.TrimSpace(d.url)), Model: d.testModel, APIKey: key, ThinkingMode: d.thinkingMode, ThinkingParams: d.thinkingParams}
				d.probeText = ""
				p.runWithTimeout(k, "testing", 30*time.Second, func(ctx context.Context) (bool, func() string, func()) {
					start := time.Now()
					out, err := cleanup.Run(ctx, o, "嗯，请保留这2个按钮，先不要发送，等我确认以后再发送。")
					elapsed := time.Since(start).Seconds()
					if err != nil {
						return false, func() string { return explain(err) }, nil
					}
					return true, func() string { return T("thinkingTestOK", elapsed) }, func() { d.probeText = out }
				})
			}
		}
		if d.probeText != "" {
			ui.Text(c, d.probeText).FontSize(12).Selectable()
		}
	}
	ui.Row(c.Key("foot")).Gap(8).AlignItems(ui.Center).Children(func() {
		if id != "" && labelButton(c.Key("remove"), icTrash, T("removeProvider")).Clicked() {
			name := d.name
			cleared := false
			p.change(func(s *config.Settings) {
				s.Providers.Custom = slices.DeleteFunc(s.Providers.Custom, func(x config.CustomProvider) bool { return x.ID == id })
				cleared = unassign(s, kind, id, "")
			})
			go p.a.setSecret(account, "") // its key goes with it
			delete(p.custom, slot)
			k.invalidate()
			p.dlgOpen = false
			if cleared {
				p.say("assignCleared", name)
			} else {
				p.say("providerRemoved", name)
			}
		}
		ui.Spacer(c)
		if ui.Button(c.Key("cancel"), T("cancelBtn")).Clicked() {
			delete(p.custom, slot) // Cancel discards; closing keeps the draft
			k.invalidate()
			p.dlgOpen = false
		}
		if ui.PrimaryButton(c.Key("save"), T("save")).Clicked() {
			name, url := strings.TrimSpace(d.name), strings.TrimSpace(d.url)
			_, thinkingErr := cleanup.ThinkingParams(d.thinkingMode, d.thinkingParams)
			switch {
			case kind == providers.KindCleanup && thinkingErr != nil:
				d.err = "errThinkingParams"
			case name == "":
				d.err = "errName"
			case !providers.ValidURL(url):
				d.err = "eBadURL"
			default:
				saved := config.CustomProvider{ID: id, Kind: kind, Name: name, URL: url, Models: slices.Clone(d.models), Enabled: d.enabled}
				if kind == providers.KindCleanup {
					saved.ThinkingMode = d.thinkingMode
					saved.ThinkingParams = d.thinkingParams
				}
				if saved.ID == "" {
					saved.ID = "cp" + strconv.FormatInt(time.Now().UnixNano(), 36)
				}
				cleared := false
				p.change(func(s *config.Settings) {
					if i := slices.IndexFunc(s.Providers.Custom, func(x config.CustomProvider) bool { return x.ID == saved.ID }); i >= 0 {
						s.Providers.Custom[i] = saved
					} else {
						s.Providers.Custom = append(s.Providers.Custom, saved)
					}
					// A default this provider can no longer serve is cleared, never replaced.
					a := providers.Assigned(kind, *s)
					if a.Provider == saved.ID && (!saved.Enabled || !slices.Contains(saved.Models, a.Model)) {
						cleared = unassign(s, kind, saved.ID, "")
					}
				})
				if key := d.key; key != "" {
					go func() { // encrypted file I/O, off the main thread
						if err := p.a.setSecret(providers.CustomAccount(saved.ID), key); err != nil {
							p.win.Update(func() { p.say("keyNotSaved", err.Error()) })
						}
					}()
				}
				delete(p.custom, slot)
				k.invalidate()
				p.dlgOpen = false
				if cleared {
					p.say("assignCleared", name)
				} else {
					p.say("providerSaved", name)
				}
			}
		}
	})
}
