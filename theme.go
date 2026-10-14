package main

import "github.com/egoist/mygo/ui"

// The neutral palette follows the approved mock/claude.html and OpsTwin:
// white/light-grey surfaces, quiet borders, neutral primary actions, and
// color reserved for status. The rail uses the same neutral family.
//
// The frame's theme is a copy of the default with these colors, set with
// Context.SetTheme at the top of each window's view. The copy keeps what
// the default takes from the system: the font and the metrics, and light
// or dark unless Settings → General says otherwise. With Increase
// Contrast on, the default's stronger borders and secondary text are kept.

// appearance is the saved choice: "light", "dark" or "" for the system's.
// It is read and written on the main thread only.
var appearance string

// useTheme sets the app's theme for this frame and returns it.
func useTheme(c *ui.Context) *ui.Theme {
	t := *c.Theme() // the default, which follows the system
	strong := c.Preferences().HighContrast
	switch appearance {
	case "light":
		t.Dark = false
	case "dark":
		t.Dark = true
	}
	if t.Dark {
		t.Background = ui.Hex("#161616")
		t.Surface = ui.Hex("#1b1b1b") // buttons and fields
		t.SurfaceHover = ui.Hex("#242424")
		t.SurfacePressed = ui.Hex("#2e2e2e")
		t.Text = ui.Hex("#fafafa")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#fafafa"), ui.Hex("#d4d4d4"), ui.Hex("#d4d4d4"), ui.Hex("#262626")
		t.Danger, t.Warning, t.Success = ui.Hex("#f87171"), ui.Hex("#fbbf24"), ui.Hex("#34d399")
		t.Selection = ui.Hex("#292929")
		if !strong {
			t.Border = ui.Hex("#242424")
			t.TextMuted = ui.Hex("#a3a3a3")
		}
	} else {
		t.Background = ui.Hex("#ffffff")
		t.Surface = ui.Hex("#ffffff") // buttons and fields: white, with a hairline
		t.SurfaceHover = ui.Hex("#f5f5f5")
		t.SurfacePressed = ui.Hex("#ebebeb")
		t.Text = ui.Hex("#262626")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#262626"), ui.Hex("#404040"), ui.Hex("#000000"), ui.Hex("#fafafa")
		t.Danger, t.Warning, t.Success = ui.Hex("#b91c1c"), ui.Hex("#92400e"), ui.Hex("#047857")
		t.Selection = ui.Hex("#f0f0f0")
		if !strong {
			t.Border = ui.Hex("#ebebeb")
			t.TextMuted = ui.Hex("#666666")
		}
	}
	c.SetTheme(&t)
	return &t
}

func pick(t *ui.Theme, light, dark string) ui.Color {
	if t.Dark {
		return ui.Hex(dark)
	}
	return ui.Hex(light)
}

// The navigation rail keeps the colors it always had: its ground, the
// circle of its current page, the fill under the pointer, and its edge.
func railColor(t *ui.Theme) ui.Color    { return pick(t, "#fafafa", "#171717") }
func railSelected(t *ui.Theme) ui.Color { return pick(t, "#ebebeb", "#2a2a2a") }
func railHover(t *ui.Theme) ui.Color    { return pick(t, "#f0f0f0", "#202020") }
func railLine(t *ui.Theme) ui.Color     { return pick(t, "#ebebeb", "#252525") }
func railText(t *ui.Theme) ui.Color     { return pick(t, "#262626", "#fafafa") }

// fillColor is the subtle fill: card footers, the settings' category list.
func fillColor(t *ui.Theme) ui.Color { return pick(t, "#f5f5f5", "#202020") }

// hoverColor is the fill of something the pointer is on.
func hoverColor(t *ui.Theme) ui.Color { return pick(t, "#f0f0f0", "#242424") }

// strongLine is a divider a little firmer than the theme's, as the edge of
// a field or of the keycap.
func strongLine(t *ui.Theme) ui.Color { return pick(t, "#d6d6d6", "#454545") }

// softText is the secondary text: values and descriptions.
func softText(t *ui.Theme) ui.Color { return pick(t, "#404040", "#d4d4d4") }

// faintText is for what matters least, as a time beside a text.
func faintText(t *ui.Theme) ui.Color { return pick(t, "#666666", "#a3a3a3") }

// infoText marks a state worth noticing that is neither good nor bad.
func infoText(t *ui.Theme) ui.Color { return pick(t, "#1d4ed8", "#60a5fa") }

// cardColor fills the grouped boxes.
func cardColor(t *ui.Theme) ui.Color { return pick(t, "#ffffff", "#1b1b1b") }

// insetColor fills what sits inside a card.
func insetColor(t *ui.Theme) ui.Color { return fillColor(t) }

// noticeColor is the ground of a warning.
func noticeColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(251, 191, 36, 0.14)
	}
	return ui.RGBA(146, 64, 14, 0.09)
}

// okTint is the ground of a small "done" mark.
func okTint(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(52, 211, 153, 0.16)
	}
	return ui.RGBA(4, 120, 87, 0.12)
}
