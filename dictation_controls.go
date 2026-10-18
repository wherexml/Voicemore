package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"strings"
	"time"
	"voiceinput/internal/config"
	"voiceinput/internal/macos"
	"voiceinput/internal/session"
)

func (p *prefsWindow) shortcutControl(c *ui.Context, kind, value string, compact bool) {
	t := c.Theme()
	label := T("sKey")
	if kind == "translation" {
		label = T("translationKey")
	}
	text := keyName(value)
	if value == "" {
		text = T("recordShortcut")
	}
	active := p.shortcutKind == kind
	if active {
		text = p.shortcutPreview
		if text == "" {
			text = T("pressShortcut")
		}
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		b := ui.Button(c.Key("record"), text).Height(36).Label(label + ": " + text)
		if compact {
			b.Grow(1)
		} else {
			b.MinWidth(170)
		}
		if active {
			b.Border(1, t.Accent)
		}
		if b.Clicked() {
			if active {
				p.cancelShortcut()
			} else {
				if !p.visible() {
					p.open("dictation")
				}
				p.beginShortcut(kind)
			}
		}
		if kind == "translation" && value != "" {
			if ui.Button(c.Key("clear"), "×").Label(T("clearTranslationKey")).Clicked() {
				p.cancelShortcut()
				p.saveShortcut("translation", "")
			}
		}
	})
}
func (p *prefsWindow) beginShortcut(kind string) {
	v := p.a.ctrl.View()
	if p.a.updates.installing {
		p.say("updateBusy")
		return
	}
	if v.Phase != session.Idle && v.Phase != session.Done && v.Phase != session.Failed {
		p.say("shortcutBusy")
		return
	}
	p.cancelShortcut()
	p.status = nil
	p.shortcutKind = kind
	p.shortcutPreview = ""
	p.a.shortcutsSuspended = true
	p.a.bindTrigger()
	stop, err := macos.CaptureShortcut(p.win.NativeHandle(), func(value string) {
		p.shortcutPreview = keyName(value)
		if config.ValidateShortcut(value) != nil {
			p.shortcutPreview = T("shortcutUnsafe")
		}
		p.refresh()
	}, func(value string) {
		if p.stopShortcutCapture != nil {
			p.stopShortcutCapture()
			p.stopShortcutCapture = nil
		}
		p.shortcutKind = ""
		p.shortcutGuardUntil = time.Now().Add(250 * time.Millisecond)
		p.a.shortcutsSuspended = false
		if value != "" {
			p.saveShortcut(kind, value)
		} else {
			p.a.bindTrigger()
		}
		p.refresh()
	})
	if err != nil {
		p.shortcutKind = ""
		p.shortcutGuardUntil = time.Now().Add(250 * time.Millisecond)
		p.a.shortcutsSuspended = false
		p.a.bindTrigger()
		p.say("notSaved", err.Error())
	} else {
		p.stopShortcutCapture = stop
	}
	p.refresh()
}
func (p *prefsWindow) cancelShortcut() {
	if p.stopShortcutCapture == nil {
		return
	}
	p.stopShortcutCapture()
	p.stopShortcutCapture = nil
	p.shortcutKind = ""
	p.shortcutGuardUntil = time.Now().Add(250 * time.Millisecond)
	p.a.shortcutsSuspended = false
	p.a.bindTrigger()
	p.refresh()
}
func (p *prefsWindow) saveShortcut(kind, value string) {
	if err := config.ValidateShortcut(value); err != nil {
		p.say("shortcutUnsafe")
		p.a.bindTrigger()
		return
	}
	old := p.a.settings.Get()
	next := old
	if (kind == "dictation" && sameShortcut(value, old.TranslationKey) && value != "") || (kind == "translation" && sameShortcut(value, old.TriggerKey)) {
		p.say("shortcutTaken")
		p.a.bindTrigger()
		return
	}
	if kind == "translation" {
		next.TranslationKey = value
	} else {
		next.TriggerKey = value
	}
	if err := p.a.settings.Set(next); err != nil {
		_ = p.a.settings.Set(old)
		p.say("notSaved", err.Error())
		p.a.bindTrigger()
		return
	}
	p.a.bindTrigger()
	if p.a.triggerErr != "" {
		reason := p.a.triggerErr
		if err := p.a.settings.Set(old); err != nil {
			p.say("notSaved", err.Error())
		} else {
			p.status = func() string { return reason }
		}
		p.a.bindTrigger()
	} else {
		p.status = nil
	}
	p.fnConflict = macos.FnKeyConflict()
	p.a.refreshAll()
}

func connectedButtonDevice(saved config.ButtonDevice) (config.ButtonDevice, bool) {
	for _, actual := range macos.ButtonDevices() {
		if saved.Matches(actual) {
			return actual, true
		}
	}
	return saved, false
}
func deviceEventName(d config.ButtonDevice, page, usage uint32) string {
	if d.IsMouse() && page == 9 {
		if usage == 3 {
			return T("mouseMiddleButton")
		}
		return T("mouseSideButton", usage)
	}
	if d.VendorID == 11427 && d.ProductID == 16401 && page == 12 && usage == 233 {
		return T("djiConnectionKey")
	}
	return macos.DeviceButtonName(page, usage)
}
func deviceBindingProblem(d config.ButtonDevice, page, usage uint32) string {
	capability := macos.ButtonDeviceCapabilities(d)
	if !capability.CanBind {
		return capability.Reason
	}
	if !capability.Supports(page, usage) {
		return "deviceUnsupportedKey"
	}
	return ""
}
func buttonBindingName(b config.DeviceButtonBinding) string {
	return deviceEventName(b.Device, b.Page, b.Usage)
}

var icButtonReceiver = icon(`<rect x="4" y="1.5" width="8" height="13" rx="2"/><path d="M6.5 4h3M6.5 6h3M7 12h2"/><circle cx="8" cy="9" r="1.2"/>`)
var icButtonHeadphones = icon(`<path d="M2.5 9V7a5.5 5.5 0 0 1 11 0v2M2.5 8H4v5H2.5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1zM12 8h1.5a1 1 0 0 1 1 1v3a1 1 0 0 1-1 1H12z"/>`)
var icButtonMouse = icon(`<rect x="3.5" y="1" width="9" height="14" rx="4.5"/><path d="M8 1v5M3.5 6h9"/><path d="M8 3v1"/>`)

func buttonDeviceIcon(d config.ButtonDevice) *ui.SVG {
	if d.IsMouse() {
		return icButtonMouse
	}
	if strings.HasPrefix(d.ID, "audio-") {
		return icButtonHeadphones
	}
	if d.UsagePage == 1 {
		return icKeyboard
	}
	return icButtonReceiver
}
func deviceIconTile(c *ui.Context, d config.ButtonDevice, size float32) {
	t := c.Theme()
	ui.Row(c).Size(size, size).Radius(size/4).Background(t.Background).Border(1, settingsGroupLine(c)).Center().Children(func() { ui.Icon(c, buttonDeviceIcon(d)).Size(size*.55, size*.55).TextColor(t.TextMuted) })
}
func deviceButton(c *ui.Context, key, label string) ui.Element {
	return ui.Button(c.Key(key), label).Height(30).FontSize(12).Radius(8).Border(1, settingsGroupLine(c)).Background(c.Theme().Background)
}
func deviceRadio(c *ui.Context, on bool) {
	t := c.Theme()
	color := t.TextMuted
	if on {
		color = t.Accent
	}
	ui.Row(c).Size(14, 14).Radius(7).Border(1.5, color).Center().Children(func() {
		if on {
			ui.Box(c).Size(6, 6).Radius(3).Background(color)
		}
	})
}
func (p *prefsWindow) stopDeviceTest() {
	if !p.deviceTest {
		return
	}
	p.deviceTest = false
	p.a.bindTrigger()
}
func (p *prefsWindow) beginDeviceTest(b config.DeviceButtonBinding) {
	v := p.a.ctrl.View()
	if v.Phase != session.Idle && v.Phase != session.Done && v.Phase != session.Failed {
		p.say("shortcutBusy")
		return
	}
	p.cancelShortcut()
	p.stopDeviceTest()
	p.deviceSelected, _ = connectedButtonDevice(b.Device)
	p.deviceEditing = b.ID
	p.devicePage, p.deviceUsage = b.Page, b.Usage
	p.deviceTest = true
	p.deviceTestResult = "deviceTestWaiting"
	p.deviceDeadline = time.Now().Add(12 * time.Second)
	deadline := p.deviceDeadline
	time.AfterFunc(12*time.Second, func() {
		macos.OnMain(func() {
			if p.deviceTest && p.deviceDeadline.Equal(deadline) {
				p.deviceTestResult = "deviceTestTimedOut"
				p.stopDeviceTest()
				p.refresh()
			}
		})
	})
	p.a.bindTrigger()
}
func (p *prefsWindow) deviceButtons(c *ui.Context) {
	c.After(500 * time.Millisecond)
	t := c.Theme()
	bindings := p.a.settings.Get().AllDeviceButtons()
	if p.deviceTest && time.Now().After(p.deviceDeadline) {
		p.deviceTestResult = "deviceTestTimedOut"
		p.stopDeviceTest()
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, T("deviceButtons")).FontSize(13).TextColor(t.TextMuted).Grow(1)
		if deviceButton(c, "add-device", "＋ "+T("addDeviceButton")).Clicked() {
			p.openDeviceWizard(nil, 0)
		}
	})
	card(c, "device-buttons", func() {
		if len(bindings) == 0 {
			ui.Column(c).FillWidth().Height(168).Center().Gap(12).Children(func() {
				ui.Icon(c, icPlus).Size(24, 24).TextColor(t.TextMuted)
				ui.Text(c, T("noDeviceButtons")).FontSize(13).TextColor(t.TextMuted)
				if deviceButton(c, "add", T("addDeviceButton")).Clicked() {
					p.openDeviceWizard(nil, 0)
				}
			})
			return
		}
		for _, b := range bindings {
			actual, connected := connectedButtonDevice(b.Device)
			name := actual.Name
			supportProblem := deviceBindingProblem(actual, b.Page, b.Usage)
			if b.Device.ID == config.LegacyDJIButtonID {
				name = T("djiName")
			}
			ui.Column(c.Key("binding-"+b.ID)).FillWidth().Gap(8).Padding(14, 16).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					deviceIconTile(c, actual, 36)
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						ui.Text(c, name).FontSize(13).FontWeight(500)
						state, color := T("deviceDisconnected"), t.TextMuted
						if connected {
							state, color = T("deviceConnected"), t.Success
						}
						if !b.Enabled {
							state, color = T("deviceDisabled"), t.TextMuted
						}
						if connected && supportProblem != "" {
							state, color = T(supportProblem), t.Warning
						} else if b.Enabled && macos.ButtonDeviceError(b.Device) != "" {
							state, color = T("deviceClaimFailed"), t.Warning
							if macos.ButtonDeviceError(b.Device) == "deviceSourceUnavailable" {
								state = T("deviceSourceUnavailable")
							}
						}
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(6, 6).Radius(3).Background(color)
							ui.Text(c, state).FontSize(11).TextColor(color)
						})
					})
					on := b.Enabled
					if ui.Switch(c.Key("enabled"), &on).Label(T("deviceEnable") + ": " + name).Changed() {
						p.stopDeviceTest()
						b.Enabled = on
						p.change(func(s *config.Settings) { s.PutDeviceButton(b) })
						p.a.bindTrigger()
					}
					if ui.Button(c.Key("remove"), "×").Size(28, 28).Label(T("removeDeviceButton") + ": " + name).Clicked() {
						p.stopDeviceTest()
						p.deviceTestResult = ""
						p.change(func(s *config.Settings) { s.RemoveDeviceButton(b.ID) })
						p.a.bindTrigger()
					}
				})
				ui.Row(c).Padding(0, 0, 0, 48).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, buttonBindingName(b)).FontSize(11).Padding(3, 7).Radius(5).Border(1, settingsGroupLine(c)).Background(t.Background)
					ui.Text(c, "→").TextColor(t.TextMuted)
					action := "deviceDictationFull"
					if b.Action == "translation" {
						action = "deviceTranslationFull"
					}
					ui.Text(c, T(action)).FontSize(12).TextColor(t.TextMuted).Grow(1)
					label := T("deviceTestShort")
					testing := p.deviceTest && p.deviceEditing == b.ID
					if testing {
						label = T("deviceCancelTest")
					}
					if deviceButton(c, "test", label).Disabled(!connected || !b.Enabled || supportProblem != "").Clicked() {
						if testing {
							p.deviceTestResult = ""
							p.stopDeviceTest()
						} else {
							p.beginDeviceTest(b)
						}
					}
					if deviceButton(c, "edit", T("deviceEditShort")).Clicked() {
						p.openDeviceWizard(&b, 2)
					}
				})
			})
		}
	})
	if p.deviceTestResult != "" {
		label := T(p.deviceTestResult)
		color := t.TextMuted
		if p.deviceTestResult == "deviceTestWaiting" {
			label = fmt.Sprintf(T("deviceTestWaiting"), deviceEventName(p.deviceSelected, p.devicePage, p.deviceUsage))
		}
		if p.deviceTestResult == "deviceTestReceived" {
			label = fmt.Sprintf(T("deviceTestReceived"), deviceEventName(p.deviceSelected, p.devicePage, p.deviceUsage))
			color = t.Success
		}
		if p.deviceTest && macos.ButtonDeviceError(p.deviceSelected) != "" {
			label = T("deviceClaimFailed")
			color = t.Warning
		}
		ui.Text(c, label).FontSize(12).TextColor(color)
	}
}
func (p *prefsWindow) openDeviceWizard(binding *config.DeviceButtonBinding, step int) {
	v := p.a.ctrl.View()
	if v.Phase != session.Idle && v.Phase != session.Done && v.Phase != session.Failed {
		p.say("shortcutBusy")
		return
	}
	p.cancelShortcut()
	p.stopDeviceTest()
	p.deviceTestResult = ""
	p.status = nil
	p.deviceWizard = true
	p.deviceStep = step
	p.deviceCaptured = false
	p.deviceAction = "dictation"
	p.deviceSelected = config.ButtonDevice{}
	p.devicePage, p.deviceUsage = 0, 0
	p.deviceEditing = ""
	if binding != nil {
		p.deviceSelected, _ = connectedButtonDevice(binding.Device)
		p.deviceEditing = binding.ID
		p.deviceAction = binding.Action
		p.devicePage, p.deviceUsage = binding.Page, binding.Usage
		p.deviceCaptured = step == 2
	}
	p.deviceDeadline = time.Now().Add(12 * time.Second)
	p.a.bindTrigger()
}
func (p *prefsWindow) closeDeviceWizard() {
	if p.deviceWizard {
		p.deviceWizard = false
		p.deviceCaptured = false
		p.a.bindTrigger()
	}
}
func (p *prefsWindow) retryDeviceCapture() {
	p.deviceStep = 1
	p.deviceCaptured = false
	p.status = nil
	p.deviceDeadline = time.Now().Add(12 * time.Second)
	p.a.bindTrigger()
}
func (p *prefsWindow) saveDeviceBinding() {
	if reason := deviceBindingProblem(p.deviceSelected, p.devicePage, p.deviceUsage); reason != "" {
		p.say(reason)
		return
	}
	if _, connected := connectedButtonDevice(p.deviceSelected); !connected || macos.ButtonDeviceError(p.deviceSelected) != "" {
		p.say("deviceClaimFailed")
		return
	}
	old := p.a.settings.Get()
	next := old
	next.DeviceButtons = append([]config.DeviceButtonBinding(nil), old.DeviceButtons...)
	if next.DeviceButtonTaken(p.deviceSelected, p.devicePage, p.deviceUsage, p.deviceEditing) {
		p.say("deviceButtonTaken")
		return
	}
	enabled := true
	for _, b := range old.AllDeviceButtons() {
		if b.ID == p.deviceEditing {
			enabled = b.Enabled
		}
	}
	id := p.deviceEditing
	if id == "" {
		id = fmt.Sprintf("button-%d", time.Now().UnixNano())
	}
	next.PutDeviceButton(config.DeviceButtonBinding{ID: id, Device: p.deviceSelected, Page: p.devicePage, Usage: p.deviceUsage, Action: p.deviceAction, Enabled: enabled})
	if err := p.a.settings.Set(next); err != nil {
		_ = p.a.settings.Set(old)
		p.say("notSaved", err.Error())
		return
	}
	p.deviceWizard = false
	p.status = nil
	p.a.refreshAll()
}
func (p *prefsWindow) deviceButtonWizard(c *ui.Context) {
	was := p.deviceWizard
	ui.DialogBase(c, &p.deviceWizard, func(back, panel ui.Element) {
		t := c.Theme()
		back.Background(ui.RGBA(0, 0, 0, .32))
		panel.Width(490).MaxWidth(700).Radius(16).Background(t.Background).Border(1, settingsGroupLine(c)).Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, .3)).Gap(0)
		ui.Row(c).Padding(20, 24, 12, 24).AlignItems(ui.Center).Children(func() {
			title := T("addDeviceButton")
			if p.deviceEditing != "" {
				title = T("editDeviceButton")
			}
			ui.Text(c, title).FontSize(16).FontWeight(600).Grow(1)
			if ui.Button(c.Key("close"), "×").Size(28, 28).Label(T("close")).Clicked() {
				p.deviceWizard = false
			}
		})
		ui.Row(c).Padding(8, 24, 22, 24).AlignItems(ui.Center).Children(func() {
			for i, key := range []string{"deviceStepSelect", "deviceStepCapture", "deviceStepAction"} {
				if i > 0 {
					ui.Text(c, "—").FontSize(12).TextColor(t.TextMuted).Grow(1).TextAlign(ui.Center)
				}
				color, bg, fg := t.TextMuted, settingsGroupColor(t), t.TextMuted
				if i == p.deviceStep {
					color, bg, fg = t.Text, t.Accent, t.AccentText
				} else if i < p.deviceStep {
					fg = t.Success
				}
				ui.Row(c.Key(key)).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Size(21, 21).Radius(11).Background(bg).Center().Children(func() {
						if i < p.deviceStep {
							ui.Icon(c, icCheck).Size(12, 12).TextColor(fg)
						} else {
							ui.Text(c, fmt.Sprint(i+1)).FontSize(11).TextColor(fg)
						}
					})
					ui.Text(c, T(key)).FontSize(12).TextColor(color)
				})
			}
		})
		_, connected := connectedButtonDevice(p.deviceSelected)
		problem := macos.ButtonDeviceError(p.deviceSelected)
		if p.a.deviceButtonErr != "" {
			problem = p.a.deviceButtonErr
		}
		capability := macos.ButtonDeviceCapabilities(p.deviceSelected)
		ready := connected && capability.CanBind && problem == ""
		taken := p.deviceCaptured && p.a.settings.Get().DeviceButtonTaken(p.deviceSelected, p.devicePage, p.deviceUsage, p.deviceEditing)
		if !ready {
			p.deviceCaptured = false
		}
		ui.Scroll(c.Key("wizard-body")).Height(320).Children(func() {
			ui.Column(c).FillWidth().MinHeight(296).Padding(0, 24, 24, 24).Gap(10).Children(func() {
				switch p.deviceStep {
				case 0:
					options := macos.ConnectedButtonDevices()
					if len(options) == 0 {
						ui.Text(c, T("noConnectedButtonDevices")).TextColor(t.TextMuted)
					}
					for _, opt := range options {
						d := opt.Device
						selected := d.ID == p.deviceSelected.ID || d.SamePhysicalDevice(p.deviceSelected)
						border := settingsGroupLine(c)
						if selected {
							border = t.Accent
						}
						b := ui.RadioBase(c.Key(d.ID), &p.deviceSelected, d).FillWidth().Padding(12).Gap(12).Radius(10).Background(settingsGroupColor(t)).Border(1, border).Disabled(!opt.CanBind).Label(d.Name)
						b.Children(func() {
							deviceIconTile(c, d, 44)
							ui.Column(c).Grow(1).Gap(5).Children(func() {
								ui.Text(c, d.Name).FontSize(13).FontWeight(500)
								ui.Text(c, T(opt.Kind)).FontSize(11).TextColor(t.TextMuted)
							})
							if !opt.CanBind {
								ui.Text(c, T(opt.Reason)).FontSize(11).TextColor(t.TextMuted)
							} else {
								ui.Box(c).Size(6, 6).Radius(3).Background(t.Success).Label(T("deviceConnected"))
							}
							deviceRadio(c, selected)
						})
						if b.Changed() {
							p.deviceSelected = d
							p.status = nil
						}
					}
				case 1:
					label, status, color := T("pressDeviceKey"), T("deviceWaiting"), t.TextMuted
					if p.deviceCaptured {
						label = T("deviceKeyReceived") + ": " + deviceEventName(p.deviceSelected, p.devicePage, p.deviceUsage)
						status = T("deviceSinglePress")
						color = t.Success
						if taken {
							label = T("deviceButtonTaken")
							status = deviceEventName(p.deviceSelected, p.devicePage, p.deviceUsage)
							color = t.Warning
						}
					} else if !connected {
						label = T("deviceDisconnected")
						status = ""
					} else if !capability.CanBind {
						label = T(capability.Reason)
						status = ""
					} else if problem != "" {
						label = T("deviceClaimFailed")
						if problem == "deviceSourceUnavailable" {
							label = T(problem)
						}
						status = ""
					} else if time.Now().After(p.deviceDeadline) {
						label = T("deviceKeyTimedOut")
						status = ""
					}
					ui.Column(c).FillWidth().Padding(15, 0).AlignItems(ui.Center).Gap(16).Children(func() {
						ui.Row(c).Size(64, 64).Radius(32).Background(settingsGroupColor(t)).Center().Children(func() {
							ic := buttonDeviceIcon(p.deviceSelected)
							if p.deviceCaptured && !taken {
								ic = icCheck
							}
							ui.Icon(c, ic).Size(30, 30).TextColor(color)
						})
						ui.Column(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, label).FontSize(17).FontWeight(500)
							ui.Text(c, p.deviceSelected.Name).FontSize(12).TextColor(t.TextMuted)
						})
						ui.Text(c, status).FontSize(12).TextColor(color).MinHeight(20)
						if p.deviceCaptured || problem != "" || time.Now().After(p.deviceDeadline) {
							if deviceButton(c, "retry", T("relearnDeviceButton")).Clicked() {
								p.retryDeviceCapture()
							}
						}
					})
				case 2:
					ui.Row(c).Gap(12).Padding(0, 0, 10, 0).AlignItems(ui.Center).Children(func() {
						deviceIconTile(c, p.deviceSelected, 44)
						ui.Column(c).Grow(1).Gap(5).Children(func() {
							ui.Text(c, p.deviceSelected.Name).FontSize(13).FontWeight(500)
							ui.Text(c, deviceEventName(p.deviceSelected, p.devicePage, p.deviceUsage)).FontSize(11).TextColor(t.TextMuted)
						})
						if ui.Button(c.Key("relearn"), T("relearnDeviceButton")).FontSize(12).Clicked() {
							p.retryDeviceCapture()
						}
					})
					for _, action := range []string{"dictation", "translation"} {
						key := "deviceDictationFull"
						if action == "translation" {
							key = "deviceTranslationFull"
						}
						selected := p.deviceAction == action
						border := settingsGroupLine(c)
						if selected {
							border = t.Accent
						}
						b := ui.RadioBase(c.Key(action), &p.deviceAction, action).FillWidth().Padding(16).Gap(12).Radius(10).Background(settingsGroupColor(t)).Border(1, border).Label(T(key))
						b.Children(func() {
							ui.Column(c).Grow(1).Gap(5).Children(func() {
								ui.Text(c, T(key)).FontSize(13).FontWeight(500)
								ui.Text(c, T("deviceToggleHint")).FontSize(11).TextColor(t.TextMuted)
							})
							deviceRadio(c, selected)
						})
					}
				}
				if p.status != nil {
					ui.Text(c, p.status()).FontSize(12).TextColor(t.Warning)
				}
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(settingsGroupLine(c))
		ui.Row(c).FillWidth().Padding(16, 24).Gap(8).Children(func() {
			label := T("cancel")
			if p.deviceStep > 0 {
				label = T("obBack")
			}
			if deviceButton(c, "back", label).Clicked() {
				if p.deviceStep == 0 {
					p.deviceWizard = false
				} else {
					p.deviceStep--
					p.status = nil
					p.deviceCaptured = false
					p.deviceDeadline = time.Now().Add(12 * time.Second)
					p.a.bindTrigger()
				}
			}
			ui.Spacer(c)
			label = T("deviceNext")
			if p.deviceStep == 2 {
				label = T("saveDeviceBinding")
			}
			disabled := !ready || (p.deviceStep > 0 && (!p.deviceCaptured || taken))
			if p.deviceStep == 0 {
				disabled = p.deviceSelected.ID == "" || !connected || !capability.CanBind
			}
			if ui.PrimaryButton(c.Key("next"), label).Height(30).FontSize(12).Radius(8).Disabled(disabled).Clicked() {
				if p.deviceStep == 2 {
					p.saveDeviceBinding()
				} else {
					p.deviceStep++
					p.deviceDeadline = time.Now().Add(12 * time.Second)
					p.a.bindTrigger()
				}
			}
		})
	})
	if was && !p.deviceWizard {
		p.deviceCaptured = false
		p.a.bindTrigger()
	}
	if p.deviceWizard {
		c.After(500 * time.Millisecond)
	}
}

func init() {
	for key, value := range map[string][3]string{
		"externalMouse": {"Mouse", "滑鼠", "鼠标"}, "mouseMiddleButton": {"Middle button", "滑鼠中鍵", "鼠标中键"}, "mouseSideButton": {"Mouse button %d", "滑鼠按鍵 %d", "鼠标按键 %d"},
		"usbController": {"USB controller", "USB 控制器", "USB 控制器"}, "wirelessController": {"Wireless controller", "無線控制器", "无线控制器"}, "wirelessReceiver": {"Wireless receiver", "無線接收器", "无线接收器"}, "externalKeyboard": {"External keyboard", "外接鍵盤", "外接键盘"}, "bluetoothHeadphones": {"Bluetooth audio", "藍牙音訊", "蓝牙音频"},
		"deviceIdentityAmbiguous":    {"Device identity is ambiguous", "無法區分相同設備", "无法区分相同设备"},
		"deviceSourceUnavailable":    {"No interceptable event from macOS", "系統未提供可攔截事件", "系统未提供可拦截事件"},
		"deviceSelectiveUnsupported": {"Selective interception unavailable", "暫不支援逐鍵接管", "暂不支持逐键接管"},
		"deviceNoAuxiliaryButtons":   {"No bindable auxiliary buttons", "未提供可綁定的輔助按鍵", "未提供可绑定的辅助按键"},
		"deviceUnsupportedKey":       {"This key cannot be intercepted", "此按鍵暫不支援接管", "此按键暂不支持接管"},
		"deviceAudioOnly":            {"Button binding unavailable", "暫不支援按鍵綁定", "暂不支持按键绑定"}, "deviceAlreadyAdded": {"Added", "已添加", "已添加"}, "deviceNext": {"Next", "下一步", "下一步"},
		"deviceWaiting": {"Waiting for button…", "等待按鍵…", "等待按键…"}, "deviceSinglePress": {"Single press supported", "支援單擊", "支持单击"},
		"deviceDictationFull": {"Start / stop dictation", "開始／結束聽寫", "开始／结束听写"}, "deviceTranslationFull": {"Start / stop translation", "開始／結束翻譯", "开始／结束翻译"}, "deviceToggleHint": {"Press to start, press again to stop", "按下開始，再按結束", "按下开始，再按结束"},
		"deviceTestShort": {"Test", "測試", "测试"}, "deviceEditShort": {"Edit", "修改", "修改"}, "deviceCancelTest": {"Cancel test", "取消測試", "取消测试"},
		"deviceTestWaiting": {"Press %s", "請按下 %s", "请按下 %s"}, "deviceTestReceived": {"✓ Detected %s · 1", "✓ 已收到 %s · 1 次", "✓ 已收到 %s · 1 次"}, "deviceTestTimedOut": {"No button received · Retry", "未收到按鍵 · 請重試", "未收到按键 · 请重试"},

		"translationKey": {"Translation shortcut", "翻譯快捷鍵", "翻译快捷键"}, "translationLanguage": {"Target language", "目標語言", "目标语言"},
		"recordShortcut": {"Record shortcut", "錄入快捷鍵", "录入快捷键"}, "pressShortcut": {"Press shortcut…", "請按快捷鍵…", "请按快捷键…"}, "clearTranslationKey": {"Clear translation shortcut", "清除翻譯快捷鍵", "清除翻译快捷键"},
		"shortcutTaken": {"Shortcut already in use", "快捷鍵已被佔用", "快捷键已被占用"}, "shortcutBusy": {"Finish dictation before recording a shortcut", "請先結束聽寫再錄入快捷鍵", "请先结束听写再录入快捷键"},
		"deviceEnable": {"Enable", "啟用", "启用"}, "deviceDisabled": {"Disabled", "已停用", "已停用"},
		"editDeviceButton": {"Edit device button", "修改裝置按鍵", "修改设备按键"}, "relearnDeviceButton": {"Detect again", "重新識別", "重新识别"},
		"noConnectedButtonDevices": {"No connected button devices", "沒有已連接的按鍵裝置", "没有已连接的按键设备"},
		"pressDeviceKey":           {"Press the button you want to use", "按一下你要使用的按鍵", "按一下你要使用的按键"},
		"deviceKeyReceived":        {"Button detected", "已識別按鍵", "已识别按键"}, "deviceKeyTimedOut": {"No button received", "未收到按鍵", "未收到按键"},
		"saveDeviceBinding": {"Save binding", "儲存綁定", "保存绑定"}, "deviceButtonTaken": {"Button already bound", "此按鍵已綁定", "此按键已绑定"},
		"deviceButtons": {"External device buttons", "外接裝置按鍵", "外接设备按键"}, "addDeviceButton": {"Add device button", "添加裝置按鍵", "添加设备按键"}, "noDeviceButtons": {"No device buttons", "還沒有裝置按鍵", "还没有设备按键"},
		"djiName": {"DJI wireless microphone", "DJI 無線麥克風", "DJI 无线麦克风"}, "deviceConnected": {"Connected", "已連接", "已连接"}, "deviceDisconnected": {"Disconnected", "未連接", "未连接"}, "deviceClaimFailed": {"Could not claim device", "接管失敗", "接管失败"},
		"djiConnectionKey": {"Connection button", "連接鍵", "连接键"}, "deviceAction": {"Button action", "按鍵動作", "按键动作"}, "deviceDictation": {"Toggle dictation", "切換聽寫", "切换听写"}, "deviceTranslation": {"Toggle translation", "切換翻譯", "切换翻译"},
		"testDeviceButton": {"Test button", "測試按鍵", "测试按键"}, "removeDeviceButton": {"Remove", "移除", "移除"}, "deviceStepSelect": {"Choose device", "選擇裝置", "选择设备"}, "deviceStepCapture": {"Detect button", "識別按鍵", "识别按键"}, "deviceStepAction": {"Bind action", "綁定動作", "绑定动作"},
		"pressDeviceButton": {"Press the connection button", "請按下連接鍵", "请按下连接键"}, "deviceButtonReceived": {"Connection button detected", "已收到連接鍵", "已收到连接键"}, "retryTranslation": {"Retry translation", "重試翻譯", "重试翻译"},
		"retryDevice":        {"Retry", "重試", "重试"},
		"translationWorking": {"Translating", "正在翻譯", "正在翻译"},
	} {
		texts[key] = value
	}
}

// Match persisted accelerator aliases as well as newly captured key names.
func sameShortcut(a, b string) bool {
	normalize := func(value string) string {
		if identity := macos.NativeShortcutIdentity(value); identity != "" {
			return identity
		}
		parts := strings.Split(value, "+")
		if len(parts) == 0 {
			return ""
		}
		mods := map[string]bool{}
		for _, part := range parts[:len(parts)-1] {
			switch strings.ToLower(strings.TrimSpace(part)) {
			case "cmd", "command", "super", "meta", "cmdorctrl", "commandorcontrol":
				mods["cmd"] = true
			case "ctrl", "control":
				mods["ctrl"] = true
			case "alt", "option", "altgr":
				mods["alt"] = true
			case "shift":
				mods["shift"] = true
			}
		}
		out := ""
		for _, key := range []string{"cmd", "ctrl", "alt", "shift"} {
			if mods[key] {
				out += key + "+"
			}
		}
		return out + strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	}
	return normalize(a) == normalize(b)
}
