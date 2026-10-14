package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/config"
	"voiceinput/internal/localasr"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/session"
)

// onboardWindow is the first-run guide: a window of its own, about
// 820×600, laid out as a setup wizard as mock/claude.html has it. A slim
// title bar with the brand, a four-step progress bar, a padded content
// region that starts at its top, and a footer with Back on the left and
// Later and the one primary action on the right. While it shows, the main
// window is hidden.
//
// Everything in it is real. A step is complete when the app is actually
// ready for it, never because Continue was pressed:
//
//	how it works      it has been read (left once)
//	speech model      the default speech model can serve: the on-device
//	                  model is deployed and ready, or a cloud provider is
//	                  set up and chosen. One or the other; text cleanup is
//	                  not asked for.
//	permissions       macOS reports microphone, accessibility and input
//	                  monitoring as granted
//	first dictation   a practice run started here delivered a transcript
//	                  that is not empty to the guide's own field
//
// Progress is saved in the settings (config.Onboarding), so the guide
// resumes where it was. Settings saved by a version older than the guide
// are those of an existing user, who is not shown it at launch; it stays
// available from Settings → General.
type onboardWindow struct {
	a   *app
	win *mygo.Window

	// review shows the steps, even once all are complete. The guide opens
	// on them unless it is already complete, and completing the last step
	// never leaves them by itself: only Finish or Enter Voicemore does,
	// so the practice transcript stays to read, edit and try again.
	review      bool
	practice    string // the text of the practice field
	practiceRun int    // counts practice runs and their discards: a sink of an earlier count is stale
	permsWere   bool   // the permissions were all granted when last looked at
}

// The steps, by the text key of their name; the heading's key adds "h".
var obSteps = []string{"obS1", "obS2", "obS3", "obS4"}

func newOnboardWindow(a *app) *onboardWindow {
	o := &onboardWindow{a: a}
	o.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           T("obTitle"),
		Width:           820,
		Height:          600,
		MinWidth:        640,
		MinHeight:       480,
		Hidden:          true,
		StateKey:        "onboarding",
		TitleBarStyle:   mygo.TitleBarHidden,
		DisableMinimize: true,
		Content:         ui.View(o.view),
	})
	// Closing it is "Later": into the normal window.
	o.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			o.leave()
		}
	})
	return o
}

func (o *onboardWindow) visible() bool { return o != nil && o.win != nil && o.win.IsVisible() }

// refresh draws the window again, from any goroutine.
func (o *onboardWindow) refresh() {
	if o != nil && o.win != nil {
		o.win.Invalidate()
	}
}

// change edits the saved progress of the guide.
func (o *onboardWindow) change(fn func(ob *config.Onboarding)) {
	s := o.a.settings.Get()
	fn(&s.Onboarding)
	if err := o.a.settings.Set(s); err != nil {
		o.a.main.say("notSaved", err.Error())
	}
}

// open shows the guide at its first incomplete step, in place of the main
// window.
func (o *onboardWindow) open() {
	o.discardPractice()
	s := o.a.settings.Get()
	next := -1
	for i := range obSteps {
		if !o.done(i, s) {
			next = i
			break
		}
	}
	// A complete guide reopens on its summary; any other stays on the
	// steps until the user leaves.
	o.review = next >= 0
	o.change(func(ob *config.Onboarding) {
		ob.Left = false
		if next >= 0 {
			ob.Step = next
		}
	})
	o.win.SetTitle(T("obTitle"))
	o.a.main.stopPlayer()
	o.a.main.win.Hide()
	o.win.Show()
	o.win.Focus()
}

// leave is "Later", "Finish" and "Enter Voicemore": the guide is put away,
// not shown at the next launch, and the main window comes up as it is.
func (o *onboardWindow) leave() {
	o.review = false
	o.discardPractice() // a practice run does not outlive the guide
	o.change(func(ob *config.Onboarding) { ob.Left = true })
	o.win.Hide()
	o.a.main.reveal()
}

// goStep moves to a step. Leaving the introduction is what marks it read.
func (o *onboardWindow) goStep(i int) {
	i = min(max(i, 0), len(obSteps)-1)
	o.discardPractice() // a practice run does not outlive its step
	o.review = true     // explicit step navigation also leaves the completion summary
	o.change(func(ob *config.Onboarding) {
		if ob.Step == 0 && i != 0 {
			ob.Seen = true
		}
		ob.Step = i
	})
}

// speechReady says the default speech model can serve now. The on-device
// model counts when the app manages it and it is ready; a cloud or custom
// provider counts when it is set up and chosen.
func (o *onboardWindow) speechReady(s config.Settings) bool {
	if providers.Check(providers.KindASR, s, o.a.secret) != nil {
		return false
	}
	if s.ASRAssign.Provider == providers.Local {
		return s.ASRManaged && o.a.localASR.Snapshot().Phase == "ready"
	}
	return true
}

// permissionsGranted says macOS reports all three permissions as granted.
func permissionsGranted() bool {
	return macos.MicrophoneStatus() == "authorized" && macos.AccessibilityTrusted(false) && macos.InputMonitoringGranted(false)
}

// done says a step is complete, from the state of the app.
func (o *onboardWindow) done(i int, s config.Settings) bool {
	switch i {
	case 0:
		return s.Onboarding.Seen
	case 1:
		return o.speechReady(s)
	case 2:
		return permissionsGranted()
	}
	return s.Onboarding.Practiced
}

func (o *onboardWindow) view(c *ui.Context) {
	t := useTheme(c)
	bar := c.TitleBar()
	s := o.a.settings.Get()
	step := min(max(s.Onboarding.Step, 0), len(obSteps)-1)
	var done [4]bool
	count := 0
	for i := range obSteps {
		done[i] = o.done(i, s)
		if done[i] {
			count++
		}
	}
	all := count == len(obSteps)
	fin := all && !o.review
	// A step can be entered once the speech model and the permissions
	// before it are actually ready. "Later" is the way out without them.
	canEnter := func(i int) bool {
		for j := 1; j <= 2 && j < i; j++ {
			if !done[j] {
				return false
			}
		}
		return true
	}
	// A trigger key that could not be bound for want of a permission is
	// bound again as soon as macOS reports them granted.
	if granted := done[2]; granted != o.permsWere {
		o.permsWere = granted
		if granted && o.a.triggerErr != "" {
			o.a.bindTrigger()
			o.a.main.refresh()
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyW) {
		o.leave()
	}
	ui.Column(c).Fill().Children(func() {
		// The title bar: the brand, centered; the window controls are macOS's.
		ui.Row(c.Key("title")).Height(max(bar.Height, 40)).FillWidth().Gap(7).Justify(ui.Center).AlignItems(ui.Center).
			Background(fillColor(t)).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).DragWindow().Children(func() {
			ui.Box(c).Size(16, 16).Draw(drawAppMark)
			ui.Text(c, "Voicemore").FontSize(12.5).FontWeight(500).SingleLine()
			ui.Text(c, "·").FontSize(12.5).TextColor(t.TextMuted)
			ui.Text(c, T("obTitle")).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
		})

		// The progress bar: four steps joined by a line that fills as they complete.
		ui.Row(c.Key("steps")).Height(60).Padding(0, 40).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			for i, name := range obSteps {
				o.stepButton(c, i, T(name), done[i], !fin && i == step, true)
				if i < len(obSteps)-1 {
					line := t.Border
					if done[i] {
						line = t.Success
					}
					ui.Box(c).Grow(1).MinWidth(14).Height(2).Radius(1).Background(line)
				}
			}
		})

		// The content: one padded region, starting at its top.
		ui.Scroll(c.Key(fmt.Sprintf("body-%d-%t", step, fin))).Grow(1).Padding(30, 48, 24).Children(func() {
			if fin {
				o.finished(c, s)
				return
			}
			if step == len(obSteps)-1 {
				// The practice surface is the subject here: a quieter title.
				ui.Text(c, T(obSteps[step]+"h")).FontSize(20).FontWeight(600)
				ui.Box(c).Height(16)
			} else {
				ui.Text(c, T(obSteps[step]+"h")).FontSize(26).FontWeight(600)
				ui.Box(c).Height(22)
			}
			switch step {
			case 0:
				o.intro(c, s)
			case 1:
				o.speech(c, s)
			case 2:
				o.permissions(c)
			default:
				o.firstDictation(c, s, done[1] && done[2])
			}
		})

		// The footer keeps its places in every step: Back, then Later, then
		// the primary action.
		ui.Row(c.Key("foot")).Height(60).Padding(0, 24).Gap(10).AlignItems(ui.Center).
			Background(fillColor(t)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			if fin {
				if ui.Button(c.Key("review"), T("obReview")).Clicked() {
					o.review = true
				}
				ui.Spacer(c)
				if ui.PrimaryButton(c.Key("enter"), T("obEnter")).Clicked() {
					o.leave()
				}
				return
			}
			if ui.Button(c.Key("back"), T("obBack")).Disabled(step == 0).Clicked() {
				o.goStep(step - 1)
			}
			ui.Spacer(c)
			if step != 0 && !done[step] {
				hint := "obNotYet"
				if step < len(obSteps)-1 {
					hint = "obNeedStep" // Continue waits for the step; Later does not
				}
				ui.Text(c, T(hint)).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
			}
			later := "obLater"
			if all {
				later = "obEnter"
			}
			if ui.Button(c.Key("later"), T(later)).Clicked() {
				o.leave()
			}
			if step == len(obSteps)-1 {
				if ui.PrimaryButton(c.Key("finish"), T("obFinish")).Disabled(!all).Clicked() {
					o.leave()
				}
			} else if ui.PrimaryButton(c.Key("next"), T("obContinue")).Disabled(!canEnter(step + 1)).Clicked() {
				o.goStep(step + 1)
			}
		})
	})
}

// stepButton is a step of the progress bar: a numbered dot, or a check
// once it is complete, and its name. It goes to its step.
func (o *onboardWindow) stepButton(c *ui.Context, i int, name string, done, current, enabled bool) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("step-"+itoa(i))).Gap(9).Padding(6, 8).Radius(8).AlignItems(ui.Center).Label(name)
	if enabled && b.Hovered() {
		b.Background(hoverColor(t))
	}
	b.Children(func() {
		dot := ui.Row(c).Size(24, 24).Radius(12).Justify(ui.Center).AlignItems(ui.Center)
		switch {
		case done:
			dot.Background(okTint(t))
		case current:
			dot.Background(t.Accent)
		default:
			dot.Border(1, strongLine(t))
		}
		dot.Children(func() {
			switch {
			case done:
				ui.Icon(c, icCheck).FontSize(13).TextColor(t.Success)
			case current:
				ui.Text(c, itoa(i+1)).FontSize(12).FontWeight(500).TextColor(t.AccentText)
			default:
				ui.Text(c, itoa(i+1)).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
			}
		})
		col, weight := t.TextMuted, 400
		if done {
			col = softText(t)
		}
		if current {
			col, weight = t.Text, 600
		}
		ui.Text(c, name).FontSize(13).FontWeight(weight).TextColor(col).SingleLine()
	})
	if b.Clicked() && enabled { // a step behind an unfinished requirement is not entered from here
		o.goStep(i)
	}
}

// intro is the first step: press the key, speak, the text appears.
func (o *onboardWindow) intro(c *ui.Context, s config.Settings) {
	t := c.Theme()
	key := keyName(s.TriggerKey)
	first, third := T("obI1text"), T("obI3text", key)
	if s.TriggerMode == config.TriggerHold {
		first, third = T("obI1holdText"), T("obI3holdText")
	}
	item := func(k string, n int, visual func(), title, text string) {
		ui.Column(c.Key(k)).Grow(1).Basis(0).MinWidth(0).Children(func() {
			ui.Row(c).Height(52).AlignItems(ui.Center).Children(visual)
			ui.Box(c).Height(14)
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Text(c, itoa(n)).FontSize(11).FontWeight(500).TextColor(t.TextMuted)
				ui.Text(c, title).FontSize(15).FontWeight(600)
			})
			ui.Box(c).Height(5)
			ui.Text(c, text).FontSize(14).LineHeight(1.55).TextColor(softText(t))
		})
	}
	ui.Row(c).Gap(32).AlignItems(ui.Start).Children(func() {
		item("press", 1, func() { keycap(c, key, 38, 13) }, T("obI1", key), first)
		item("speak", 2, func() { ui.Box(c).Size(146, 34).Draw(drawPillPreview) }, T("obI2"), T("obI2text"))
		item("text", 3, func() { ui.Box(c).Size(96, 20).Draw(drawCaretLine) }, T("obI3"), third)
	})
}

// drawCaretLine pictures inserted text: three words and a caret after them.
func drawCaretLine(p *ui.Painter, r ui.Rect) {
	grey := ui.RGBA(128, 127, 120, 0.45)
	x, cy := r.X, r.Y+r.H/2
	for _, w := range []float32{26, 38, 16} {
		p.Fill(ui.Rect{X: x, Y: cy - 3, W: w, H: 6}, grey, 3)
		x += w + 4
	}
	p.Fill(ui.Rect{X: x, Y: cy - 9, W: 1.5, H: 18}, ui.RGB(50, 102, 173), 0.75)
}

// choice is one of the two options of the speech step, drawn as a card
// with a radio mark. It reports a press that changes the choice.
func (o *onboardWindow) choice(c *ui.Context, key, title, text string, on bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Padding(14, 16).Gap(12).Radius(12).Justify(ui.Start).AlignItems(ui.Start).Label(title)
	border := t.Border
	if on {
		border = strongLine(t)
		b.Background(hoverColor(t))
	} else if b.Hovered() {
		border = strongLine(t)
	}
	b.Border(1, border)
	b.Children(func() {
		radio := ui.Row(c).Size(16, 16).Radius(8).Justify(ui.Center).AlignItems(ui.Center).Border(1, strongLine(t))
		radio.Children(func() {
			if on {
				ui.Box(c).Size(8, 8).Radius(4).Background(t.Text)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(14.5).FontWeight(500)
			ui.Text(c, text).FontSize(13).TextColor(t.TextMuted)
		})
	})
	return b.Clicked() && !on
}

// activateLocal makes the app-managed model the speech default, as its
// editor in Settings → Models does before deploying or starting it.
func (o *onboardWindow) activateLocal() {
	s := o.a.settings.Get()
	s.ASRManaged = true
	s.ASRURL, s.ASRModel = localasr.URL, localasr.Model
	s.ASRAssign = config.Assignment{Provider: providers.Local}
	if err := o.a.settings.Set(s); err != nil {
		o.a.main.say("notSaved", err.Error())
	}
	o.a.refreshAll()
}

// speech is the second step: the on-device model, or a cloud provider.
// Only one is needed. The actions are the real ones of Settings → Models.
func (o *onboardWindow) speech(c *ui.Context, s config.Settings) {
	t := c.Theme()
	cloud := s.Onboarding.Cloud
	ui.Row(c.Key("choices")).Gap(10).AlignItems(ui.Stretch).Children(func() {
		if o.choice(c, "local", T("obLocal"), T("obLocalText"), !cloud) {
			o.change(func(ob *config.Onboarding) { ob.Cloud = false })
		}
		if o.choice(c, "cloud", T("obCloud"), T("obCloudText"), cloud) {
			o.change(func(ob *config.Onboarding) { ob.Cloud = true })
		}
	})
	ui.Box(c).Height(20)

	a := s.ASRAssign
	if cloud {
		// Nothing is said here until there is something to report: a cloud
		// default that serves, or one that is chosen and cannot.
		if a.Provider != "" && a.Provider != providers.Local {
			if o.speechReady(s) {
				ui.Row(c.Key("cloud-ok")).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icCheck).FontSize(13).TextColor(t.Success)
					ui.Text(c, providerName(a.Provider, s, providers.KindASR)+" · "+o.a.modelLabel(providers.KindASR, s)).FontSize(13.5).TextColor(t.Success)
				})
			} else {
				ui.Text(c, o.a.modelLabel(providers.KindASR, s)).FontSize(13.5).TextColor(t.Danger)
			}
			ui.Box(c).Height(12)
		}
		ui.Row(c.Key("cloud-actions")).Children(func() {
			if ui.PrimaryButton(c.Key("providers"), T("obCloudOpen")).Clicked() {
				o.a.prefs.open("models")
				o.a.prefs.guideReturn = 1
			}
		})
		return
	}

	st := o.a.localASR.Snapshot()
	if st.Busy() {
		c.After(500 * time.Millisecond) // the progress moves
	}
	needs := !st.Installed || st.Phase == "error"
	ui.Row(c.Key("local-status")).Gap(8).AlignItems(ui.Center).Children(func() {
		col := t.TextMuted
		switch {
		case st.Busy():
			ui.Spinner(c)
		case st.Phase == "ready":
			col = t.Success
			ui.Icon(c, icCheck).FontSize(13).TextColor(t.Success)
		case st.Phase == "error":
			col = t.Danger
		}
		ui.Text(c, localasr.Model+" · "+localASRState(st)).FontSize(13.5).TextColor(col).SingleLine()
		if st.Phase == "installing" && st.Total > 0 {
			ui.Text(c, fmt.Sprintf("%.0f%% · %.2f / %.2f GB", float64(st.Done)*100/float64(st.Total), float64(st.Done)/1e9, float64(st.Total)/1e9)).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
		}
	})
	if st.Phase == "installing" && st.Total > 0 {
		ui.Box(c).Height(10)
		ui.Progress(c, min(1.0, float64(st.Done)/float64(st.Total)))
	}
	if st.Phase == "error" && st.Detail != "" {
		ui.Box(c).Height(8)
		ui.Text(c, st.Detail).FontSize(13).TextColor(t.Danger).Selectable()
	}
	ui.Box(c).Height(12)
	ui.Row(c.Key("local-actions")).Gap(10).AlignItems(ui.Center).Children(func() {
		if needs && !st.Busy() {
			if ui.PrimaryButton(c.Key("download"), T("obDownload")).Clicked() {
				o.activateLocal()
				o.a.localASR.Deploy("")
			}
			ui.Text(c, T("obDownloadSize", float64(localasr.ModelBytes())/1e9+0.65)).FontSize(12.5).TextColor(t.TextMuted)
		}
		if st.Installed && st.Phase != "ready" && !st.Busy() {
			if ui.PrimaryButton(c.Key("start"), T("builtinStart")).Clicked() {
				o.activateLocal()
				o.a.localASR.Start()
			}
		}
		if st.Busy() {
			if ui.Button(c.Key("cancel"), T("cancelBtn")).Disabled(st.Phase == "stopping").Clicked() {
				o.a.localASR.Stop()
			}
		}
		if st.Phase == "ready" && (a.Provider != providers.Local || !s.ASRManaged) {
			if ui.Button(c.Key("default"), T("setDefault")).Clicked() {
				o.activateLocal()
			}
		}
	})
}

// permissions is the third step. The states are what macOS reports, and
// the buttons do what those of Settings → Permissions do: ask, or open the
// pane of System Settings where the permission is given.
func (o *onboardWindow) permissions(c *ui.Context) {
	t := c.Theme()
	c.After(time.Second) // they are granted outside the app: look again
	mic := macos.MicrophoneStatus()
	ax, input := macos.AccessibilityTrusted(false), macos.InputMonitoringGranted(false)
	row := func(key string, svg *ui.SVG, name, why string, control func()) {
		ui.Row(c.Key(key)).Gap(14).MinHeight(62).Padding(10, 0).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, svg).FontSize(18).TextColor(t.TextMuted)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, name).FontSize(14).FontWeight(500)
				ui.Text(c, why).FontSize(13).TextColor(t.TextMuted)
			})
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
		})
	}
	granted := func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(t.Success)
		ui.Text(c, T("granted")).FontSize(13).SingleLine()
	}
	ui.Column(c.Key("perms")).Radius(12).Background(cardColor(t)).Border(1, t.Border).Padding(0, 16).Gap(1).Dividers(1, t.Border).Children(func() {
		row("mic", icMic, T("pMic"), T("obWhyMic"), func() {
			switch mic {
			case "authorized":
				granted()
			case "undetermined":
				if ui.Button(c.Key("ask"), T("request")).Clicked() {
					macos.RequestMicrophone(func(bool) { o.a.refreshAll() })
				}
			default: // denied: macOS will not ask again
				ui.Text(c, T("obDenied")).FontSize(13).TextColor(t.Danger).SingleLine()
				if ui.Button(c.Key("open"), T("openSettings")).Clicked() {
					macos.OpenPrivacySettings("Privacy_Microphone")
				}
			}
		})
		row("ax", icPerson, T("pAx"), T("obWhyAx"), func() {
			if ax {
				granted()
				return
			}
			ui.Text(c, T("notGranted")).FontSize(13).TextColor(t.Danger).SingleLine()
			if ui.Button(c.Key("ask"), T("request")).Clicked() {
				macos.AccessibilityTrusted(true)
			}
		})
		row("input", icKeyboard, T("pInput"), T("obWhyInput"), func() {
			if input {
				granted()
				return
			}
			ui.Text(c, T("notGranted")).FontSize(13).TextColor(t.Danger).SingleLine()
			if ui.Button(c.Key("open"), T("openSettings")).Clicked() {
				macos.OpenPrivacySettings("Privacy_ListenEvent")
			}
		})
	})
	ui.Box(c).Height(14)
	note := T("obPermsNote")
	switch {
	case mic != "authorized" && mic != "undetermined":
		note = T("obDeniedNote")
	case !ax || !input:
		note = T("permHint")
	case mic != "authorized":
		note = ""
	}
	if note != "" {
		ui.Text(c, note).FontSize(13).TextColor(t.TextMuted)
	}
}

// firstDictation is the last step: one real dictation whose transcript
// goes to the guide's own field and nowhere else. "Start practice" starts a
// practice run of the session controller, which is given a sink for that
// one run: such a run never calls macos.Insert, never touches the
// clipboard and is never observed (internal/session). The step is complete
// when that run delivers a transcript that is not empty, and from nothing
// else: the history is not consulted and typing in the field does not count.
func (o *onboardWindow) firstDictation(c *ui.Context, s config.Settings, ready bool) {
	t := c.Theme()
	ctrl := o.a.ctrl
	view := ctrl.View()
	phase := view.Phase
	practicing := ctrl.Practicing()
	recording := practicing && phase == session.Recording
	failed := practicing && phase == session.Failed
	// One surface: the field, and under a hairline its recording state and
	// actions. Its edge darkens with the focus, as a field's would.
	edge := t.Border
	surface := ui.Column(c.Key("practice-surface")).FillWidth().MaxWidth(600).Radius(12).Background(cardColor(t)).Clip()
	surface.Children(func() {
		area := ui.TextArea(c.Key("practice"), &o.practice).Lines(5, 10).Label(T("obField")).
			FillWidth().Padding(14, 16).BorderWidth(0).Background(ui.Transparent)
		if area.Focused() || recording {
			edge = t.TextMuted
		}
		ui.Row(c.Key("practice-actions")).FillWidth().Height(48).Padding(0, 10, 0, 14).Gap(8).AlignItems(ui.Center).
			Background(fillColor(t)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			switch {
			case recording:
				// A red dot and the live levels: the field is listening.
				ui.Box(c).Size(7, 7).Radius(3.5).Background(t.Danger)
				ui.Row(c).Height(16).Gap(2).AlignItems(ui.Center).Children(func() {
					for _, l := range view.Levels {
						ui.Box(c).Size(2, 3+13*min(max(l, 0), 1)).Radius(1).Background(t.TextMuted)
					}
				})
			case failed:
				ui.Text(c, view.Message).FontSize(12.5).TextColor(t.Danger).Grow(1).MinWidth(0).SingleLine().Selectable()
			case practicing && phase != session.Done:
				ui.Spinner(c) // preparing, finalizing, tidying
			}
			ui.Spacer(c)
			switch {
			case recording:
				if ui.Button(c.Key("cancel"), T("cancel")).Clicked() {
					o.discardPractice()
				}
				if ui.PrimaryButton(c.Key("stop"), T("stopInsert")).Clicked() {
					go ctrl.Confirm()
				}
			case practicing && !failed && phase != session.Done:
				// It can still be abandoned.
				if ui.Button(c.Key("cancel"), T("cancel")).Clicked() {
					o.discardPractice()
				}
			default:
				// Starting again appends to what is in the field.
				busy := phase != session.Idle && phase != session.Done && phase != session.Failed
				start := ui.Button(c.Key("start"), "").Label(T("obStart")).Disabled(!ready || busy || o.a.updates.installing).Children(func() {
					ui.Icon(c, icMic).FontSize(13)
					ui.Text(c, T("obStart")).SingleLine()
				})
				if start.Clicked() {
					o.startPractice()
				}
			}
		})
	})
	surface.Border(1, edge)
}

// startPractice starts the practice run and gives it its sink. The sink is
// tied to this start: if the step or the window is left before the
// transcript arrives, the run is cancelled and a result that still comes is
// dropped.
func (o *onboardWindow) startPractice() {
	o.practiceRun++
	run := o.practiceRun
	sink := func(text string) {
		mygo.RunOnMain(func() {
			if o.practiceRun != run || !o.visible() {
				return // left meanwhile: discarded
			}
			if strings.TrimSpace(o.practice) != "" {
				o.practice = strings.TrimRight(o.practice, " \n") + " "
			}
			o.practice += text
			o.change(func(ob *config.Onboarding) { ob.Practiced = true })
			o.refresh()
		})
	}
	o.a.ctrl.StartPractice(sink)
}

// discardPractice ends the practice run in progress, if any, and makes
// sure nothing it produces later is used. It is called whenever the step
// or the window is left.
func (o *onboardWindow) discardPractice() {
	o.practiceRun++
	o.a.ctrl.CancelPractice()
}

// finished is the completion view, in the same frame: its actions are in
// the footer.
func (o *onboardWindow) finished(c *ui.Context, s config.Settings) {
	t := c.Theme()
	ui.Column(c.Key("finished")).FillWidth().AlignItems(ui.Center).Padding(56, 0, 0).Children(func() {
		ui.Row(c).Size(48, 48).Radius(24).Justify(ui.Center).AlignItems(ui.Center).Background(okTint(t)).Children(func() {
			ui.Icon(c, icCheck).FontSize(24).TextColor(t.Success)
		})
		ui.Box(c).Height(18)
		ui.Text(c, T("obComplete")).FontSize(26).FontWeight(600)
		ui.Box(c).Height(8)
		ui.Text(c, T("obCompleteText", keyName(s.TriggerKey))).FontSize(15).TextColor(softText(t)).MaxWidth(440)
	})
}
