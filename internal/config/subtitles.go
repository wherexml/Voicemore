package config

// Subtitles are the settings of the live subtitles
// (docs/public/LIVE-SUBTITLES.md). They are independent of dictation:
// ASRAssign and TranslationLanguage are never read or written through them.
// Zero values stand for the defaults, so a settings file written before this
// field reads as the defaults.
type Subtitles struct {
	// Engine is SubtitleSherpa (streaming, the default) or SubtitleQwen
	// (accuracy first, final every 8–9 s).
	Engine string `json:"engine,omitempty"`
	// Language is the recognition language of the local Qwen worker: "zh",
	// "auto" or "en". The sherpa model is bilingual and ignores it.
	Language string `json:"language,omitempty"`
	// Target is the language subtitles are translated to.
	Target string `json:"target,omitempty"`

	OrigSize     int  `json:"origSize,omitempty"`
	TransSize    int  `json:"transSize,omitempty"`
	Opacity      int  `json:"opacity,omitempty"` // of the background, in percent
	HideOriginal bool `json:"hideOriginal,omitempty"`

	// The window's content bounds in DIPs. A zero size is the default size,
	// which follows the font sizes; the user resizing it keeps theirs.
	// Placed says X and Y were saved; without it the window opens at the
	// bottom centre of the display in use.
	Placed bool `json:"placed,omitempty"`
	X      int  `json:"x,omitempty"`
	Y      int  `json:"y,omitempty"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

const (
	SubtitleQwen   = "qwen"
	SubtitleSherpa = "sherpa"

	SubtitleOrigMin, SubtitleOrigMax   = 12, 24
	SubtitleTransMin, SubtitleTransMax = 16, 36
	SubtitleOpacityMin                 = 30
)

// Normalized fills in the defaults and keeps the sizes within their ranges.
func (s Subtitles) Normalized() Subtitles {
	if s.Engine != SubtitleQwen {
		s.Engine = SubtitleSherpa
	}
	switch s.Language {
	case "zh", "auto", "en":
	default:
		s.Language = "zh"
	}
	if s.Target == "" {
		s.Target = "zh-Hans"
	}
	if s.OrigSize == 0 {
		s.OrigSize = 15
	}
	if s.TransSize == 0 {
		s.TransSize = 22
	}
	if s.Opacity == 0 {
		s.Opacity = 72
	}
	s.OrigSize = min(max(s.OrigSize, SubtitleOrigMin), SubtitleOrigMax)
	s.TransSize = min(max(s.TransSize, SubtitleTransMin), SubtitleTransMax)
	s.Opacity = min(max(s.Opacity, SubtitleOpacityMin), 100)
	return s
}
