package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/egoist/mygo"

	"voiceinput/internal/cleanup"
	"voiceinput/internal/config"
	"voiceinput/internal/providers"
	"voiceinput/internal/secrets"
	"voiceinput/internal/session"
	"voiceinput/internal/subtitles"
)

// Live subtitles' runtime: where the helper, engines and logs are, which
// text service translates, and the bounded translation queue. The
// development switches below take effect in Voicemore Dev only.
const (
	subtitlesLiveEnv           = "VOICE_INPUT_SUBTITLES_LIVE"            // "1": open on launch and start capturing real system audio
	subtitlesLogEnv            = "VOICE_INPUT_SUBTITLES_LOG"             // absolute path: append the JSONL event log there
	subtitlesProductionTextEnv = "VOICE_INPUT_SUBTITLES_PRODUCTION_TEXT" // "1": translate with the installed app's text service
	subtitlesTapEnv            = "VOICE_INPUT_SUBTITLES_TAP"             // absolute path of a VoicemoreAudioTap to use instead of the bundled one
	subtitlesQwenRootEnv       = "VOICE_INPUT_SUBTITLES_QWEN_ROOT"       // installed local-asr directory, read-only
	subtitlesSherpaRootEnv     = "VOICE_INPUT_SUBTITLES_SHERPA_ROOT"     // directory with venv/ and model/
	subtitlesCorrectionEnv     = "VOICE_INPUT_SUBTITLES_CORRECTION"      // "0": no background Qwen correction of sherpa, for paired replays
	subtitlesSpeakersEnv       = "VOICE_INPUT_SUBTITLES_SPEAKERS"        // "0": no speaker labels, for paired replays
	subtitlesSpeakerRootEnv    = "VOICE_INPUT_SUBTITLES_SPEAKER_ROOT"    // directory with segmentation.onnx and embedding.onnx
	subtitlesDataEnv           = "VOICE_INPUT_SUBTITLES_DATA"            // directory of the saved session slot, for isolated acceptance
)

const (
	subtitleTranslateT = 30 * time.Second
)

// dictationGate is whether this app's microphone dictation is busy, from
// the controller's phase. Live subtitles only read it: optional work
// (Qwen correction, its retranslation and speaker labels) yields while it is busy. It sees
// only this process: another app's dictation is not in it.
var (
	dictationGate  = subtitles.NewGate()
	dictationPhase atomic.Int32 // the last phase noted, for the log
)

// noteDictationPhase is called with every dictation view change. It never
// blocks and changes nothing of the dictation.
func noteDictationPhase(p session.Phase) {
	dictationPhase.Store(int32(p))
	switch p {
	case session.Preparing, session.Recording, session.Finalizing, session.Cleaning, session.Inserting:
		dictationGate.Set(true)
	default:
		dictationGate.Set(false)
	}
}

func devBuild() bool { return mygo.App.Name() == "Voicemore Dev" }

// devPath is an absolute path a development switch names, in Dev only.
func devPath(env string) string {
	if p := os.Getenv(env); devBuild() && filepath.IsAbs(p) {
		return p
	}
	return ""
}

func subtitlesLiveOnLaunch() bool { return devBuild() && os.Getenv(subtitlesLiveEnv) == "1" }

// subtitleConfig is where a session's helper, engines and files are.
func (a *app) subtitleConfig(s config.Subtitles, log *subtitleLog) subtitles.Config {
	data, _ := mygo.App.Path(mygo.PathUserData)
	cfg := subtitles.Config{
		Engine:     s.Engine,
		Language:   s.Language,
		TapPath:    devPath(subtitlesTapEnv),
		QwenRoot:   devPath(subtitlesQwenRootEnv),
		SherpaRoot: devPath(subtitlesSherpaRootEnv),
		// Speakers is set per session by the window (subtitles_speakers.go).
		SpeakerRoot: devPath(subtitlesSpeakerRootEnv),
		WorkDir:    filepath.Join(data, "subtitles", "runtime"),
		Correction: !(devBuild() && os.Getenv(subtitlesCorrectionEnv) == "0"),
		Gate:       dictationGate,
		Log:        log.emit,
	}
	os.MkdirAll(cfg.WorkDir, 0o700)
	if cfg.TapPath == "" {
		if exe, err := os.Executable(); err == nil {
			cfg.TapPath = filepath.Join(filepath.Dir(exe), "..", "Resources", "subtitles", "VoicemoreAudioTap")
		}
	}
	if cfg.SherpaRoot == "" {
		cfg.SherpaRoot = filepath.Join(data, "subtitles", "sherpa")
	}
	if cfg.SpeakerRoot == "" {
		cfg.SpeakerRoot = filepath.Join(data, "subtitles", "speakers")
	}
	if cfg.QwenRoot == "" {
		// The managed local model of this app, else the one the installed app manages.
		if a.localASR != nil && a.localASR.Root != "" {
			if _, err := os.Stat(filepath.Join(a.localASR.Root, "model")); err == nil {
				cfg.QwenRoot = a.localASR.Root
			}
		}
		if cfg.QwenRoot == "" {
			root, _ := mygo.App.Path(mygo.PathAppData)
			cfg.QwenRoot = filepath.Join(root, "Voice Input", "local-asr")
		}
	}
	return cfg
}

// subtitleLog is the opt-in JSONL event log of development acceptance: PCM
// counts, recognition revisions and translation timing. It never holds
// audio or credentials.
type subtitleLog struct {
	mu    sync.Mutex
	f     *os.File
	start time.Time
}

func openSubtitleLog() *subtitleLog {
	p := devPath(subtitlesLogEnv)
	if p == "" {
		return nil
	}
	os.MkdirAll(filepath.Dir(p), 0o700)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return &subtitleLog{f: f, start: time.Now()}
}

func (l *subtitleLog) emit(event string, fields map[string]any) {
	if l == nil {
		return
	}
	rec := map[string]any{}
	for k, v := range fields {
		rec[k] = v
	}
	now := time.Now()
	rec["event"], rec["t"], rec["t_ms"] = event, now.Format(time.RFC3339Nano), now.Sub(l.start).Milliseconds()
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.f.Write(append(b, '\n'))
	l.mu.Unlock()
}

// standaloneTextOptions is the text model for subtitle translation. Dictation's
// managed voice does its cleanup inside its audio task, which is why
// CleanupOptions refuses it; subtitles translate text on their own, so the
// text model resolves as with any other recognition provider. s is a copy:
// nothing is saved, and dictation routing is unchanged.
func standaloneTextOptions(s config.Settings, sec providers.Secrets) (cleanup.Options, error) {
	if providers.ManagedVoice(s) {
		s.ASRAssign = config.Assignment{Provider: providers.Local}
	}
	o, err := providers.CleanupOptions(s, sec)
	o.PolishRules = ""
	return o, err
}

// subtitleTextOptions resolves the text service, and says which route it is.
func (a *app) subtitleTextOptions() (cleanup.Options, string, error) {
	if devBuild() && os.Getenv(subtitlesProductionTextEnv) == "1" {
		o, err := productionTextOptions()
		return o, "production", err
	}
	o, err := standaloneTextOptions(a.settings.Get(), a.secret)
	return o, "settings", err
}

// productionTextOptions reads the installed app's text settings and
// credential in memory, for development acceptance only. Its files are read,
// never written: the credential store is opened only when it already exists.
func productionTextOptions() (cleanup.Options, error) {
	root, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		return cleanup.Options{}, err
	}
	dir := filepath.Join(root, "Voice Input")
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return cleanup.Options{}, err
	}
	s := config.Defaults()
	if err := json.Unmarshal(b, &s); err != nil {
		return cleanup.Options{}, err
	}
	sec := func(string) string { return "" }
	_, e1 := os.Lstat(filepath.Join(dir, "credentials", "master.key"))
	_, e2 := os.Lstat(filepath.Join(dir, "credentials", "secrets.enc"))
	if e1 == nil && e2 == nil {
		if vault, err := secrets.Open(dir); err == nil {
			sec = func(account string) string { v, _ := vault.Get(account); return v }
		}
	}
	return standaloneTextOptions(s, sec)
}

func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil {
		return u.Host
	}
	return ""
}

// subtitleTargetName is the language Translate is asked for.
func subtitleTargetName(id string) string {
	switch id {
	case "zh-Hans":
		return "Simplified Chinese (简体中文)"
	case "zh-Hant":
		return "Traditional Chinese (繁體中文)"
	case "en":
		return "English"
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	case "fr":
		return "French"
	case "de":
		return "German"
	case "es":
		return "Spanish"
	}
	return id
}

// sameLanguage reports a segment already in the target language, which then
// shows once. Only English and Simplified Chinese can match a recognizer's
// output: Traditional Chinese is a conversion, so it is always translated.
func sameLanguage(source, target, text string) bool {
	var lang string
	switch target {
	case "en":
		lang = "en"
	case "zh-Hans":
		lang = "zh"
	default:
		return false
	}
	if source == "zh" || source == "en" {
		return source == lang
	}
	// Automatic or bilingual recognition: the segment's script decides.
	han, latin := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	if lang == "zh" {
		return han > 0 && han*2 >= latin
	}
	return han == 0 && latin > 0
}

// subtitleSource is what the recognizer hears, for sameLanguage and the tag.
func subtitleSource(s config.Subtitles) string {
	if s.Engine == config.SubtitleSherpa {
		return "bilingual"
	}
	return s.Language
}

func subtitleSourceTag(src string) string {
	switch src {
	case "zh":
		return "ZH"
	case "en":
		return "EN"
	case "bilingual":
		return "ZH·EN"
	}
	return "AUTO"
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// subtitleErrCode is the interface code of a session failure.
func subtitleErrCode(err error) string {
	var e *subtitles.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return subtitles.ErrEngineFailed
}
