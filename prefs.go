package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/macos"
)

// prefsWindow is the settings: a window of its own above whatever page the
// main window shows, with categories on the left and their content on the
// right, as mock/index.html has it. Settings apply as they change. All of
// its state is in this struct, so changing category, opening a provider's
// editor or closing and reopening the window loses nothing that was typed.
type prefsWindow struct {
	a      *app
	win    *mygo.Window
	cat    string // "general", "perms", "dictation", "models" or "privacy"
	status func() string

	audioDays  float64
	fnConflict string // what the system does on Fn

	// Models (models.go).
	pickOpen    map[string]*bool       // kind → its default picker is open
	pickHandles map[string][]ui.Handle // kind → the picker's rows, for the arrow keys
	pickFocus   map[string]bool        // kind → its picker just opened and takes the focus
	dlgOpen     bool
	addOpen     bool               // the "Add model" dialog, above the editor
	addErr      string             // a text key
	dlg         dialogRef          // what the open editor edits
	drafts      map[string]*string // text fields being edited, by a key of their own
	shown       map[string]bool    // secret field key → shown in clear
	conns       map[string]*conn   // connection checks, by provider
	models      []string           // the local cleanup service's list, once fetched
	listOpen    bool
	custom      map[string]*customDraft // custom provider editors' drafts
}

// conn is a connection check of one provider. Checks run off the main
// thread; a newer check, or an edit of the provider's fields, makes an
// older one's result stale, and stale results are dropped.
type conn struct {
	gen     int
	cancel  context.CancelFunc
	loading func() string // what is running, or nil
	result  func() string // the last outcome in the current language, or nil
	ok      bool
}

// invalidate cancels a running check and forgets the last result.
func (k *conn) invalidate() {
	k.gen++
	if k.cancel != nil {
		k.cancel()
	}
	k.cancel, k.loading, k.result = nil, nil, nil
}

func newPrefsWindow(a *app) *prefsWindow {
	p := &prefsWindow{a: a, cat: "general",
		pickOpen: map[string]*bool{}, pickHandles: map[string][]ui.Handle{}, pickFocus: map[string]bool{},
		drafts: map[string]*string{}, shown: map[string]bool{}, conns: map[string]*conn{}, custom: map[string]*customDraft{}}
	p.audioDays = float64(a.settings.Get().AudioRetentionDays)
	p.fnConflict = macos.FnKeyConflict()
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           T("navSettings"),
		Width:           980,
		Height:          760,
		MinWidth:        680,
		MinHeight:       460,
		Hidden:          true,
		StateKey:        "prefs",
		TitleBarStyle:   mygo.TitleBarHidden,
		DisableMinimize: true,
		Content:         ui.View(p.view),
	})
	// Closing hides it, with everything in it kept.
	p.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			p.close()
		}
	})
	return p
}

func (p *prefsWindow) visible() bool { return p != nil && p.win != nil && p.win.IsVisible() }

// open shows the settings, at a category when one is named.
func (p *prefsWindow) open(category string) {
	if category != "" {
		p.cat = category
	}
	p.win.SetTitle(T("navSettings"))
	p.win.Show()
	p.win.Focus()
	p.a.main.refresh() // the rail marks Settings as open
}

// close hides the settings and gives the main window the keyboard back.
func (p *prefsWindow) close() {
	p.win.Hide()
	if p.a.main.win.IsVisible() {
		p.a.main.win.Focus()
	}
	p.a.main.refresh()
}

func (p *prefsWindow) toggle(category string) {
	if p.visible() {
		p.close()
	} else {
		p.open(category)
	}
}

// refresh draws the window again, from any goroutine.
func (p *prefsWindow) refresh() {
	if p != nil && p.win != nil {
		p.win.Invalidate()
	}
}

func (p *prefsWindow) say(key string, args ...any) {
	p.status = func() string { return T(key, args...) }
}

// change edits the saved settings and reports a failure to save them.
func (p *prefsWindow) change(fn func(s *config.Settings)) {
	s := p.a.settings.Get()
	fn(&s)
	if err := p.a.settings.Set(s); err != nil {
		p.say("notSaved", err.Error())
	}
	p.a.main.refresh() // Home shows the trigger and the defaults
}

// draft is the text of a field being edited, kept by a key so that it
// outlives the frame, the category and the dialog it is typed in.
func (p *prefsWindow) draft(key, initial string) *string {
	if d, ok := p.drafts[key]; ok {
		return d
	}
	d := initial
	p.drafts[key] = &d
	return &d
}

// conn is the connection check of a provider.
func (p *prefsWindow) conn(key string) *conn {
	if k, ok := p.conns[key]; ok {
		return k
	}
	k := &conn{}
	p.conns[key] = k
	return k
}

// run starts a check off the main thread. Its result is shown, and apply
// (what the check learned, if anything) is run, only if no newer check
// and no edit came in between.
func (p *prefsWindow) run(k *conn, loadingKey string, work func(ctx context.Context) (ok bool, result func() string, apply func())) {
	k.invalidate()
	gen := k.gen
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	k.cancel = cancel
	k.loading = func() string { return T(loadingKey) }
	go func() {
		ok, result, apply := work(ctx)
		cancel()
		p.win.Update(func() {
			if k.gen != gen {
				return // stale: the fields changed, or another check started
			}
			k.loading, k.cancel, k.ok, k.result = nil, nil, ok, result
			if apply != nil {
				apply()
			}
		})
	}()
}

// connStatus is the line beside a provider's buttons: what is running, or
// what the last check found.
func connStatus(c *ui.Context, k *conn) {
	t := c.Theme()
	switch {
	case k.loading != nil:
		ui.Spinner(c)
		ui.Text(c, k.loading()).FontSize(12).TextColor(t.TextMuted)
	case k.result != nil:
		col := t.Danger
		if k.ok {
			col = t.Success
		}
		ui.Text(c, k.result()).FontSize(12).TextColor(col).Grow(1).Selectable()
	}
}

var categories = []struct {
	id, label string
	icon      *ui.SVG
}{
	{"general", "gGeneral", icSliders},
	{"perms", "gPerms", icShield},
	{"dictation", "navDictate", icMic},
	{"models", "gModels", icSparkle},
	{"privacy", "gPrivacy", icLock},
}

func (p *prefsWindow) view(c *ui.Context) {
	t := useTheme(c)
	bar := c.TitleBar()
	// Escape and Cmd+W close the window. An open editor or picker takes
	// Escape first: the window only closes when none is open.
	overlay := p.dlgOpen || p.addOpen
	for _, open := range p.pickOpen {
		overlay = overlay || *open
	}
	if !overlay && (c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW)) {
		p.close()
	}
	title := ""
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// The categories: 176 px on the subtle fill, 38 px rows.
		ui.Column(c.Key("nav")).Width(176).Background(fillColor(t)).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Box(c).Height(max(bar.Height, 52)).FillWidth().DragWindow()
			ui.Column(c).Padding(4, 10, 12).Gap(2).Children(func() {
				for _, cat := range categories {
					on := p.cat == cat.id
					if on {
						title = T(cat.label)
					}
					// ButtonBase centers its children by default; the mock uses
					// a shared leading edge for every icon and label.
					row := ui.ButtonBase(c.Key(cat.id)).FillWidth().Height(38).Padding(0, 10).Gap(10).Radius(8).Justify(ui.Start).AlignItems(ui.Center).Label(T(cat.label))
					row.Children(func() {
						ui.Icon(c, cat.icon).Size(18, 18).Shrink(0).TextColor(t.Text)
						weight := 400
						if on {
							weight = 600
						}
						ui.Text(c, T(cat.label)).FontSize(13).FontWeight(weight).SingleLine()
					})
					switch {
					case on:
						row.Background(railSelected(t))
					case row.Hovered():
						row.Background(hoverColor(t))
					}
					if row.Clicked() {
						p.cat = cat.id
						p.status = nil
					}
				}
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Height(52).Padding(0, 30).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(600)
			})
			// The window scrolls inside; a category keeps its own scroll.
			ui.Scroll(c.Key("cat-"+p.cat)).Grow(1).Padding(28, 30).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(720).Gap(10).Children(func() {
					switch p.cat {
					case "perms":
						p.permissions(c)
					case "dictation":
						p.dictation(c)
					case "models":
						p.modelsView(c)
					case "privacy":
						p.privacy(c)
					default:
						p.general(c)
					}
				})
			})
			if p.status != nil {
				ui.Row(c).Padding(8, 30).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
					ui.Text(c, p.status()).FontSize(12).TextColor(t.TextMuted).Selectable()
				})
			}
		})
	})
	// The editor of a provider, over the window (models.go).
	p.dialog(c)
}

// card is a box of rows with hairlines between them.
func card(c *ui.Context, key string, rows func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).Radius(10).Background(cardColor(t)).Border(1, t.Border).Gap(1).Dividers(1, t.Border).Children(rows)
}

// setting is a row: an optional icon, a label with a hint, and a control.
func setting(c *ui.Context, key string, svg *ui.SVG, label, hint string, control func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(16).Padding(11, 14).AlignItems(ui.Center).Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(16).TextColor(t.TextMuted)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, label)
			if hint != "" {
				ui.Text(c, hint).FontSize(11.5).TextColor(t.TextMuted)
			}
		})
		ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
	})
}

func (p *prefsWindow) general(c *ui.Context) {
	card(c, "general", func() {
		setting(c, "lang", nil, T("language"), "", func() {
			i := int(langIndex.Load())
			if ui.Segmented(c.Key("seg"), &i, langNames...).Changed() {
				if err := p.a.setLanguage(i); err != nil {
					p.say("notSaved", err.Error())
				}
				p.win.SetTitle(T("navSettings"))
			}
		})
	})
}

// permissions shows the three permissions as macOS reports them, each with
// a way to grant it when it is missing.
func (p *prefsWindow) permissions(c *ui.Context) {
	t := c.Theme()
	state := func(granted bool, action string, act func()) {
		if granted {
			ui.Box(c).Size(8, 8).Radius(4).Background(t.Success)
			ui.Text(c, T("granted")).FontSize(12).SingleLine()
			return
		}
		ui.Text(c, T("notGranted")).FontSize(12).TextColor(t.Danger).SingleLine()
		if ui.Button(c.Key("grant"), action).Clicked() {
			act()
		}
	}
	card(c, "perms", func() {
		mic := macos.MicrophoneStatus()
		setting(c, "mic", icMic, T("pMic"), "", func() {
			state(mic == "authorized", T("openSettings"), func() {
				if mic == "undetermined" {
					macos.RequestMicrophone(func(bool) { p.a.refreshAll() })
				} else {
					macos.OpenPrivacySettings("Privacy_Microphone")
				}
			})
		})
		setting(c, "ax", icPerson, T("pAx"), "", func() {
			state(macos.AccessibilityTrusted(false), T("request"), func() { macos.AccessibilityTrusted(true) })
		})
		setting(c, "input", icKeyboard, T("pInput"), "", func() {
			state(macos.InputMonitoringGranted(false), T("openSettings"), func() { macos.OpenPrivacySettings("Privacy_ListenEvent") })
		})
	})
	if !macos.AccessibilityTrusted(false) || !macos.InputMonitoringGranted(false) {
		ui.Text(c, T("permHint")).FontSize(11.5).TextColor(t.TextMuted)
	}
}

var fnActions = map[string]string{
	"change the input source": "fnInput", "show Emoji & Symbols": "fnEmoji", "start Dictation": "fnDictation",
}

// dictation is the trigger and the one cleanup switch. Where cleanup goes
// is the Text cleanup default's business, under Models.
func (p *prefsWindow) dictation(c *ui.Context) {
	t := c.Theme()
	s := p.a.settings.Get()
	if s.TriggerKey == macos.KeyFn && p.fnConflict != "" {
		what := p.fnConflict
		if k, ok := fnActions[what]; ok {
			what = T(k)
		}
		ui.Text(c, T("fnConflict", what)).FontSize(12).TextColor(t.Warning).Padding(10, 14).Radius(8).Background(noticeColor(t))
	}
	card(c, "dictation", func() {
		setting(c, "key", nil, T("sKey"), T("sKeyHint"), func() { p.triggerKey(c, s) })
		setting(c, "mode", nil, T("sMode"), T("sModeHint"), func() {
			mode := 0
			if s.TriggerMode == config.TriggerHold {
				mode = 1
			}
			// A custom shortcut reports no release: it can only toggle.
			seg := ui.Segmented(c.Key("seg"), &mode, T("mToggle"), T("mHold")).Disabled(!macos.IsModifierKey(s.TriggerKey))
			if seg.Changed() {
				p.change(func(s *config.Settings) {
					s.TriggerMode = []string{config.TriggerToggle, config.TriggerHold}[mode]
				})
			}
		})
		setting(c, "cleanup", nil, T("sCleanDefault"), T("sCleanDefaultHint"), func() {
			on := s.CleanupDefault
			if ui.Switch(c.Key("sw"), &on).Label(T("sCleanDefault")).Changed() {
				p.a.setCleanup(on)
			}
		})
	})
}

// triggerKey chooses among the modifier keys. A custom shortcut saved in
// settings.json stays selected and is shown as such.
func (p *prefsWindow) triggerKey(c *ui.Context, s config.Settings) {
	keys := []string{macos.KeyFn, macos.KeyRightOption, macos.KeyRightCommand, macos.KeyRightControl}
	names := []string{"Fn", T("rOption"), T("rCommand"), T("rControl")}
	if !macos.IsModifierKey(s.TriggerKey) {
		keys = append(keys, s.TriggerKey)
		names = append(names, T("customKey", s.TriggerKey))
	}
	chosen := names[0]
	for i, k := range keys {
		if k == s.TriggerKey {
			chosen = names[i]
		}
	}
	if ui.Select(c.Key("sel"), &chosen, names).Label(T("sKey")).Width(170).Changed() {
		for i, n := range names {
			if n == chosen && keys[i] != s.TriggerKey {
				key := keys[i]
				p.change(func(s *config.Settings) { s.TriggerKey = key })
				p.a.bindTrigger()
				p.fnConflict = macos.FnKeyConflict()
			}
		}
	}
}

func (p *prefsWindow) privacy(c *ui.Context) {
	s := p.a.settings.Get()
	card(c, "privacy", func() {
		setting(c, "audio", nil, T("sAudio"), T("sAudioHint"), func() {
			on := s.SaveAudio
			if ui.Switch(c.Key("sw"), &on).Label(T("sAudio")).Changed() {
				p.change(func(s *config.Settings) { s.SaveAudio = on })
			}
		})
		setting(c, "days", nil, T("sDays"), T("sDaysHint"), func() {
			// Match the mock's single field. NumberInput includes two extra
			// buttons whose intrinsic widths overflow a constrained wrapper.
			draft := p.draft("audioDays", strconv.Itoa(s.AudioRetentionDays))
			in := ui.TextInput(c.Key("n"), draft).Label(T("sDays")).Width(150)
			commit := in.Submitted() || !in.Focused()
			if in.Shortcut(0, ui.KeyUp) {
				*draft = strconv.Itoa(min(3650, s.AudioRetentionDays+1))
				commit = true
			}
			if in.Shortcut(0, ui.KeyDown) {
				*draft = strconv.Itoa(max(0, s.AudioRetentionDays-1))
				commit = true
			}
			if commit {
				days, err := strconv.Atoi(strings.TrimSpace(*draft))
				if err != nil || days < 0 || days > 3650 {
					*draft = strconv.Itoa(s.AudioRetentionDays)
				} else if days != s.AudioRetentionDays {
					p.change(func(s *config.Settings) { s.AudioRetentionDays = days })
					go p.a.purge()
				}
			}
		})
		setting(c, "observe", nil, T("sObserve"), T("sObserveHint"), func() {
			on := s.ObserveEdits
			if ui.Switch(c.Key("sw"), &on).Label(T("sObserve")).Changed() {
				p.change(func(s *config.Settings) { s.ObserveEdits = on })
			}
		})

	})
	ui.Row(c.Key("clear")).Children(func() {
		if labelButton(c.Key("btn"), icTrash, T("clearAll")).Clicked() {
			go p.clearHistory()
		}
	})
}

// clearHistory asks first: it deletes every record and every recording.
func (p *prefsWindow) clearHistory() {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  p.win,
		Type:    mygo.MessageWarning,
		Message: T("clearAsk"),
		Detail:  T("clearDetail"),
		Buttons: []string{T("clearYes"), T("cancel")},
	})
	if err != nil || res.Button != 0 {
		return
	}
	err = p.a.history.Clear()
	p.a.main.win.Update(func() {
		p.a.main.stopPlayer()
		p.a.main.selected = ""
	})
	p.win.Update(func() {
		if err != nil {
			p.say("failed", err.Error())
		} else {
			p.say("cleared")
		}
	})
}
