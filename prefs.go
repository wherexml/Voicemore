package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
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
	cat    string // "general", "perms", "models" or "privacy"
	status func() string

	dragWin    *mygo.Window
	dragPane   string
	bundlePath string // exact running bundle, never a backup or guessed install path
	audioDays  float64
	fnConflict string // what the system does on Fn
	mics       []macos.InputDevice
	micsAt     time.Time // when mics was read: microphones come and go

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
	permsWere   bool                    // the permissions were all granted when last looked at
	// guideReturn follows a visit from the first-run guide: 1 once it has
	// opened these settings, 2 once an editor has been open. When that
	// editor closes, the settings close too and the guide is in front again.
	guideReturn int
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
	p.bundlePath = runningAppBundle()
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
	if category == "dictation" {
		category = "general"
	}
	if category != "" {
		p.dlgOpen, p.addOpen = false, false
		p.cat = category
	}
	p.refresh()
	p.win.SetTitle(T("navSettings"))
	p.win.Show()
	p.win.Focus()
	p.a.main.refresh() // the rail marks Settings as open
}

// close hides the settings and gives the main window the keyboard back.
func (p *prefsWindow) close() {
	if p.dragWin != nil {
		p.dragWin.Hide()
	}
	p.guideReturn = 0
	p.win.Hide()
	if p.a.main.win.IsVisible() {
		p.a.main.win.Focus()
	}
	if p.a.onboard.visible() {
		p.a.onboard.win.Focus()
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
	if p.guideReturn > 0 {
		if p.dlgOpen {
			p.guideReturn = 2
		} else if p.guideReturn == 2 {
			p.close() // back to the guide
		}
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

// hintDelay is how long the pointer rests on a row before its hint shows,
// so that passing over the rows does not flash their hints.
const hintDelay = 300 * time.Millisecond

// hintHover is when the pointer came onto each settings row it is on, by
// row; a row it left has no entry. Only the main thread touches it.
var hintHover = map[string]time.Time{}

// setting is a row: an optional icon, a label with a hint, and a control.
// The hint explains the setting: it shows once the pointer has rested on
// the row for hintDelay, and goes when the pointer leaves.
func setting(c *ui.Context, key string, svg *ui.SVG, label, hint string, control func()) {
	settingRow(c, key, svg, label, hint, false, control)
}

// settingRow is setting; with notice, the hint is a state the user must
// see (a missing device, an approval to give) and always shows.
func settingRow(c *ui.Context, key string, svg *ui.SVG, label, hint string, notice bool, control func()) {
	t := c.Theme()
	row := ui.Row(c.Key(key)).Gap(16).Padding(11, 14).AlignItems(ui.Center)
	shown := notice
	if !notice && hint != "" {
		// Rows of different cards may share a key: the label tells them apart.
		id := key + "\x00" + label
		if row.Hovered() {
			since, ok := hintHover[id]
			if !ok {
				since = time.Now()
				hintHover[id] = since
			}
			if wait := hintDelay - time.Since(since); wait > 0 {
				c.After(wait)
			} else {
				shown = true
			}
		} else {
			delete(hintHover, id) // left before the delay: nothing shows
		}
	}
	row.Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(16).TextColor(t.TextMuted)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, label)
			if hint != "" {
				// A hidden hint keeps its room, so the rows never move.
				col := t.TextMuted
				if !shown {
					col = ui.Transparent
				}
				ui.Text(c, hint).FontSize(11.5).TextColor(col)
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
		// Light, dark, or the system's. It applies to the app's own
		// surfaces at once; what macOS draws follows the system.
		setting(c, "appearance", nil, T("appearance"), "", func() {
			modes := []string{"light", "dark", ""}
			i := 2
			for k, mode := range modes {
				if mode == appearance {
					i = k
				}
			}
			if ui.Segmented(c.Key("seg"), &i, T("apLight"), T("apDark"), T("apAuto")).Changed() {
				appearance = modes[i]
				p.change(func(s *config.Settings) { s.Appearance = modes[i] })
				p.a.refreshAll()
			}
		})
		p.loginItem(c)
		setting(c, "dock", nil, T("sDock"), T("sDockHint"), func() {
			on := p.a.settings.Get().ShowInDock
			if ui.Switch(c.Key("sw"), &on).Label(T("sDock")).Changed() {
				p.change(func(s *config.Settings) { s.ShowInDock = on })
				macos.SetDockIcon(on)
				p.win.Focus()
			}
		})
		// The first-run guide, for whoever left it or never saw it.
		setting(c, "guide", nil, T("obGuide"), T("obGuideHint"), func() {
			if ui.Button(c.Key("open"), T("obShowBtn")).Clicked() {
				p.close()
				p.a.onboard.open()
			}
		})
	})
	p.audio(c)
	p.dictation(c)
}

// loginItem is the app's entry in System Settings → Login Items. macOS
// keeps it, so the switch shows what the system reports, not a saved copy.
func (p *prefsWindow) loginItem(c *ui.Context) {
	status := macos.LoginItemStatus()
	hint := T("sLoginItemHint")
	switch status {
	case "approval":
		hint = T("loginApproval")
	case "unavailable":
		hint = T("loginUnavailable")
	}
	settingRow(c, "login", nil, T("sLoginItem"), hint, status == "approval" || status == "unavailable", func() {
		if status == "approval" && ui.Button(c.Key("open"), T("openSettings")).Clicked() {
			macos.OpenLoginItemsSettings()
		}
		on := status == "enabled" || status == "approval"
		if ui.Switch(c.Key("sw"), &on).Label(T("sLoginItem")).Disabled(status == "unavailable").Changed() {
			if err := macos.SetLoginItem(on); err != nil {
				p.say("loginFailed", err.Error())
			} else {
				p.status = nil
			}
			if on && macos.LoginItemStatus() == "approval" {
				macos.OpenLoginItemsSettings()
			}
		}
	})
}

// audio is the microphone, the cues and muting the output while recording.
func (p *prefsWindow) audio(c *ui.Context) {
	t := c.Theme()
	s := p.a.settings.Get()
	// Microphones are plugged in and out while the page is open.
	c.After(2 * time.Second)
	if time.Since(p.micsAt) >= 2*time.Second {
		p.mics, p.micsAt = macos.InputDevices(), time.Now()
	}
	auto, connected := T("micAuto"), false
	uids := []string{""}
	for _, m := range p.mics {
		if m.Default {
			auto = T("micAutoNamed", m.Name)
		}
	}
	names := []string{auto}
	for _, m := range p.mics {
		uids = append(uids, m.UID)
		names = append(names, m.Name)
		connected = connected || m.UID == s.InputDevice
	}
	hint, notice := T("sMicDeviceHint"), false
	if s.InputDevice != "" && !connected {
		// It stays chosen, and is used again once it is back.
		name := s.InputDeviceName
		if name == "" {
			name = s.InputDevice
		}
		uids = append(uids, s.InputDevice)
		names = append(names, T("micDisconnected", name))
		hint, notice = T("micDisconnectedHint"), true
	}
	ui.Text(c, T("gAudio")).FontSize(16).TextColor(t.Text)
	card(c, "audio", func() {
		settingRow(c, "mic", nil, T("sMicDevice"), hint, notice, func() {
			chosen := names[0]
			for i, uid := range uids {
				if uid == s.InputDevice {
					chosen = names[i]
				}
			}
			if ui.Select(c.Key("sel"), &chosen, names).Label(T("sMicDevice")).Width(240).Changed() {
				for i, n := range names {
					if n != chosen || uids[i] == s.InputDevice {
						continue
					}
					uid, name := uids[i], ""
					for _, m := range p.mics {
						if m.UID == uid {
							name = m.Name
						}
					}
					p.change(func(s *config.Settings) { s.InputDevice, s.InputDeviceName = uid, name })
				}
			}
		})
		setting(c, "sounds", nil, T("sSounds"), T("sSoundsHint"), func() {
			on := s.InteractionSounds
			if ui.Switch(c.Key("sw"), &on).Label(T("sSounds")).Changed() {
				p.change(func(s *config.Settings) { s.InteractionSounds = on })
				if on {
					macos.PlayRecordingCue(true) // a sample of what it sounds like
				}
			}
		})
		setting(c, "mute", nil, T("sMuteOutput"), T("sMuteOutputHint"), func() {
			on := s.MuteOutput
			if ui.Switch(c.Key("sw"), &on).Label(T("sMuteOutput")).Changed() {
				p.change(func(s *config.Settings) { s.MuteOutput = on })
			}
		})
	})
}

// permissions shows the three permissions as macOS reports them, each with
// a way to grant it when it is missing.
func (p *prefsWindow) permissions(c *ui.Context) {
	c.After(time.Second)
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
	// A trigger key that could not be bound for want of a permission is
	// bound again as soon as macOS reports them granted.
	if granted := permissionsGranted(); granted != p.permsWere {
		p.permsWere = granted
		if granted && p.a.triggerErr != "" {
			p.a.bindTrigger()
			p.a.main.refresh()
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
			state(macos.AccessibilityTrusted(false), T("request"), func() { p.showPermissionDrag("Privacy_Accessibility") })
		})
		setting(c, "input", icKeyboard, T("pInput"), "", func() {
			state(macos.InputMonitoringGranted(false), T("openSettings"), func() { p.showPermissionDrag("Privacy_ListenEvent") })
		})
	})
}

// runningAppBundle offers only the actual packaged executable's app. A plain
// go run binary has no app to register in System Settings.
func runningAppBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(bundle) != ".app" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(contents, "Info.plist")); err != nil {
		return ""
	}
	return bundle
}

// showPermissionDrag shows the app drag card above other apps while the user
// grants Accessibility or Input Monitoring. It exports the actual file URL.
func (p *prefsWindow) showPermissionDrag(pane string) {
	macos.OpenPrivacySettings(pane)
	if p.bundlePath == "" {
		return
	}
	p.dragPane = pane
	if p.dragWin == nil {
		p.dragWin = mygo.NewFlyout(mygo.FlyoutOptions{
			Tray: p.a.tray, Width: 248, Height: 112, Hidden: true,
			Content: ui.View(p.permissionDragView),
		})
		p.dragWin.SetAlwaysOnTop(true)
		p.dragWin.OnClose(func(e *mygo.CloseEvent) {
			if !p.a.quitting {
				e.PreventDefault()
				p.dragWin.Hide()
			}
		})
	}
	area := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	p.dragWin.SetBounds(mygo.Rectangle{X: area.X + area.Width - 280, Y: area.Y + area.Height/2 - 56, Width: 248, Height: 112})
	p.dragWin.ShowInactive()
}

func (p *prefsWindow) permissionDragView(c *ui.Context) {
	useTheme(c)
	c.Root().Background(ui.Transparent)
	c.After(time.Second)
	granted := p.dragPane == "Privacy_Accessibility" && macos.AccessibilityTrusted(false) || p.dragPane == "Privacy_ListenEvent" && macos.InputMonitoringGranted(false)
	if granted {
		p.dragWin.Hide()
		p.a.refreshAll()
		return
	}
	data, err := transfer.FileData(p.bundlePath)
	if err != nil {
		return
	}
	ui.Column(c.Key("drag-app")).Size(248, 112).Padding(16).Gap(10).Radius(18).Background(cardColor(c.Theme())).Label(T("permDragApp")).
		DragData(data, transfer.DragOptions{Operations: transfer.Copy, Done: func(r transfer.Result) {
			if r.Err != nil {
				p.status = func() string { return T("permDragFailed") }
				p.refresh()
			}
		}}).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(44, 44).Draw(drawAppMark)
			ui.Text(c, "Voicemore").FontSize(17).FontWeight(600)
		})
		ui.Text(c, T("permDragHint")).FontSize(12).TextColor(c.Theme().TextMuted)
	})
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
			p.a.cleanupSwitch(c)
		})
		setting(c, "observe", nil, T("sObserve"), T("sObserveHint"), func() {
			on := s.ObserveEdits
			if ui.Switch(c.Key("sw"), &on).Label(T("sObserve")).Changed() {
				p.change(func(s *config.Settings) { s.ObserveEdits = on })
			}
		})
	})

	ui.Text(c, T("basicFormat")).FontSize(16).TextColor(t.Text)
	card(c, "basic-format", func() {
		for _, row := range []struct {
			key string
			on  bool
			set func(*config.Settings, bool)
		}{
			{"formatPeriod", s.TextStyle.TrimPeriod, func(s *config.Settings, v bool) { s.TextStyle.TrimPeriod = v }},
			{"formatCalm", s.TextStyle.CalmExclamations, func(s *config.Settings, v bool) { s.TextStyle.CalmExclamations = v }},
			{"formatFillers", s.TextStyle.RemoveFillers, func(s *config.Settings, v bool) { s.TextStyle.RemoveFillers = v }},
			{"formatAcronyms", s.TextStyle.JoinAcronyms, func(s *config.Settings, v bool) { s.TextStyle.JoinAcronyms = v }},
		} {
			setting(c, row.key, nil, T(row.key), T(row.key+"Hint"), func() {
				on := row.on
				if ui.Switch(c.Key("sw"), &on).Label(T(row.key)).Changed() {
					p.change(func(s *config.Settings) { row.set(s, on) })
				}
			})
		}
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
