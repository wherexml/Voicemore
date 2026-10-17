package config

// Speech language of the app-managed local ASR worker. The empty value is
// Chinese, which the worker always used: saved settings without the field
// keep recognising exactly as before. Custom and cloud providers ignore it.
const (
	ASRLanguageChinese = ""
	ASRLanguageAuto    = "auto"
	ASRLanguageEnglish = "en"
)

// LocalASRLanguage is the worker's value of a setting: "zh", "auto" or "en".
// Anything unknown is Chinese, the earlier fixed behaviour.
func LocalASRLanguage(v string) string {
	switch v {
	case ASRLanguageAuto, ASRLanguageEnglish:
		return v
	}
	return "zh"
}

// PrivateApp is an application whose dictations still insert, but leave no
// history record, recording or dictionary learning. Matched by Bundle ID.
type PrivateApp struct {
	BundleID string `json:"bundleId"`
	AppName  string `json:"appName"`
}

// PrivateBundle says dictations whose start or insertion app has this
// Bundle ID are private. An unknown (empty) Bundle ID matches nothing.
func (s Settings) PrivateBundle(bundle string) bool {
	if bundle == "" {
		return false
	}
	for _, p := range s.PrivateApps {
		if p.BundleID == bundle {
			return true
		}
	}
	return false
}
