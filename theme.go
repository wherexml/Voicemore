package main

import "github.com/egoist/mygo/ui"

// The palette is that of mock/index.html, after the OpsTwins app: a white
// ground, one quiet divider, a subtle fill, and the accent kept for
// primary actions and the soft selection. MyGo's default theme fills
// controls and cards with a mid grey, which read much heavier.
//
// The frame's theme is a copy of the default with these colors, set with
// Context.SetTheme at the top of each window's view. The copy keeps what
// the default takes from the system: light or dark, the font and the
// metrics. With Increase Contrast on, the default's stronger borders and
// secondary text are kept too.

// useTheme sets the app's theme for this frame and returns it.
func useTheme(c *ui.Context) *ui.Theme {
	t := *c.Theme() // the default, which follows the system
	strong := c.Preferences().HighContrast
	if t.Dark {
		t.Background = ui.Hex("#1b1d22")
		t.Surface = ui.Hex("#22252b") // buttons and fields
		t.SurfaceHover = ui.Hex("#292c33")
		t.SurfacePressed = ui.Hex("#30343c")
		t.Text = ui.Hex("#eceef2")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#6f98f3"), ui.Hex("#84a8f6"), ui.Hex("#5d88ea"), ui.Hex("#10131a")
		t.Danger, t.Warning, t.Success = ui.Hex("#ff7b86"), ui.Hex("#e7b25a"), ui.Hex("#5fcf8f")
		t.Selection = ui.Hex("#24304a")
		if !strong {
			t.Border = ui.Hex("#2a2d34")
			t.TextMuted = ui.Hex("#a0a7b3")
		}
	} else {
		t.Background = ui.Hex("#ffffff")
		t.Surface = ui.Hex("#ffffff") // buttons and fields: white, with a hairline
		t.SurfaceHover = ui.Hex("#f5f6f8")
		t.SurfacePressed = ui.Hex("#f0f1f3")
		t.Text = ui.Hex("#252831")
		t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = ui.Hex("#356ee8"), ui.Hex("#2f63d3"), ui.Hex("#2a59be"), ui.Hex("#ffffff")
		t.Danger, t.Warning, t.Success = ui.Hex("#c5283a"), ui.Hex("#8a5a00"), ui.Hex("#1f8a4c")
		t.Selection = ui.Hex("#e8effd")
		if !strong {
			t.Border = ui.Hex("#e7e9ee")
			t.TextMuted = ui.Hex("#626a76")
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

// railColor is the ground of the navigation rail, and railSelected the
// circle of its current page.
func railColor(t *ui.Theme) ui.Color    { return pick(t, "#fcfcfd", "#17191d") }
func railSelected(t *ui.Theme) ui.Color { return pick(t, "#edeef1", "#2c3038") }

// fillColor is the subtle fill: card footers, the settings' category list.
func fillColor(t *ui.Theme) ui.Color { return pick(t, "#f5f6f8", "#22252b") }

// hoverColor is the fill of something the pointer is on.
func hoverColor(t *ui.Theme) ui.Color { return pick(t, "#f0f1f3", "#292c33") }

// strongLine is a divider a little firmer than the theme's, as the edge of
// the keycap.
func strongLine(t *ui.Theme) ui.Color { return pick(t, "#d7dae1", "#3a3e47") }

// faintText is for what matters least, as a time beside a text.
func faintText(t *ui.Theme) ui.Color { return pick(t, "#9aa1ad", "#6f7682") }

// cardColor fills the grouped boxes.
func cardColor(t *ui.Theme) ui.Color { return pick(t, "#ffffff", "#1f2126") }

// insetColor fills what sits inside a card.
func insetColor(t *ui.Theme) ui.Color { return fillColor(t) }

// noticeColor is the ground of a warning.
func noticeColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(231, 178, 90, 0.14)
	}
	return ui.RGBA(255, 159, 10, 0.12)
}
