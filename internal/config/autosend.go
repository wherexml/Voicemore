package config

// AutoSend is resolved against the final local insertion target, never the
// application at recording start. Unknown shortcuts fail closed.
type AutoSend struct {
	ApplicationsInitialized bool          `json:"applicationsInitialized,omitempty"`
	Enabled                 bool          `json:"enabled"`
	Default                 string        `json:"default"`
	Apps                    []AutoSendApp `json:"apps,omitempty"`
}
type AutoSendApp struct {
	BundleID string `json:"bundleId"`
	AppName  string `json:"appName"`
	Shortcut string `json:"shortcut"`
}

func (a AutoSend) Clone() AutoSend { a.Apps = append([]AutoSendApp(nil), a.Apps...); return a }
func (a AutoSend) Shortcut(bundle string) string {
	if !a.Enabled || bundle == "" {
		return ""
	}
	key := a.Default
	for _, r := range a.Apps {
		if r.BundleID == bundle {
			if r.Shortcut != "inherit" && r.Shortcut != "" {
				key = r.Shortcut
			}
			break
		}
	}
	switch key {
	case "enter", "cmd-enter", "ctrl-enter":
		return key
	}
	return ""
}
func DefaultAutoSend() AutoSend {
	return AutoSend{Enabled: true, Default: "enter", Apps: []AutoSendApp{
		{BundleID: "com.openai.chat", AppName: "ChatGPT", Shortcut: "inherit"},
		{BundleID: "com.tencent.xinWeChat", AppName: "WeChat", Shortcut: "inherit"},
		{BundleID: "com.electron.lark", AppName: "Feishu", Shortcut: "inherit"},
	}}
}
