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

// onboardWindow hosts the approved V6 interface in an embedded local page.
// Permissions, hotkeys and practice transcripts come from the native bridge.
// Progress is persisted; practice never inserts text into another application.
type onboardWindow struct {
	a   *app
	win *mygo.Window

	// review shows the steps, even once all are complete. The guide opens
	// on them unless it is already complete, and completing the last step
	// never leaves them by itself: only Finish or Enter Voicemore does,
	// so the practice transcript stays to read, edit and try again.
	review            bool
	pressed           bool
	practice          string // the text of the practice field
	practiceAttempt   bool
	practiceDelivered bool
	practiceRun       int  // counts practice runs and their discards: a sink of an earlier count is stale
	permsWere         bool // the permissions were all granted when last looked at

	// The stage is centered in the height between the title bar and the
	// footer, from its height in the last frame (stageH) for the content
	// it showed (stageKey).
	stageKey    string
	stageH      float32
	practicedAt time.Time // when the last practice transcript arrived, for the result's entrance
}

// The steps, by the text key of their name; the heading's key adds "h".
var obSteps = []string{"obS1", "obS2", "obS3", "obS4"}

func newOnboardWindow(a *app) *onboardWindow {
	o := &onboardWindow{a: a}
	setupOnboardingFrontend(o)
	o.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           T("obTitle"),
		Width:           720,
		Height:          540,
		MinWidth:        620,
		MinHeight:       480,
		Hidden:          true,
		StateKey:        "onboarding-v6",
		TitleBarStyle:   mygo.TitleBarHidden,
		DisableMinimize: true,
		URL:             "/",
		BackgroundColor: "#080d19",
	})
	o.win.Page().OnWillNavigate(func(e *mygo.NavigateEvent) {
		if e.URL != "mygo://localhost/" {
			e.PreventDefault()
		}
	})
	// Closing returns to the main window without claiming completion.
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
	if s.Onboarding.GuideVersion != 7 {
		o.change(func(ob *config.Onboarding) {
			if ob.GuideVersion == 6 {
				ob.Step = 1
			} else {
				ob.Step = 0
				ob.KeyDetected = false
			}
			ob.GuideVersion = 7
			ob.Practiced = false
		})
	}
	o.change(func(ob *config.Onboarding) {
		if ob.Step >= 4 {
			ob.Step = 0
		}
		ob.Left = false
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

// icInsert is the third move of the introduction: lines of text and the
// cursor they are inserted at, in the family of icons.go.
var icInsert = icon(`<path d="M2.4 4.4h6.4M2.4 8h4.4M2.4 11.6h5.6M12 3.4v9.2M10.6 3.4h2.8M10.6 12.6h2.8"/>`)

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
	titleH := max(bar.Height, 40)
	const footH = 64
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		// The title bar is bare: the window controls are macOS's, and the
		// strip only moves the window. It shares the window's surface.
		ui.Box(c.Key("title")).Height(titleH).FillWidth().DragWindow()

		// The stage: one centered column, at most 540 wide (the practice
		// step excepted), centered in the
		// height left by the title bar and the footer. Its height is that of
		// the last frame; when it changes, the frame is drawn again once.
		key := fmt.Sprintf("stage-%d-%t", step, fin)
		top := float32(8)
		if b := o.win.ContentBounds(); b.Height > 0 && o.stageKey == key {
			room := float32(b.Height) - titleH - footH - 32
			top = max(8, (room-o.stageH)/2)
		}
		ui.Scroll(c.Key("body-"+key)).Grow(1).Padding(top, 48, 24).AlignItems(ui.Center).Children(func() {
			// The practice field keeps the earlier native rule: it widens with
			// the window, between equal margins. The other steps keep to 540.
			stage := ui.Column(c.Key(key)).FillWidth()
			if fin || step != len(obSteps)-1 {
				stage.MaxWidth(540)
			}
			if h := stage.Bounds().H; h > 0 && (o.stageKey != key || abs32(h-o.stageH) > 1) {
				o.stageKey, o.stageH = key, h
				c.After(0)
			}
			stage.Children(func() {
				if fin {
					o.finished(c, s)
					return
				}
				ui.Row(c.Key("visual")).Height(56).AlignItems(ui.Center).Children(func() { o.visual(c, step, s) })
				ui.Box(c).Height(22)
				ui.Text(c, T("obStepOf", step+1, len(obSteps))+" · "+T(obSteps[step])).FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.Box(c).Height(6)
				ui.Text(c, T(obSteps[step]+"h")).FontSize(24).FontWeight(600)
				ui.Box(c).Height(22)
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
				// Continue waits for the speech model and the permissions; say
				// so beside the step, not in the footer.
				if (step == 1 || step == 2) && !done[step] {
					ui.Box(c).Height(12)
					ui.Text(c, T("obNeedStep")).FontSize(12.5).TextColor(t.TextMuted)
				}
			})
		})

		// The footer: Back on the left, the progress as dots in the middle,
		// Later and the one primary action on the right, always in the same
		// places. Divided by a hairline only.
		ui.Row(c.Key("foot")).Height(footH).Padding(0, 22).Gap(10).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Row(c.Key("left")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Children(func() {
				if fin {
					if quietButton(c, "review", T("obReview"), nil).Clicked() {
						o.review = true
					}
					return
				}
				// None on the first step; the zones keep the dots centered.
				if step > 0 && quietButton(c, "back", T("obBack"), icBack).Clicked() {
					o.goStep(step - 1)
				}
			})
			o.dots(c, step, done, fin, count)
			ui.Row(c.Key("right")).Grow(1).Basis(0).MinWidth(0).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if fin {
					if ui.PrimaryButton(c.Key("enter"), T("obEnter")).MinWidth(96).Clicked() {
						o.leave()
					}
					return
				}
				later := "obLater"
				if all {
					later = "obEnter"
				}
				if quietButton(c, "later", T(later), nil).Clicked() {
					o.leave()
				}
				if step == len(obSteps)-1 {
					if ui.PrimaryButton(c.Key("finish"), T("obFinish")).MinWidth(96).Disabled(!all).Clicked() {
						o.leave()
					}
				} else if ui.PrimaryButton(c.Key("next"), T("obContinue")).MinWidth(96).Disabled(!canEnter(step + 1)).Clicked() {
					o.goStep(step + 1)
				}
			})
		})
	})
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// quietButton is a footer action without a frame: secondary text that
// takes a fill under the pointer.
func quietButton(c *ui.Context, key, label string, svg *ui.SVG) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Height(32).Padding(0, 12).Gap(5).Radius(8).AlignItems(ui.Center).Label(label)
	if b.Hovered() {
		b.Background(hoverColor(t))
	}
	b.Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(12).TextColor(softText(t))
		}
		ui.Text(c, label).FontSize(13).TextColor(softText(t)).SingleLine()
	})
	return b
}

// dots is the progress: one dot per step, the current one a short bar,
// the done ones darker. Each goes to its step.
func (o *onboardWindow) dots(c *ui.Context, step int, done [4]bool, fin bool, count int) {
	t := c.Theme()
	ui.Row(c.Key("dots")).Shrink(0).Gap(2).AlignItems(ui.Center).Label(T("obTitle") + " — " + T("obProgress", count, len(obSteps))).Children(func() {
		for i, name := range obSteps {
			current := !fin && i == step
			state := T("obStTodo")
			if done[i] {
				state = T("obStDone")
			}
			w, dw := float32(18), float32(6)
			if current {
				w, dw = 30, 18
			}
			b := ui.ButtonBase(c.Key("dot-"+itoa(i))).Size(w, 22).Radius(6).Justify(ui.Center).AlignItems(ui.Center).Label(T(name) + ", " + state)
			col := strongLine(t)
			switch {
			case current:
				col = t.Text
			case done[i] || b.Hovered():
				col = softText(t)
			}
			b.Children(func() { ui.Box(c).Size(dw, 6).Radius(3).Background(col) })
			if b.Clicked() {
				o.goStep(i)
			}
		}
	})
}

// visual is a step's one quiet picture: the app icon, the two line
// drawings of the guide (speech becoming text, a switch turned on), and
// the trigger key.
func (o *onboardWindow) visual(c *ui.Context, step int, s config.Settings) {
	switch step {
	case 0:
		ui.Box(c).Size(56, 56).Draw(drawAppMark)
	case 1:
		ui.Box(c).Size(52, 52).Draw(func(p *ui.Painter, r ui.Rect) { drawSpeechToText(p, r, c.Theme()) })
	case 2:
		ui.Box(c).Size(52, 52).Draw(func(p *ui.Painter, r ui.Rect) { drawAllowSwitch(p, r, c.Theme()) })
	default:
		keycap(c, keyName(s.TriggerKey), 46, 15)
	}
}

// ink is the theme's text color at an opacity, for the line drawings.
func ink(t *ui.Theme, a float32) ui.Color {
	if t.Dark {
		return ui.RGBA(250, 250, 250, a)
	}
	return ui.RGBA(38, 38, 38, a)
}

// drawSpeechToText is mock/assets/onboarding-speech-to-text.svg: four
// level bars beside three lines of text, on a 48 grid centered in r.
func drawSpeechToText(p *ui.Painter, r ui.Rect, t *ui.Theme) {
	u := float32(1)
	x0, y0 := r.X+(r.W-48)/2, r.Y+(r.H-48)/2
	const w = 2.25
	for _, b := range [][3]float32{{8, 20, 28}, {12.5, 16, 32}, {17, 13, 35}, {21.5, 19, 29}} {
		p.Fill(ui.Rect{X: x0 + (b[0]-w/2)*u, Y: y0 + (b[1]-w/2)*u, W: w, H: (b[2] - b[1] + w) * u}, ink(t, 1), w/2)
	}
	for _, l := range [][3]float32{{28, 40, 18}, {28, 40, 24}, {28, 35, 30}} {
		p.Fill(ui.Rect{X: x0 + (l[0]-w/2)*u, Y: y0 + (l[2]-w/2)*u, W: (l[1] - l[0] + w) * u, H: w}, ink(t, 0.42), w/2)
	}
}

// drawAllowSwitch is mock/assets/onboarding-allow-switch.svg: a System
// Settings switch turned on, with a light check where the knob was.
func drawAllowSwitch(p *ui.Painter, r ui.Rect, t *ui.Theme) {
	x0, y0 := r.X+(r.W-48)/2, r.Y+(r.H-48)/2
	track := ui.Rect{X: x0 + 5, Y: y0 + 13, W: 38, H: 22}
	p.Fill(track, ink(t, 0.06), 11)
	p.Stroke(track, ink(t, 1), 11, 2)
	p.Fill(ui.Rect{X: x0 + 25, Y: y0 + 17, W: 14, H: 14}, ink(t, 1), 7)
	check := (&ui.Path{}).MoveTo(x0+11.8, y0+24.3).LineTo(x0+14.3, y0+26.8).LineTo(x0+19.2, y0+21.4)
	p.StrokePath(check, 2, ink(t, 0.45))
}

// intro is the first step: press the key, speak, the text appears. One
// sequence on the sunken well, each move's picture in a 64 px column.
func (o *onboardWindow) intro(c *ui.Context, s config.Settings) {
	t := c.Theme()
	key := keyName(s.TriggerKey)
	first, third := T("obI1text"), T("obI3text", key)
	if s.TriggerMode == config.TriggerHold {
		first, third = T("obI1holdText"), T("obI3holdText")
	}
	item := func(k string, visual func(), title, text string) {
		ui.Row(c.Key(k)).Gap(14).Padding(12, 0).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Width(64).Height(40).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Clip().Children(visual)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, title).FontSize(14).FontWeight(600)
				ui.Text(c, text).FontSize(13).LineHeight(1.5).TextColor(t.TextMuted)
			})
		})
	}
	ui.Column(c.Key("intro")).Radius(12).Background(fillColor(t)).Padding(4, 18).Gap(1).Dividers(1, t.Border).Children(func() {
		item("press", func() { keycap(c, key, 30, 12.5) }, T("obI1", key), first)
		item("speak", func() { capsuleMini(c, s) }, T("obI2"), T("obI2text"))
		item("text", func() { ui.Icon(c, icInsert).FontSize(24).TextColor(softText(t)) }, T("obI3"), third)
	})
}

// capsuleMini is the capsule of the chosen style, small: Indigo is
// overlay_v2.go's pill with five 3 px bars, the last mint, 28 px high;
// Classic is the black pill of overlay.go with its level wave.
func capsuleMini(c *ui.Context, s config.Settings) {
	if s.CapsuleStyle == config.CapsuleClassic {
		ui.Row(c).Height(28).Padding(0, 10).Gap(3).Radius(14).AlignItems(ui.Center).Background(ui.RGB(42, 44, 50)).Children(func() {
			for _, h := range []float32{4, 8, 12, 6, 13, 9, 5} {
				ui.Box(c).Size(2, h).Radius(1).Background(ui.RGBA(255, 255, 255, 0.8))
			}
		})
		return
	}
	ui.Row(c).Height(28).Padding(0, 10).Gap(3).Radius(14).AlignItems(ui.Center).
		Gradient(v2Indigo, v2IndigoEnd, 135).Border(0.5, ui.RGBA(255, 255, 255, 0.11)).Children(func() {
		for i, h := range []float32{8, 13, 17, 11, 7} {
			col := v2Ink
			if i == 4 {
				col = v2Mint
			}
			ui.Box(c).Size(3, h).Radius(1.5).Background(col)
		}
	})
}

// choice is one of the two options of the speech step, drawn as a card
// with a radio mark. It reports a press that changes the choice.
func (o *onboardWindow) choice(c *ui.Context, key, title, text string, on bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Grow(1).Basis(0).MinWidth(0).Padding(14, 16).Gap(12).Radius(12).Justify(ui.Start).AlignItems(ui.Start).Label(title)
	border := t.Border
	b.Background(cardColor(t))
	if on {
		border = t.TextMuted
		b.Background(fillColor(t))
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
	ui.Box(c).Height(14)
	ui.Column(c.Key("branch")).Radius(12).Background(fillColor(t)).Padding(14, 16).Children(func() { o.speechBranch(c, s) })
}

// speechBranch is the state and the actions of the chosen branch, on the
// sunken well under the two choices.
func (o *onboardWindow) speechBranch(c *ui.Context, s config.Settings) {
	t := c.Theme()
	cloud := s.Onboarding.Cloud
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
			ui.Text(c, T("obDownloadSize", float64(localasr.DownloadBytes())/1e9)).FontSize(12.5).TextColor(t.TextMuted)
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

// permissions is the third step, revealed in order. The states are what
// macOS reports. Granted rows fold to a quiet line; the first missing one
// is the focus, with its reason and the primary button; the ones after it
// wait with a plain button. The buttons do what those of Settings →
// Permissions do: ask for the microphone, or open System Settings with the
// drag helper for Accessibility and Input Monitoring.
func (o *onboardWindow) permissions(c *ui.Context) {
	t := c.Theme()
	c.After(time.Second) // they are granted outside the app: look again
	mic := macos.MicrophoneStatus()
	ax, input := macos.AccessibilityTrusted(false), macos.InputMonitoringGranted(false)
	type perm struct {
		key     string
		svg     *ui.SVG
		name    string
		why     string
		granted bool
		act     func(c *ui.Context, primary bool)
	}
	button := func(c *ui.Context, key, label string, primary bool) bool {
		if primary {
			return ui.PrimaryButton(c.Key(key), label).Clicked()
		}
		return ui.Button(c.Key(key), label).Clicked()
	}
	perms := []perm{
		{"mic", icMic, T("pMic"), T("obWhyMic"), mic == "authorized", func(c *ui.Context, primary bool) {
			if mic == "undetermined" {
				if button(c, "ask", T("request"), primary) {
					macos.RequestMicrophone(func(bool) { o.a.refreshAll() })
				}
				return
			}
			// Denied: macOS will not ask again.
			ui.Text(c, T("obDenied")).FontSize(13).TextColor(t.Danger).SingleLine()
			if button(c, "open", T("openSettings"), primary) {
				macos.OpenPrivacySettings("Privacy_Microphone")
			}
		}},
		{"ax", icPerson, T("pAx"), T("obWhyAx"), ax, func(c *ui.Context, primary bool) {
			if button(c, "ask", T("request"), primary) {
				o.a.prefs.showPermissionDrag("Privacy_Accessibility")
			}
		}},
		{"input", icKeyboard, T("pInput"), T("obWhyInput"), input, func(c *ui.Context, primary bool) {
			if button(c, "ask", T("request"), primary) {
				o.a.prefs.showPermissionDrag("Privacy_ListenEvent")
			}
		}},
	}
	next := -1
	for i, p := range perms {
		if !p.granted {
			next = i
			break
		}
	}
	ui.Column(c.Key("perms")).Radius(12).Background(fillColor(t)).Padding(0, 16).Gap(1).Dividers(1, t.Border).Children(func() {
		for i, p := range perms {
			focus := i == next
			height := float32(52)
			switch {
			case p.granted:
				height = 44
			case focus:
				height = 64
			}
			ui.Row(c.Key(p.key)).Gap(14).MinHeight(height).Padding(10, 0).AlignItems(ui.Center).Children(func() {
				if p.granted {
					ui.Row(c).Size(18, 18).Radius(9).Justify(ui.Center).AlignItems(ui.Center).Background(okTint(t)).Children(func() {
						ui.Icon(c, icCheck).FontSize(11).TextColor(t.Success)
					})
				} else {
					col := t.TextMuted
					if focus {
						col = softText(t)
					}
					ui.Icon(c, p.svg).FontSize(18).TextColor(col)
				}
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					name := ui.Text(c, p.name).FontSize(14)
					switch {
					case p.granted:
						name.TextColor(softText(t))
					case focus:
						name.FontWeight(500)
						ui.Text(c, p.why).FontSize(12.5).TextColor(t.TextMuted)
					default:
						name.TextColor(t.TextMuted)
					}
				})
				ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
					if p.granted {
						ui.Box(c).Size(8, 8).Radius(4).Background(t.Success)
						ui.Text(c, T("granted")).FontSize(13).SingleLine()
						return
					}
					p.act(c, focus)
				})
			})
		}
	})
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
		ui.Box(c).Height(12)
		ui.Text(c, note).FontSize(12.5).TextColor(t.TextMuted)
	}
}

// firstDictation is the last step: one real dictation whose transcript
// goes to the guide's own field and nowhere else. "Start practice" starts a
// practice run of the session controller, which is given a sink for that
// one run: such a run never calls macos.Insert, never touches the
// clipboard and is never observed (internal/session). The step is complete
// when that run delivers a transcript that is not empty, and from nothing
// else: the history is not consulted and typing in the field does not count.
//
// Above the field: what is still missing, each with the way to it, while
// the run cannot start; or, once a dictation is in, the result line.
func (o *onboardWindow) firstDictation(c *ui.Context, s config.Settings, ready bool) {
	t := c.Theme()
	ctrl := o.a.ctrl
	view := ctrl.View()
	phase := view.Phase
	practicing := ctrl.Practicing()
	recording := practicing && phase == session.Recording
	failed := practicing && phase == session.Failed
	// A run that is Done has delivered its transcript; the controller keeps
	// it Practicing until the capsule's dismissal, so Done counts as finished
	// here, or the result would miss its entrance.
	practiced := s.Onboarding.Practiced && (!practicing || phase == session.Done)

	if !ready && !practicing {
		type need struct {
			step       int
			name, what string
		}
		var needs []need
		if !o.speechReady(s) {
			what := T("obStTodo")
			if st := o.a.localASR.Snapshot(); s.ASRAssign.Provider == providers.Local && st.Busy() {
				what = localASRState(st)
			}
			needs = append(needs, need{1, T("obS2"), what})
		}
		if !permissionsGranted() {
			needs = append(needs, need{2, T("obS3"), T("obStTodo")})
		}
		ui.Column(c.Key("needs")).Radius(12).Background(noticeColor(t)).Padding(12, 14).Gap(2).Children(func() {
			ui.Text(c, T("obNeedFirst")).FontSize(13).FontWeight(500)
			for _, n := range needs {
				ui.Row(c.Key("need-" + itoa(n.step))).MinHeight(34).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, n.name).FontSize(13).SingleLine()
					ui.Text(c, n.what).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
					ui.Spacer(c)
					if ui.Button(c.Key("go"), T("obFix")).Clicked() {
						o.goStep(n.step)
					}
				})
			}
		})
		ui.Box(c).Height(14)
	}

	// The result rises in once, with the field's edge turning green.
	since := time.Since(o.practicedAt)
	fresh := !o.practicedAt.IsZero() && since < 1200*time.Millisecond && !c.Preferences().ReduceMotion
	if fresh {
		c.After(16 * time.Millisecond)
	}
	if practiced {
		alpha, rise := float32(1), float32(0)
		if fresh && since < 320*time.Millisecond {
			k := float32(since) / float32(320*time.Millisecond)
			alpha, rise = k, 4*(1-k)
		}
		ui.Row(c.Key("result")).Gap(9).AlignItems(ui.Center).Opacity(alpha).Translate(0, rise).Children(func() {
			ui.Row(c).Size(18, 18).Radius(9).Justify(ui.Center).AlignItems(ui.Center).Background(okTint(t)).Children(func() {
				ui.Icon(c, icCheck).FontSize(11).TextColor(t.Success)
			})
			ui.Text(c, T("obPracticed")).FontSize(13.5).FontWeight(500)
		})
		ui.Box(c).Height(10)
	}

	// One surface: the field, and under a hairline its recording state and
	// the action. Its edge darkens with the focus, as a field's would.
	edge := t.Border
	if practiced {
		edge = successEdge(t, fresh)
	}
	surface := ui.Column(c.Key("practice-surface")).FillWidth().Radius(12).Background(cardColor(t)).Clip()
	surface.Children(func() {
		area := ui.TextArea(c.Key("practice"), &o.practice).Lines(4, 8).Label(T("obField")).Placeholder(T("obFieldPh")).
			FillWidth().Padding(14, 16).FontSize(15).BorderWidth(0).Background(ui.Transparent)
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
				// Starting again appends to what is in the field. Before the
				// first dictation it is the primary action; after, Try again.
				busy := phase != session.Idle && phase != session.Done && phase != session.Failed
				label := T("obStart")
				if practiced {
					label = T("obAgain")
				}
				content := func() {
					ui.Icon(c, icMic).FontSize(13)
					ui.Text(c, label).SingleLine()
				}
				var start ui.Element
				if practiced {
					start = ui.Button(c.Key("start"), "").Label(label).Disabled(!ready || busy || o.a.updates.committing).Children(content)
				} else {
					start = ui.PrimaryButton(c.Key("start"), "").Label(label).Disabled(!ready || busy || o.a.updates.committing).Children(content)
				}
				if start.Clicked() {
					o.startPractice()
				}
			}
		})
	})
	surface.Border(1, edge)
	ui.Box(c).Height(12)
	ui.Text(c, T("obPracticeNote")).FontSize(12.5).TextColor(t.TextMuted)
}

// successEdge is the practice field's edge once a dictation is in: green,
// stronger for a moment after it arrives.
func successEdge(t *ui.Theme, fresh bool) ui.Color {
	a := float32(0.45)
	if fresh {
		a = 0.9
	}
	if t.Dark {
		return ui.RGBA(52, 211, 153, a)
	}
	return ui.RGBA(4, 120, 87, a)
}

// startPractice starts the practice run and gives it its sink. The sink is
// tied to this start: if the step or the window is left before the
// transcript arrives, the run is cancelled and a result that still comes is
// dropped.
func (o *onboardWindow) startPractice() {
	o.practiceAttempt = true
	o.practiceDelivered = false
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
			o.practiceDelivered = true
			o.practicedAt = time.Now() // the result line rises in
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
	o.practiceAttempt = false
	o.practiceRun++
	o.a.ctrl.CancelPractice()
}

// finished is the completion view, on the same stage, centered: its
// actions are in the footer.
func (o *onboardWindow) finished(c *ui.Context, s config.Settings) {
	t := c.Theme()
	ui.Column(c.Key("finished")).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Row(c).Size(52, 52).Radius(26).Justify(ui.Center).AlignItems(ui.Center).Background(okTint(t)).Children(func() {
			ui.Icon(c, icCheck).FontSize(26).TextColor(t.Success)
		})
		ui.Box(c).Height(22)
		ui.Text(c, T("obComplete")).FontSize(24).FontWeight(600).TextAlign(ui.Center)
		ui.Box(c).Height(8)
		ui.Text(c, T("obCompleteText", keyName(s.TriggerKey))).FontSize(14).LineHeight(1.55).TextColor(softText(t)).TextAlign(ui.Center).MaxWidth(440)
	})
}
