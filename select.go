package main

import "github.com/egoist/mygo/ui"

// selectOption separates a stable value from its localized text and optional
// application/provider presentation. All settings pickers share this control.
type selectOption struct {
	Value, Label, Group string
	Content             func(*ui.Context)
}

func (p *prefsWindow) standardSelect(c *ui.Context, id, label, value, placeholder string, width float32, disabled bool, opts []selectOption, onChange func(string)) {
	t := c.Theme()
	open := p.pickOpen[id]
	if open == nil {
		open = new(bool)
		p.pickOpen[id] = open
	}
	if disabled {
		*open = false
	}
	chosen := -1
	for i, o := range opts {
		if o.Value == value {
			chosen = i
			break
		}
	}
	accessibleLabel := label
	if chosen >= 0 {
		accessibleLabel += ": " + opts[chosen].Label
	}
	trigger := ui.ButtonBase(c.Key(id+"-trigger")).Height(40).Padding(0, 12).Gap(10).Radius(9).AlignItems(ui.Center).Background(t.Background).Border(1, settingsGroupLine(c)).Label(accessibleLabel).Disabled(disabled)
	if width > 0 {
		trigger.Width(width)
	} else {
		trigger.FillWidth()
	}
	trigger.Children(func() {
		if chosen >= 0 && opts[chosen].Content != nil {
			opts[chosen].Content(c)
		} else {
			name := placeholder
			if chosen >= 0 {
				name = opts[chosen].Label
			}
			ui.Text(c, name).FontSize(13).Grow(1).SingleLine()
		}
		arrow := icDown
		if *open {
			arrow = icUp
		}
		ui.Icon(c, arrow).FontSize(14).TextColor(faintText(t))
	})
	if trigger.Clicked() {
		for k, v := range p.pickOpen {
			if k != id {
				*v = false
			}
		}
		*open = !*open
		p.pickFocus[id] = *open
	}
	for _, key := range []ui.Key{ui.KeyDown, ui.KeyUp} {
		trigger.OnShortcut(0, key, func() {
			if !disabled {
				for k, v := range p.pickOpen {
					if k != id {
						*v = false
					}
				}
				*open = true
				p.pickFocus[id] = true
			}
		})
	}
	ui.PopoverBase(c.Key(id+"-popover"), trigger, open, func(panel ui.Element) {
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			*open = false
			trigger.Focus()
			return
		}
		panel.Margin(6, 0, 0, 0).Padding(5).Radius(12).Width(max(160, trigger.Bounds().W)).Background(t.Background).Border(1, settingsGroupLine(c)).Shadow(0, 12, 34, 0, ui.RGBA(30, 38, 48, .12))
		if len(opts) == 0 {
			ui.Text(c, T("selectEmpty")).FontSize(12).TextColor(t.TextMuted).Padding(12, 10)
			return
		}
		handles := p.pickHandles[id]
		for len(handles) < len(opts) {
			handles = append(handles, ui.Handle{})
		}
		p.pickHandles[id] = handles
		focused := -1
		for i := range opts {
			if handles[i].Focused(c) {
				focused = i
			}
		}
		move := func(n int) { handles[(n+len(opts))%len(opts)].Focus() }
		switch {
		case c.Shortcut(0, ui.KeyDown):
			if focused < 0 {
				move(max(0, chosen))
			} else {
				move(focused + 1)
			}
		case c.Shortcut(0, ui.KeyUp):
			if focused < 0 {
				move(max(0, chosen))
			} else {
				move(focused - 1)
			}
		case c.Shortcut(0, ui.KeyHome):
			move(0)
		case c.Shortcut(0, ui.KeyEnd):
			move(len(opts) - 1)
		}
		ui.Scroll(c).MaxHeight(330).Gap(1).Children(func() {
			for i, o := range opts {
				if o.Group != "" && (i == 0 || opts[i-1].Group != o.Group) {
					ui.Text(c, o.Group).FontSize(12).FontWeight(500).TextColor(faintText(t)).Padding(7, 10)
				}
				row := ui.ButtonBase(c.Key(o.Value)).FillWidth().MinHeight(38).Padding(9, 10).Gap(10).Radius(8).AlignItems(ui.Center).Label(o.Label).Bind(&handles[i])
				row.Children(func() {
					if o.Content != nil {
						o.Content(c)
					} else {
						ui.Text(c, o.Label).FontSize(13).Grow(1).SingleLine()
					}
					check := ui.Icon(c, icCheck).FontSize(13).TextColor(t.Accent)
					if chosen != i {
						check.Invisible()
					}
				})
				if chosen == i {
					row.Background(settingsGroupColor(t))
				}
				if row.Hovered() || focused == i {
					row.Background(hoverColor(t))
				}
				if row.Clicked() {
					*open = false
					trigger.Focus()
					onChange(o.Value)
				}
			}
		})
		if p.pickFocus[id] {
			p.pickFocus[id] = false
			handles[max(0, chosen)].Focus()
		}
	})
}
