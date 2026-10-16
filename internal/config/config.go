// Package config holds the user's settings, saved as JSON in the app's data
// directory. Defaults follow the "suggested, not confirmed" table of
// docs/requirements.md §9 and stay configurable.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"voiceinput/internal/textstyle"
)

const (
	TriggerToggle = "toggle"
	TriggerHold   = "hold"
)

// SchemaVersion is the version Open migrates settings to.
const SchemaVersion = 5

// Defaults of the two independent model services. Speech recognition is
// the existing ASR service with its own streaming protocol; cleanup is an
// OpenAI-compatible endpoint, by default the local oMLX server. Cleanup
// stays opt-in: these only say where it would go once it is turned on.
const (
	DefaultASRURL       = "http://127.0.0.1:48131"
	DefaultASRModel     = "Qwen3-ASR-1.7B-8bit"
	DefaultCleanupURL   = "http://127.0.0.1:48000/v1"
	DefaultCleanupModel = "Qwen3.6-35B-A3B-MLX-4bit"
)

// Assignment is the default model of a kind: the provider it comes from and,
// for providers with several models, which one. Provider "local" is the
// existing local service of that kind, whose model is configured with it;
// an empty Provider means nothing is chosen.
type Assignment struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// CustomProvider is a provider the user added. Its API key, if any, is in
// the encrypted local credential store, never here.
type CustomProvider struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // "asr" or "cleanup"
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Models  []string `json:"models"`
	Enabled bool     `json:"enabled"`
}

// Providers is what is remembered about the cloud and custom providers,
// without any credential.
type Providers struct {
	Enabled          map[string]bool  `json:"enabled,omitempty"`
	VolcMode         string           `json:"volcMode,omitempty"`
	VolcResource     string           `json:"volcResource,omitempty"` // the resource ID; empty is the hourly one
	AliyunBase       string           `json:"aliyunBase,omitempty"`   // wss://…/api-ws/v1; empty is the default
	DeepSeekEndpoint string           `json:"deepseekEndpoint,omitempty"`
	DeepSeekModels   []string         `json:"deepseekModels,omitempty"` // model IDs the user added
	Custom           []CustomProvider `json:"custom,omitempty"`
}

// Clone copies the maps and slices, so that a copy of the settings can be
// changed without touching the stored ones.
func (p Providers) Clone() Providers {
	out := p
	out.Enabled = map[string]bool{}
	for k, v := range p.Enabled {
		out.Enabled[k] = v
	}
	out.DeepSeekModels = append([]string(nil), p.DeepSeekModels...)
	out.Custom = make([]CustomProvider, len(p.Custom))
	for i, c := range p.Custom {
		c.Models = append([]string(nil), c.Models...)
		out.Custom[i] = c
	}
	return out
}

// Onboarding is where the first-run guide stands. Left says the guide is
// not shown at launch any more: the user finished it or chose "Later", or
// the settings are those of an installation older than the guide.
type Onboarding struct {
	Left      bool `json:"left"`
	Step      int  `json:"step,omitempty"`
	Seen      bool `json:"seen,omitempty"`      // the introduction was read
	Practiced bool `json:"practiced,omitempty"` // a first dictation was inserted
	Cloud     bool `json:"cloud,omitempty"`     // the speech step shows the cloud branch
}

type Settings struct {
	TextStyle     textstyle.Options `json:"textStyle"`
	SchemaVersion int               `json:"schemaVersion"`

	// ASRAssign and CleanupAssign are the two default models. They are
	// independent, and both start on the local services. Providers holds
	// the other providers' settings.
	ASRAssign     Assignment `json:"asrAssign"`
	CleanupAssign Assignment `json:"cleanupAssign"`
	Providers     Providers  `json:"providers"`

	// Appearance is "light" or "dark"; empty follows the system.
	Appearance string `json:"appearance,omitempty"`
	// HistoryDetailsOpen is whether "Details" of a record in History is
	// left open: the user's last choice, kept across records and launches.
	HistoryDetailsOpen bool `json:"historyDetailsOpen,omitempty"`
	// Onboarding is the progress of the first-run guide.
	Onboarding Onboarding `json:"onboarding"`

	// Language is "en", "zh-Hant" or "zh-Hans"; empty follows the system.
	Language string `json:"language"`

	// ASR is the existing local service (docs/asr-integration.md).
	// ASRModel is the model that service last reported, for display: the
	// service loads its model itself, and nothing here changes it.
	ASRModel           string `json:"asrModel"`
	ASRManaged         bool   `json:"asrManaged,omitempty"`
	ASRURL             string `json:"asrUrl"`
	FinalizeTimeoutSec int    `json:"finalizeTimeoutSec"`

	// TriggerKey is "Fn", "RightOption", "RightCommand", "RightControl" or a
	// MyGo accelerator such as "Ctrl+Alt+Space" (toggle only).
	TriggerKey  string `json:"triggerKey"`
	TriggerMode string `json:"triggerMode"`

	// Cleanup is off by default. CleanupURL is the base URL of an
	// OpenAI-compatible API ("…/v1"); requests go to its /chat/completions
	// and /models.
	CleanupDefault bool   `json:"cleanupDefault"`
	CleanupURL     string `json:"cleanupUrl"`
	CleanupModel   string `json:"cleanupModel"`
	// CleanupAPIKey is the legacy plaintext key. New keys are never
	// written here: they go to the app's encrypted local credential store item, and this field is
	// emptied once a key found here has been stored there.
	CleanupAPIKey     string `json:"cleanupApiKey,omitempty"`
	CleanupTimeoutSec int    `json:"cleanupTimeoutSec"`

	SaveAudio          bool `json:"saveAudio"`
	AudioRetentionDays int  `json:"audioRetentionDays"`
	// TextRetentionDays 0 keeps text records until they are deleted.
	TextRetentionDays int  `json:"textRetentionDays"`
	KeepCancelled     bool `json:"keepCancelled"`

	// OverlayBottomOffset is the gap between the capsule and the bottom of
	// the work area of the display the recording started on.
	OverlayBottomOffset int `json:"overlayBottomOffset"`
	// CapsuleStyle is how the dictation capsule looks: empty is the indigo
	// capsule (CapsuleV2), CapsuleClassic the earlier black one. A change
	// applies from the next dictation.
	CapsuleStyle string `json:"capsuleStyle,omitempty"`

	ClipboardRestoreDelayMs int           `json:"clipboardRestoreDelayMs"`
	RemoteInputs            []RemoteInput `json:"remoteInputs,omitempty"`
	AutoSend                AutoSend      `json:"autoSend"`

	// ObserveEdits reads the target field back after an insertion to
	// propose dictionary candidates. Off by default: it reads text of
	// another app, even if only the field just written to.
	ObserveEdits   bool `json:"observeEdits"`
	ObserveSeconds int  `json:"observeSeconds"`

	// SeedPath is the local hotword file the user may import explicitly.
	SeedPath string `json:"seedPath"`

	// InputDevice is the UID of the chosen microphone; empty follows the
	// system's default input. InputDeviceName is its name when chosen, to
	// show while it is disconnected: recording then uses the default, and
	// returns to it once it is connected again.
	InputDevice     string `json:"inputDevice,omitempty"`
	InputDeviceName string `json:"inputDeviceName,omitempty"`
	// InteractionSounds plays the start and stop cues of a recording.
	InteractionSounds bool `json:"interactionSounds"`
	// MuteOutput mutes the system's audio output while recording and puts
	// it back when the recording ends, however it ends.
	MuteOutput bool `json:"muteOutput"`
	// ShowInDock shows the app in the Dock and the app switcher; without
	// it the app lives in the menu bar only. Launch at login is not kept
	// here: macOS holds it (System Settings → Login Items).
	ShowInDock bool `json:"showInDock"`
}

const (
	CapsuleV2      = ""
	CapsuleClassic = "classic"
)

func Defaults() Settings {
	return Settings{
		TextStyle:               textstyle.Defaults(),
		SchemaVersion:           SchemaVersion,
		ASRAssign:               Assignment{Provider: "local"},
		CleanupAssign:           Assignment{Provider: "local"},
		ASRURL:                  DefaultASRURL,
		ASRModel:                DefaultASRModel,
		FinalizeTimeoutSec:      60,
		TriggerKey:              "Fn",
		TriggerMode:             TriggerToggle,
		CleanupDefault:          false,
		CleanupTimeoutSec:       30,
		SaveAudio:               true,
		AudioRetentionDays:      30,
		OverlayBottomOffset:     12,
		ClipboardRestoreDelayMs: 600,
		AutoSend:                DefaultAutoSend(),
		ObserveEdits:            false,
		ObserveSeconds:          30,
		SeedPath:                "",
		InteractionSounds:       true,
		MuteOutput:              true,
		ShowInDock:              true,
	}
}

// Store guards the settings and their file.
type Store struct {
	mu   sync.Mutex
	path string
	s    Settings
}

func Open(dir string) (*Store, error) {
	st := &Store{path: filepath.Join(dir, "settings.json"), s: Defaults()}
	b, err := os.ReadFile(st.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	// Unknown or missing fields keep their defaults.
	st.s.SchemaVersion = 0 // a file without the field is of the first version
	if err := json.Unmarshal(b, &st.s); err != nil {
		st.s = Defaults()
		return st, err
	}
	st.s = migrate(st.s)
	return st, nil
}

// migrate brings settings of an earlier version up to date, in memory; the
// file is rewritten when the settings are next saved.
func migrate(s Settings) Settings {
	if s.SchemaVersion >= SchemaVersion {
		return s
	}
	// Version 5 added the first-run guide. Settings saved by an earlier
	// version belong to someone who already uses the app: the guide is not
	// shown to them at launch, and stays available from Settings → General.
	if s.SchemaVersion < 5 {
		s.Onboarding.Left = true
		// Volcengine's "Agent Plan" access mode never had a protocol and is
		// no longer offered: such a setting falls back to the API-key mode.
		// Its stored key is left where it is. The legacy mode (App ID and
		// token) keeps working for those who use it.
		if s.Providers.VolcMode == "agent" {
			s.Providers.VolcMode = ""
		}
	}
	// Version 4 places the capsule just above the Dock. Preserve custom gaps.
	if s.SchemaVersion < 4 && s.OverlayBottomOffset == 96 {
		s.OverlayBottomOffset = 12
	}
	// Versions 1 and 2 had no providers: the defaults of a file without
	// the fields are the local services, as Defaults sets them, so the
	// existing URLs and models keep serving exactly as before.
	// Version 1 stored the full chat-completions URL, and saved an empty
	// one when cleanup had never been set up.
	s.CleanupURL = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(s.CleanupURL), "/"), "/chat/completions")
	if s.ASRModel == "" {
		s.ASRModel = DefaultASRModel
	}
	s.SchemaVersion = SchemaVersion
	return s
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.s
	s.Providers = s.Providers.Clone()
	s.RemoteInputs = append([]RemoteInput(nil), s.RemoteInputs...)
	s.AutoSend = s.AutoSend.Clone()
	return s
}

func (st *Store) Set(s Settings) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	s.RemoteInputs = append([]RemoteInput(nil), s.RemoteInputs...)
	s.AutoSend = s.AutoSend.Clone()
	st.s = s
	return WriteJSON(st.path, s)
}

// WriteJSON replaces a file atomically, so a crash never leaves half of it.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
