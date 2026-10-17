package config

// TranslationLanguages is the fixed set supported by dictation settings.
var TranslationLanguages = []struct{ ID, Name, Prompt string }{
	{"en", "English", "English"}, {"hans", "简体中文", "Simplified Chinese"}, {"hant", "繁體中文", "Traditional Chinese"},
	{"ja", "日本語", "Japanese"}, {"ko", "한국어", "Korean"}, {"fr", "Français", "French"}, {"de", "Deutsch", "German"}, {"es", "Español", "Spanish"},
}

func TranslationTarget(id string) string {
	for _, l := range TranslationLanguages {
		if l.ID == id {
			return l.Prompt
		}
	}
	return "English"
}
