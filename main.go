// Voicemore is a local dictation tool for macOS: the Fn key starts a
// recording in any app, the local ASR service transcribes it, and the text
// goes where the keyboard focus was. See docs/requirements.md for what it
// is to do and docs/implementation.md for what this version does.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"voiceinput/internal/asr"
	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/dict"
	"voiceinput/internal/localasr"
	"voiceinput/internal/macos"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/session"
	"voiceinput/internal/store"
)

// app is the state of the process. The windows' views read it on the main
// thread; goroutines change what the views read through Window.Update.
type app struct {
	updates  updateState
	localASR *localasr.Manager
	settings *config.Store
	vault    *secrets.Store
	history  *store.Store
	dict     *dict.Dict
	ctrl     *session.Controller

	tray            *mygo.Tray
	overlay         *overlay
	sendPreview     *sendPreviewWindow
	main            *mainWindow
	quitting        bool
	cleanupCheck    uint64
	cleanupChecking bool
	cleanupReady    bool
	cleanupOptions  cleanup.Options
	cleanupChecked  time.Time

	// stopHotkey ends the current trigger, and triggerErr says why there
	// is none.
	stopHotkey         func()
	shortcutsSuspended bool
	triggerGeneration  uint64
	deviceButtonErr    string
	triggerErr         string

	// The credentials, as read from the app's encrypted local store: the local
	// cleanup key and those of the providers, by account. They are held
	// here for the life of the process and persisted only in that encrypted store.
	keyMu      sync.Mutex
	cleanupKey string
	secrets    map[string]string
	keyErr     string // why the key is not in the encrypted credential store, if it is not

	prefs *prefsWindow
	// subtitles is the live subtitle window, made when first opened (subtitles.go).
	subtitles *subtitleWindow
	// onboard is the first-run guide, a window of its own (onboarding.go).
	onboard *onboardWindow
}

// secret returns the credential of an encrypted credential store account, or "".
func (a *app) secret(account string) string {
	a.keyMu.Lock()
	defer a.keyMu.Unlock()
	if account == cleanupKeyAccount {
		return a.cleanupKey
	}
	return a.secrets[account]
}

// setSecret stores a credential in the encrypted credential store, or removes it when empty.
// If the encrypted credential store refuses, the value still serves until the app quits, and
// the error says it was not stored. Nothing is written to settings.json.
func (a *app) setSecret(account, value string) error {
	if account == cleanupKeyAccount {
		return a.storeKey(value)
	}
	var err error
	if value == "" {
		err = a.vault.Delete(account)
	} else {
		err = a.vault.Set(account, value)
	}
	a.keyMu.Lock()
	if a.secrets == nil {
		a.secrets = map[string]string{}
	}
	a.secrets[account] = value
	a.keyMu.Unlock()
	return err
}

// loadSecrets reads the providers' credentials. A failure to read one
// leaves it empty: its provider then shows as not configured.
func (a *app) loadSecrets() {
	found := map[string]string{}
	for _, account := range providers.Accounts(a.settings.Get()) {
		if v, err := a.vault.Get(account); err == nil && v != "" {
			found[account] = v
		}
	}
	a.keyMu.Lock()
	if a.secrets == nil {
		a.secrets = map[string]string{}
	}
	for k, v := range found {
		if _, typed := a.secrets[k]; !typed {
			a.secrets[k] = v
		}
	}
	a.keyMu.Unlock()
	a.refreshAll()
}

// refreshAll redraws both windows, from any goroutine.
func (a *app) refreshAll() {
	a.main.refresh()
	a.prefs.refresh()
	a.onboard.refresh()
}

// cleanupKeyAccount names the app's one encrypted credential store item.
const cleanupKeyAccount = providers.LocalKeyAccount

func (a *app) key() string {
	a.keyMu.Lock()
	defer a.keyMu.Unlock()
	return a.cleanupKey
}

// storeKey puts the cleanup key in the encrypted credential store, or removes it when empty.
// If the encrypted credential store refuses, the key still serves until the app quits, and
// the error says it was not stored. It is never written to settings.json.
func (a *app) storeKey(value string) error {
	var err error
	if value == "" {
		err = a.vault.Delete(cleanupKeyAccount)
	} else {
		err = a.vault.Set(cleanupKeyAccount, value)
	}
	a.keyMu.Lock()
	a.cleanupKey = value
	a.keyMu.Unlock()
	return err
}

// loadKey reads the key from the encrypted credential store, and moves a plaintext key of an
// earlier version there. The plaintext is only removed from settings.json
// once the encrypted credential store holds the same key and gives it back.
func (a *app) loadKey() {
	stored, err := a.vault.Get(cleanupKeyAccount)
	legacy := a.settings.Get().CleanupAPIKey
	problem := ""
	switch {
	case err != nil:
		problem = T("errKeyRead", err.Error())
		stored = legacy // serve the dictation with what there is
	case legacy != "" && stored == "":
		if err := a.vault.Set(cleanupKeyAccount, legacy); err != nil {
			problem = T("errKeyMigrate", err.Error())
		} else if back, err := a.vault.Get(cleanupKeyAccount); err != nil || back != legacy {
			problem = T("errKeyMigrate", "read-back failed")
		}
		stored = legacy
	}
	if problem == "" && stored != "" {
		// Verify even an existing credential before removing any legacy copy.
		if back, err := a.vault.Get(cleanupKeyAccount); err != nil || back != stored {
			problem = T("errKeyMigrate", "read-back failed")
		} else if err := a.settings.ClearLegacyCleanupKey(back); err != nil {
			problem = T("notSaved", err.Error())
		}
	}
	a.keyMu.Lock()
	a.cleanupKey, a.keyErr = stored, problem
	a.keyMu.Unlock()
	a.main.refresh()
}

func main() {
	a := &app{}
	debug := mygo.App.Name() == "Voicemore Dev" || mygo.IsDev()
	if debug {
		mygo.App.SetName("Voicemore Dev")
	} else {
		mygo.App.SetName("Voicemore")
	}
	// Keep the existing data location across the product rename: models,
	// encrypted credentials, history and window state must remain available.
	dataRoot, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		log.Fatal("app data directory: ", err)
	}
	dataName := "Voice Input"
	if debug {
		dataName = "Voice Input Dev"
	}
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(dataRoot, dataName))
	// Opt-in native dictionary E2E uses a separate data root and no hotkeys.
	if fixture := os.Getenv("VOICE_INPUT_DICTIONARY_ACCEPTANCE"); debug && filepath.IsAbs(fixture) && os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") != "" {
		mygo.App.SetPath(mygo.PathUserData, filepath.Join(fixture, "data"))
	}
	// An ordinary app: it has a Dock icon and takes part in app switching.
	// The menu bar item and the trigger key work whether or not its window
	// is open.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	if os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") == "" && !mygo.App.RequestSingleInstanceLock() {
		return // the instance already running keeps the trigger key
	}
	mygo.App.OnWindowAllClosed(func() {}) // closing the window leaves dictation running
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.updates.committing && !a.updates.restarting {
			e.PreventDefault()
			a.main.goTo("home")
			a.main.show()
			return
		}
		a.quitting = true
	})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) ready() {
	macos.OnMain = mygo.RunOnMain

	// History, audio, dictionary and settings live in the app's data
	// directory (~/Library/Application Support/…), never in the project.
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	if err != nil {
		setLang(langFor(mygo.App.Locale()))
		mygo.Dialog.Error("Voicemore", T("errDataDir", err.Error()))
		mygo.App.Quit()
		return
	}
	// The load errors are kept as keys: the window shows them in the
	// language current when it draws.
	var loadErrs [][2]string
	note := func(what string, err error) {
		if err != nil {
			loadErrs = append(loadErrs, [2]string{what, err.Error()})
		}
	}
	a.settings, err = config.Open(dir)
	note("fileSettings", err)
	if a.settings.LoadError() != nil {
		setLang(langFor(mygo.App.Locale()))
		mygo.Dialog.Error("Voicemore", T("settingsUnreadable", err.Error()))
		mygo.App.Quit()
		return
	}
	if err == nil {
		note("fileSettings", a.initializeAutoSendApplications())
	}
	a.vault, err = secrets.Open(dir)
	note("fileSecrets", err)
	a.history, err = store.Open(dir)
	note("fileHistory", err)
	a.dict, err = dict.Open(dir)
	note("fileDict", err)
	a.applyLanguage()
	appearance = a.settings.Get().Appearance // light, dark, or the system's (theme.go)
	// The dev-only subtitle fixture opens its window once the app is up.
	defer a.openSubtitleFixture()

	a.ctrl = &session.Controller{Settings: a.settings, History: a.history, Dict: a.dict}
	a.ctrl.CopyText = func(s string) { mygo.Clipboard.WriteText(s) }
	a.ctrl.Tr, a.ctrl.Explain, a.ctrl.CleanupKey = T, explain, a.key
	// The defaults of Settings → Models decide where a dictation connects.
	a.ctrl.PrepareASR = func(ctx context.Context, s config.Settings) (func(), error) {
		if s.ASRAssign.Provider == providers.Local && s.ASRManaged {
			return a.localASR.Acquire(ctx)
		}
		return func() {}, nil
	}
	a.ctrl.ResolveASR = func(s config.Settings) (asr.Dialer, error) {
		if s.ASRAssign.Provider == providers.Local && s.ASRManaged {
			state := a.localASR.Snapshot()
			if state.Phase != "ready" {
				return nil, fmt.Errorf("本地语音模型尚未就绪，请到设置 → 模型启动：%s", state.Detail)
			}
			s.ASRURL = localasr.URL
		}
		return providers.ASR(s, a.secret)
	}
	a.ctrl.ResolveCleanup = func(s config.Settings) (cleanup.Options, error) { return providers.CleanupOptions(s, a.secret) }

	a.tray, err = mygo.NewTray(mygo.TrayOptions{Icon: trayIcon(), IconIsTemplate: true, ToolTip: "Voicemore"})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	executable, _ := os.Executable()
	archive := filepath.Join(filepath.Dir(executable), "../Resources/local-asr/runtime.tar.gz")
	compact := filepath.Join(filepath.Dir(executable), "../Resources/local-asr/runtime.tar.bz2")
	if _, err := os.Stat(compact); err == nil {
		archive = compact
	}
	a.localASR = localasr.New(filepath.Join(dir, "local-asr"), archive)
	a.overlay = newOverlay(a)
	a.sendPreview = newSendPreview(a)
	a.ctrl.SendPreview = a.sendPreview.wait
	a.startSendPreviewAcceptance()
	a.main = newMainWindow(a)
	a.prefs = newPrefsWindow(a)
	a.onboard = newOnboardWindow(a)
	a.localASR.Changed = a.refreshAll
	// A click on the Dock icon, or reopening the app, brings the window
	// back; a second launch does the same in the instance already running.
	mygo.App.OnActivate(func(bool) { a.main.show() })
	mygo.App.OnSecondInstance(func([]string, string) { mygo.RunOnMain(a.main.show) })
	a.ctrl.OnChange = func() {
		noteDictationPhase(a.ctrl.View().Phase) // live subtitles yield optional work to dictation
		a.overlay.sync()
		a.main.refresh()
		a.onboard.refresh() // its first-dictation step follows the dictation
	}
	// Local file reads finish before the user can edit credentials.
	a.loadKey()
	a.loadSecrets()
	a.ctrl.OnHistory = func() {
		a.main.refresh()
		a.onboard.refresh()
	}
	a.ctrl.OnPersistenceError = func(err error) {
		mygo.RunOnMain(func() {
			if !a.quitting {
				a.main.say("historySaveFailed", err.Error())
				a.main.show()
			}
		})
	}
	a.setMenu()
	// Without the Dock icon the menu bar item is the way in.
	if !a.settings.Get().ShowInDock {
		macos.SetDockIcon(false)
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.ctrl.Cancel()
		a.localASR.Close()
		if a.stopHotkey != nil {
			a.stopHotkey()
		}
		a.tray.Destroy()
	})

	a.startUpdates(dir)
	a.purge()
	go func() {
		for range time.Tick(6 * time.Hour) {
			a.purge()
		}
	}()

	// Always show the control window on launch, including first launch.
	// A hidden tray-only app otherwise looks as if it failed to start.
	//
	// A new installation opens on the first-run guide instead. Whoever
	// already uses the app is never sent through it: settings saved by an
	// earlier version are marked as having left it (config.migrate), and a
	// history that is not empty says the same when no settings were saved.
	if ob := a.settings.Get().Onboarding; !ob.Left && len(a.history.List("")) > 0 {
		s := a.settings.Get()
		s.Onboarding.Left = true
		if err := a.settings.Set(s); err != nil {
			log.Printf("settings: %v", err)
		}
	}
	if a.settings.Get().Onboarding.Left {
		a.main.show()
	} else {
		a.onboard.open()
	}
	log.Printf("ready: data=%s", dir)
	a.bindTrigger()
	a.ensureCleanupReady()
	if len(loadErrs) > 0 || a.triggerErr != "" {
		// Something needs the user: say it in the window.
		a.main.loadErrs = loadErrs
		a.main.show()
	}
}

// applyLanguage sets the language of the settings, or the system's when
// none was chosen.
func (a *app) applyLanguage() {
	id := a.settings.Get().Language
	if id == "" {
		id = mygo.App.Locale()
	}
	setLang(langFor(id))
}

// setLanguage saves the user's choice and redraws everything that shows
// text, the tray's menu included.
func (a *app) setLanguage(i int) error {
	s := a.settings.Get()
	s.Language = langIDs[i]
	err := a.settings.Set(s)
	setLang(i)
	a.setMenu()
	a.overlay.sync()
	a.refreshAll()
	return err
}

// ensureCleanupReady checks in the background, including after model edits.
func (a *app) ensureCleanupReady() {
	if providers.ManagedVoice(a.settings.Get()) {
		if a.cleanupChecking {
			a.cleanupCheck++
		}
		a.cleanupReady = providers.Check(providers.KindASR, a.settings.Get(), a.secret) == nil
		a.cleanupChecking = false
		return
	}
	opts, err := providers.CleanupOptions(a.settings.Get(), a.secret)
	if err != nil {
		if a.cleanupReady || a.cleanupChecking {
			a.cleanupCheck++
			a.cleanupChecking = false
			a.cleanupUnavailable()
		}
		return
	}
	if opts == a.cleanupOptions && (a.cleanupChecking || time.Since(a.cleanupChecked) < 15*time.Second) {
		return
	}
	a.checkCleanup(false)
}

func (a *app) setCleanup(on bool) {
	a.cleanupCheck++
	a.cleanupChecking = false
	a.saveCleanup(on)
}

func (a *app) checkCleanup(enable bool) {
	if providers.ManagedVoice(a.settings.Get()) {
		a.ensureCleanupReady()
		return
	}
	a.cleanupCheck++
	request := a.cleanupCheck
	a.cleanupChecking = false
	s := a.settings.Get()
	opts, err := providers.CleanupOptions(s, a.secret)
	if err != nil {
		a.cleanupUnavailable()
		return
	}
	a.cleanupOptions = opts
	a.cleanupReady = false
	a.cleanupChecking = true
	a.refreshAll()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := cleanup.Check(ctx, opts)
		mygo.RunOnMain(func() {
			if request != a.cleanupCheck {
				return
			}
			a.cleanupChecking = false
			a.cleanupChecked = time.Now()
			current, currentErr := providers.CleanupOptions(a.settings.Get(), a.secret)
			if err != nil || !result.Listed || currentErr != nil || current != opts {
				a.cleanupUnavailable()
				return
			}
			a.cleanupReady = true
			if enable {
				a.saveCleanup(true)
			} else {
				a.setMenu()
				a.refreshAll()
			}
		})
	}()
	return
}

func (a *app) cleanupUnavailable() {
	a.cleanupReady = false
	a.setMenu()
	a.refreshAll()
}

func (a *app) saveCleanup(on bool) {
	s := a.settings.Get()
	s.CleanupDefault = on
	if err := a.settings.Set(s); err != nil {
		log.Println("settings:", err)
		return
	}
	a.ctrl.SetCleanupNext(on)
	a.setMenu()
	a.refreshAll()
}

// purge applies the retention periods of the settings.
func (a *app) purge() {
	s := a.settings.Get()
	if !s.SaveAudio && s.TextRetentionDays == 0 {
		return
	}
	if err := a.history.Purge(s.AudioRetentionDays, s.TextRetentionDays, time.Now()); err != nil {
		log.Println("purge:", err)
	}
	a.main.refresh()
}

// bindTrigger registers the trigger key of the settings: a modifier key
// through the event tap, anything else as a global shortcut, which only
// toggles. A failure is kept for the window to show; no other key is
// taken in its place, and no system setting is changed.
func (a *app) bindTrigger() {
	// Native acceptance runs bind no global trigger: send preview, and the
	// subtitle fixture or live runs (subtitles.go), which must not take Fn.
	if os.Getenv("VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE") != "" || subtitlesAcceptance() {
		return
	}
	a.triggerGeneration++
	generation := a.triggerGeneration
	if a.stopHotkey != nil {
		a.stopHotkey()
		a.stopHotkey = nil
	}
	a.triggerErr, a.deviceButtonErr = "", ""
	if a.updates.committing || a.shortcutsSuspended {
		return
	}
	stops := []func(){}
	a.stopHotkey = func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}
	s := a.settings.Get()
	register := func(key string, translation bool) {
		if key == "" || (a.prefs != nil && (a.prefs.deviceWizard || a.prefs.deviceTest)) {
			return
		}
		if err := config.ValidateShortcut(key); err != nil {
			a.triggerErr = T("shortcutUnsafe")
			return
		}
		var stop func()
		var err error
		if macos.UsesNativeShortcut(key) {
			macos.InputMonitoringGranted(true)
			if translation {
				stop, err = macos.StartTranslationHotkey(key, func(combo bool) {
					if !combo {
						mygo.RunOnMain(func() {
							if generation == a.triggerGeneration {
								a.translationTrigger()
							}
						})
					}
				})
			} else {
				stop, err = macos.StartHotkey(key, func() {
					mygo.RunOnMain(func() {
						if generation == a.triggerGeneration {
							a.triggerDown()
						}
					})
				}, func(combo bool) {
					mygo.RunOnMain(func() {
						if generation == a.triggerGeneration {
							a.triggerUp(combo)
						}
					})
				}, a.cancelInput)
			}
		} else {
			fn := func() { a.triggerUp(false) }
			if translation {
				fn = a.translationTrigger
			}
			callback := fn
			err = mygo.GlobalShortcut.Register(key, func() {
				if generation == a.triggerGeneration {
					callback()
				}
			})
			if err == nil {
				stop = func() { mygo.GlobalShortcut.Unregister(key) }
			}
		}
		if err != nil {
			a.triggerErr = T("errShortcut", key, err.Error())
		} else if stop != nil {
			stops = append(stops, stop)
		}
	}
	register(s.TriggerKey, false)
	if !macos.UsesNativeShortcut(s.TriggerKey) && !(a.prefs != nil && (a.prefs.deviceWizard || a.prefs.deviceTest)) {
		if stop, err := macos.StartHotkey("", nil, nil, a.cancelInput); err == nil {
			stops = append(stops, stop)
		} else {
			a.triggerErr = err.Error()
		}
	}
	if !sameShortcut(s.TranslationKey, s.TriggerKey) {
		register(s.TranslationKey, true)
	} else if s.TranslationKey != "" {
		a.triggerErr = T("shortcutTaken")
	}

	bindings := s.AllDeviceButtons()
	capture := ""
	if a.prefs != nil && (a.prefs.deviceWizard || a.prefs.deviceTest) {
		bindings = nil
		if a.prefs.deviceStep == 1 || a.prefs.deviceTest {
			capture = a.prefs.deviceSelected.ID
		}
	}
	stopDevices, err := macos.StartDeviceButtonMonitor(bindings, capture, func(event macos.DeviceButtonEvent) {
		mygo.RunOnMain(func() {
			if generation != a.triggerGeneration {
				return
			}
			if p := a.prefs; p != nil && p.deviceTest {
				if !event.Unsupported && event.Device.ID == p.deviceSelected.ID && event.Page == p.devicePage && event.Usage == p.deviceUsage && time.Now().Before(p.deviceDeadline) {
					p.deviceTestResult = "deviceTestReceived"
					p.stopDeviceTest()
					p.refresh()
				}
				return
			}
			if p := a.prefs; p != nil && p.deviceWizard {
				if p.deviceStep == 1 && (event.Device.ID == p.deviceSelected.ID || p.deviceSelected.SamePhysicalDevice(event.Device)) && time.Now().Before(p.deviceDeadline) {
					if event.Unsupported {
						p.deviceCaptured = false
						p.status = func() string { return T("deviceUnsupportedKey") }
						p.refresh()
						return
					}
					p.deviceSelected = event.Device
					if p.deviceTest && (event.Page != p.devicePage || event.Usage != p.deviceUsage) {
						return
					}
					p.status = nil
					p.devicePage, p.deviceUsage = event.Page, event.Usage
					p.deviceCaptured = true
					p.refresh()
				}
				return
			}
			if a.shortcutsSuspended || a.updates.committing {
				return
			}
			for _, b := range a.settings.Get().AllDeviceButtons() {
				if !b.Enabled || !b.Device.Matches(event.Device) || b.Page != event.Page || b.Usage != event.Usage {
					continue
				}
				if b.Action == "translation" {
					a.translationTrigger()
				} else if a.onboard != nil && a.onboard.visible() {
					a.triggerUp(false)
				} else {
					go a.ctrl.Toggle()
				}
				break
			}
		})
	})
	if err != nil {
		a.deviceButtonErr = err.Error()
		log.Printf("trigger: %v", err)
	} else {
		stops = append(stops, stopDevices)
	}

}

func (a *app) translationTrigger() {
	mygo.RunOnMain(func() {
		if a.shortcutsSuspended || a.updates.committing || (a.onboard != nil && a.onboard.visible()) {
			return
		}
		go a.ctrl.ToggleTranslation()
	})
}

// setMenu builds the tray's menu, again whenever what it shows changes.
func (a *app) setMenu() {
	a.tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: T("trayToggle"), Click: func(*mygo.MenuItem, *mygo.Window) {
			if !a.updates.committing {
				go a.ctrl.Toggle()
			}
		}},
		{Label: T("trayCleanup"), Type: mygo.MenuItemCheckbox, Checked: a.ctrl.CleanupNext(), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.setCleanup(!a.ctrl.CleanupNext())
		}},
		mygo.Separator(),
		{Label: T("updateCheck"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.main.goTo("home")
			a.main.updateOpen = true
			a.main.show()
			a.checkUpdates()
		}},
		{Label: T("trayOpen"), Click: func(*mygo.MenuItem, *mygo.Window) { a.main.show() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}))
}
