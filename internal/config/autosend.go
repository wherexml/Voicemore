package config

// AutoSend is resolved against the final local insertion target, never the
// application at recording start. Unknown shortcuts fail closed.
type AutoSend struct {
	AICatalogVersion        int           `json:"aiCatalogVersion,omitempty"`
	ApplicationsInitialized bool          `json:"applicationsInitialized,omitempty"`
	DelayMs                 *int          `json:"delayMs,omitempty"`
	Enabled                 bool          `json:"enabled"`
	Default                 string        `json:"default"`
	Apps                    []AutoSendApp `json:"apps,omitempty"`
}
type AutoSendApp struct {
	BundleID string `json:"bundleId"`
	AppName  string `json:"appName"`
	Shortcut string `json:"shortcut"`
}

func (a AutoSend) Clone() AutoSend {
	a.Apps = append([]AutoSendApp(nil), a.Apps...)
	if a.DelayMs != nil {
		n := *a.DelayMs
		a.DelayMs = &n
	}
	return a
}
func (a AutoSend) Delay() int {
	if a.DelayMs == nil {
		return 2000
	}
	switch *a.DelayMs {
	case 0, 1000, 2000, 3000, 5000:
		return *a.DelayMs
	}
	return 2000
}
func (a AutoSend) Shortcut(bundle string) string {
	if !a.Enabled || bundle == "" {
		return ""
	}
	key := a.Default
	found := false
	for _, r := range a.Apps {
		if r.BundleID == bundle {
			found = true
			if r.Shortcut != "inherit" && r.Shortcut != "" {
				key = r.Shortcut
			}
			break
		}
	}
	if !found {
		return ""
	}
	switch key {
	case "enter", "cmd-enter", "ctrl-enter":
		return key
	}
	return ""
}

// AIApplicationPresets is an explicit desktop allowlist. Never add a general
// browser or terminal: their Bundle ID cannot identify the active AI surface.
// IDs come from installed Info.plists and Homebrew cask application metadata.
func AIApplicationPresets() []AutoSendApp {
	return []AutoSendApp{
		{"com.openai.chat", "ChatGPT", "inherit"},
		{"com.openai.codex", "Codex", "inherit"},
		{"com.anthropic.claudefordesktop", "Claude", "inherit"},
		{"com.bot.pc.doubao", "豆包", "inherit"},
		{"com.moonshot.kimichat", "Kimi", "inherit"},
		{"com.tencent.yuanbao", "腾讯元宝", "inherit"},
		{"com.deepseek.dsh", "DeepSeek Harness", "inherit"},
		{"ai.perplexity.macv3", "Perplexity", "inherit"},
		{"com.quora.poe.electron", "Poe", "inherit"},
		{"com.todesktop.230313mzl4w4u92", "Cursor", "inherit"},
		{"com.exafunction.windsurf", "Windsurf", "inherit"},
		{"com.google.antigravity", "Antigravity", "inherit"},
		{"dev.zcode.app", "ZCode", "inherit"},
		{"com.kangfenmao.CherryStudio", "Cherry Studio", "inherit"},
		{"xyz.chatboxapp.app", "Chatbox", "inherit"},
		{"ai.elementlabs.lmstudio", "LM Studio", "inherit"},
		{"jan.ai.app", "Jan", "inherit"},
		{"com.lukilabs.craft-agent", "Craft Agents", "inherit"},
	}
}

func IsAIApplication(bundle string) bool {
	for _, app := range AIApplicationPresets() {
		if app.BundleID == bundle {
			return true
		}
	}
	return false
}

// MigrateAIApplications removes legacy unrestricted rules and adds the AI
// catalog once. Subsequent launches preserve explicit removals and overrides.
func (a *AutoSend) MigrateAIApplications() bool {
	changed := false
	apps := make([]AutoSendApp, 0, len(a.Apps))
	seen := map[string]bool{}
	for _, app := range a.Apps {
		if !IsAIApplication(app.BundleID) || seen[app.BundleID] {
			changed = true
			continue
		}
		seen[app.BundleID] = true
		apps = append(apps, app)
	}
	if a.AICatalogVersion < 1 {
		for _, app := range AIApplicationPresets() {
			if !seen[app.BundleID] {
				apps = append(apps, app)
			}
		}
		a.AICatalogVersion = 1
		changed = true
	}
	if changed {
		a.Apps = apps
	}
	return changed
}

func DefaultAutoSend() AutoSend {
	return AutoSend{Enabled: false, Default: "enter", Apps: AIApplicationPresets()}
}
