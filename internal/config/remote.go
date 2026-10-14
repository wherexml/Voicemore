package config

// RemoteInput is an explicit user-selected client/connection profile. Empty
// WindowTitle applies to every window in the client; no app names are built in.
type RemoteInput struct {
	BundleID    string `json:"bundleId"`
	AppName     string `json:"appName"`
	WindowTitle string `json:"windowTitle,omitempty"`
	Shortcut    string `json:"shortcut"` // "cmd-v" or "ctrl-v"; empty disables the rule
	SyncDelayMs int    `json:"syncDelayMs"`
}
