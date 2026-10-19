package main

import "voiceinput/internal/macos"

// Called on the UI thread. A failed or suspended binding must not claim ownership.
func (o *onboardWindow) fnOwnershipHint() string {
	s := o.a.settings.Get()
	if s.Onboarding.Step != 2 || s.TriggerKey != macos.KeyFn || s.FnOwnershipNoticeSeen ||
		!o.visible() || o.a.triggerErr != "" || o.a.shortcutsSuspended || o.a.stopHotkey == nil {
		return ""
	}
	return T("fnOwnershipHint")
}

func init() {
	texts["fnOwnershipHint"] = [3]string{
		"Fn combinations still work. Its usual action returns when you quit.",
		"Fn 組合鍵不受影響，退出後恢復原功能。",
		"Fn 组合键不受影响，退出后恢复原功能。",
	}
}
