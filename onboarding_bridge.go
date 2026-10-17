package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"

	"github.com/egoist/mygo"
	"voiceinput/internal/config"
	"voiceinput/internal/localasr"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/session"
)

//go:embed resources/onboarding
var onboardingAssets embed.FS

type onboardingBridge struct{ o *onboardWindow }

func init() {
	files, err := fs.Sub(onboardingAssets, "resources/onboarding")
	if err != nil {
		panic(err)
	}
	mygo.SetFrontend(files)
}

// All guide state belongs to the main thread. Only the local guide window
// may call this bridge; no permission, detection or transcript is supplied by JS.
func (b *onboardingBridge) onMain(ctx context.Context, fn func() error) error {
	if mygo.CallerWindow(ctx) != b.o.win {
		return errors.New("invalid onboarding caller")
	}
	done := make(chan error, 1)
	mygo.RunOnMain(func() {
		if ctx.Err() != nil {
			done <- ctx.Err()
			return
		}
		done <- fn()
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (o *onboardWindow) guideState() map[string]any {
	s := o.a.settings.Get()
	granted := permissionsGranted()
	if granted != o.permsWere {
		o.permsWere = granted
		if granted {
			o.a.bindTrigger()
		}
	}
	phase := "idle"
	v := o.a.ctrl.View()
	if o.a.ctrl.Practicing() {
		switch v.Phase {
		case session.Recording:
			phase = "recording"
		case session.Preparing, session.Finalizing, session.Cleaning, session.Inserting:
			phase = "processing"
		case session.Failed:
			phase = "error"
		}
	}
	if o.practiceAttempt && !o.practiceDelivered {
		if v.Phase == session.Failed {
			phase = "error"
		} else if v.Phase == session.Done || v.Phase == session.Idle {
			phase = "empty"
		}
	}
	key := s.TriggerKey
	switch key {
	case "RightCommand":
		key = "MetaRight"
	case "RightOption":
		key = "AltRight"
	}
	theme := s.Appearance
	if theme == "" || theme == "system" {
		theme = "auto"
	}
	st := o.a.localASR.Snapshot()
	model := map[string]any{"cloud": s.Onboarding.Cloud, "ready": o.speechReady(s) && s.Onboarding.Cloud == (s.ASRAssign.Provider != providers.Local), "name": localasr.Model, "phase": st.Phase, "status": localASRState(st), "installed": st.Installed, "busy": st.Busy(), "done": st.Done, "total": st.Total, "detail": st.Detail, "sizeGB": float64(localasr.ModelBytes())/1e9 + 0.65, "selected": s.ASRAssign.Provider == providers.Local && s.ASRManaged, "current": o.a.modelLabel(providers.KindASR, s)}
	return map[string]any{"model": model, "language": langIndex.Load(), "step": s.Onboarding.Step, "perms": []bool{macos.MicrophoneStatus() == "authorized", macos.AccessibilityTrusted(false), macos.InputMonitoringGranted(false)}, "shortcut": key, "mode": s.TriggerMode, "keySeen": s.Onboarding.KeyDetected, "practiced": s.Onboarding.Practiced, "text": o.practice, "theme": theme, "speechReady": o.speechReady(s), "phase": phase, "pressed": o.pressed, "message": v.Message}
}
func (b *onboardingBridge) State(ctx context.Context) (map[string]any, error) {
	var state map[string]any
	err := b.onMain(ctx, func() error { state = b.o.guideState(); return nil })
	return state, err
}
func (b *onboardingBridge) Action(ctx context.Context, action, value string) (map[string]any, error) {
	var state map[string]any
	err := b.onMain(ctx, func() error {
		o := b.o
		s := o.a.settings.Get()
		step := s.Onboarding.Step
		if !o.visible() {
			return errors.New("guide is closed")
		}
		switch action {
		case "allow":
			switch value {
			case "0":
				if macos.MicrophoneStatus() == "undetermined" {
					macos.RequestMicrophone(func(bool) { o.refresh() })
				} else if macos.MicrophoneStatus() != "authorized" {
					macos.OpenPrivacySettings("Privacy_Microphone")
				}
			case "1":
				if !macos.AccessibilityTrusted(false) {
					o.a.prefs.showPermissionDrag("Privacy_Accessibility")
				}
			case "2":
				if !macos.InputMonitoringGranted(false) {
					o.a.prefs.showPermissionDrag("Privacy_ListenEvent")
				}
			default:
				return errors.New("invalid permission")
			}
		case "key", "mode":
			if step != 2 {
				return errors.New("not the shortcut step")
			}
			if action == "key" {
				switch value {
				case "Fn":
					s.TriggerKey = "Fn"
				case "MetaRight":
					s.TriggerKey = "RightCommand"
				case "AltRight":
					s.TriggerKey = "RightOption"
				default:
					return errors.New("unsupported shortcut")
				}
			} else {
				if value != config.TriggerHold && value != config.TriggerToggle {
					return errors.New("unsupported mode")
				}
				s.TriggerMode = value
			}
			s.Onboarding.KeyDetected = false
			o.pressed = false
			if err := o.a.settings.Set(s); err != nil {
				return err
			}
			o.a.bindTrigger()
		case "next":
			ready := step == 0 && permissionsGranted() || step == 1 && o.speechReady(s) && s.Onboarding.Cloud == (s.ASRAssign.Provider != providers.Local) || step == 2 && s.Onboarding.KeyDetected || step == 3 && s.Onboarding.Practiced && o.guideIdle()
			if !ready {
				return errors.New("请先完成当前步骤")
			}
			o.discardPractice()
			o.change(func(ob *config.Onboarding) { ob.Step = min(4, step+1) })
		case "back":
			o.discardPractice()
			o.pressed = false
			o.change(func(ob *config.Onboarding) { ob.Step = max(0, step-1) })
		case "practice":
			if step != 3 {
				return errors.New("not the practice step")
			}
			o.toggleGuidePractice()
		case "cancel":
			o.discardPractice()
		case "model-choice":
			if step != 1 || (value != "local" && value != "cloud") {
				return errors.New("invalid model choice")
			}
			o.change(func(ob *config.Onboarding) { ob.Cloud = value == "cloud" })
		case "model-download", "model-start", "model-stop", "model-default":
			if step != 1 {
				return errors.New("not the model step")
			}
			st := o.a.localASR.Snapshot()
			switch action {
			case "model-download":
				if !st.Busy() {
					o.activateLocal()
					o.a.localASR.Deploy("")
				}
			case "model-start":
				if st.Installed && !st.Busy() {
					o.activateLocal()
					o.a.localASR.Start()
				}
			case "model-stop":
				if st.Busy() && st.Phase != "stopping" {
					o.a.localASR.Stop()
				}
			case "model-default":
				if st.Phase == "ready" {
					o.activateLocal()
				}
			}
		case "models":
			o.a.prefs.open("models")
		case "text":
			if step != 3 || !o.guideIdle() {
				return errors.New("practice is busy")
			}
			if len(value) > 100000 {
				return errors.New("text too long")
			}
			o.practice = value
		case "finish":
			if step != 4 || !permissionsGranted() || !s.Onboarding.KeyDetected || !s.Onboarding.Practiced {
				return errors.New("guide not complete")
			}
			o.leave()
		default:
			return errors.New("unknown onboarding action")
		}
		state = o.guideState()
		return nil
	})
	return state, err
}
func (o *onboardWindow) guideIdle() bool {
	p := o.a.ctrl.View().Phase
	return p == session.Idle || p == session.Done || p == session.Failed
}

func (o *onboardWindow) toggleGuidePractice() {
	if o.a.ctrl.Practicing() && o.a.ctrl.View().Phase == session.Recording {
		go o.a.ctrl.Confirm()
		return
	}
	s := o.a.settings.Get()
	if !permissionsGranted() || !o.speechReady(s) || o.a.updates.installing {
		return
	}
	v := o.a.ctrl.View()
	if v.Phase != session.Idle && v.Phase != session.Done && v.Phase != session.Failed {
		return
	}
	o.startPractice()
}

// Route real keyboard events through the guide while it is visible, so
// practice never starts an ordinary cross-application dictation.
func (a *app) triggerDown() {
	mygo.RunOnMain(func() {
		if a.onboard != nil && a.onboard.visible() {
			o := a.onboard
			s := a.settings.Get()
			if s.Onboarding.Step == 2 {
				o.pressed = true
			}
			if s.Onboarding.Step == 3 && s.TriggerMode == config.TriggerHold {
				o.toggleGuidePractice()
			}
			return
		}
		go a.ctrl.KeyDown()
	})
}
func (a *app) triggerUp(combo bool) {
	mygo.RunOnMain(func() {
		if a.onboard != nil && a.onboard.visible() {
			o := a.onboard
			s := a.settings.Get()
			o.pressed = false
			if combo {
				if s.TriggerMode == config.TriggerHold {
					o.discardPractice()
				}
				return
			}
			switch s.Onboarding.Step {
			case 2:
				if permissionsGranted() && strings.TrimSpace(a.triggerErr) == "" {
					o.change(func(ob *config.Onboarding) { ob.KeyDetected = true })
				}
			case 3:
				if s.TriggerMode == config.TriggerHold {
					go a.ctrl.Confirm()
				} else {
					o.toggleGuidePractice()
				}
			}
			return
		}
		go a.ctrl.KeyUp(combo)
	})
}

func setupOnboardingFrontend(o *onboardWindow) { mygo.BindAs("Onboarding", &onboardingBridge{o}) }
