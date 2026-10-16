//go:build !darwin

package macos

import "errors"

var errUnsupported = errors.New("Voicemore needs macOS")

var ErrNoMicrophone = errors.New("no microphone was found")

func InputDeviceMissing() bool { return false }

type Target struct{ AppName, BundleID string }

func CaptureTarget() *Target                                  { return &Target{} }
func (t *Target) Retain() *Target                             { copy := *t; return &copy }
func (t *Target) StillFocused() bool                          { return false }
func (t *Target) ReplaceValue(string, string) bool            { return false }
func (t *Target) EditableSnapshot(string) (string, int, bool) { return "", 0, false }
func (t *Target) Release()                                    {}
func (t *Target) PasteAvailable() bool                        { return false }
func (t *Target) HasElement() bool                            { return false }
func (t *Target) ReadValue() (string, bool)                   { return "", false }
func Insert(string, InsertOptions) (InsertResult, error) {
	return InsertResult{}, errUnsupported
}

func StartHotkey(string, func(), func(combo bool), func()) (func(), error) {
	return nil, errUnsupported
}

type Capture struct{}

func StartCapture(func(pcm []byte)) (*Capture, error) { return nil, errUnsupported }
func StartCaptureFrom(string, func(pcm []byte)) (*Capture, error) {
	return nil, errUnsupported
}
func (c *Capture) Stop()                {}
func (c *Capture) SilenceOutput() error { return errUnsupported }
func (c *Capture) DeviceGone() bool     { return false }
func WaitRecordingStartCue() error      { return errUnsupported }

func MicrophoneStatus() string          { return "unsupported" }
func RequestMicrophone(done func(bool)) { done(false) }
func AccessibilityTrusted(bool) bool    { return false }
func InputMonitoringGranted(bool) bool  { return false }
func FnKeyConflict() string             { return "" }
func OpenPrivacySettings(pane string)   {}

func PlayRecordingCue(start bool) {}

type InputDevice struct {
	UID, Name string
	Default   bool
}

func InputDevices() []InputDevice { return nil }
func LoginItemStatus() string     { return "unavailable" }
func SetLoginItem(bool) error     { return errUnsupported }
func OpenLoginItemsSettings()     {}
func SetDockIcon(bool)            {}

func RunningApplications() []Application { return nil }

func WatchOutsideClicks(uintptr, func()) func() { return func() {} }

func (t *Target) StillSelected() bool { return false }

func StartCaptureFromWithOutputMute(string, func([]byte), bool) (*Capture, error) {
	return nil, errUnsupported
}
