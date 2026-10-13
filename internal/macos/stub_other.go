//go:build !darwin

package macos

import "errors"

var errUnsupported = errors.New("Voicemore needs macOS")

type Target struct{ AppName, BundleID string }

func CaptureTarget() *Target                { return &Target{} }
func (t *Target) Release()                  {}
func (t *Target) PasteAvailable() bool      { return false }
func (t *Target) HasElement() bool          { return false }
func (t *Target) ReadValue() (string, bool) { return "", false }
func Insert(string, InsertOptions) (InsertResult, error) {
	return InsertResult{}, errUnsupported
}

func StartHotkey(string, func(), func(combo bool), func()) (func(), error) {
	return nil, errUnsupported
}

type Capture struct{}

func StartCapture(func(pcm []byte)) (*Capture, error) { return nil, errUnsupported }
func (c *Capture) Stop()                              {}

func MicrophoneStatus() string          { return "unsupported" }
func RequestMicrophone(done func(bool)) { done(false) }
func AccessibilityTrusted(bool) bool    { return false }
func InputMonitoringGranted(bool) bool  { return false }
func FnKeyConflict() string             { return "" }
func OpenPrivacySettings(pane string)   {}

func PlayRecordingCue(start bool) {}
