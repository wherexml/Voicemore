package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"voiceinput/internal/store"
)

// The same timeline is used on Home and History. Expanded records reuse the
// waveform player, cleanup action and timings without duplicating state.
func journalDay(at, now time.Time) string {
	at = at.In(now.Location())
	if at.Format("2006-01-02") == now.Format("2006-01-02") {
		return T("today")
	}
	return strings.TrimSpace(strings.TrimSuffix(timeLabel(at, now), at.Format("15:04")))
}

func journalAction(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	return ui.ButtonBase(c).Size(28, 28).Padding(0).Radius(6).Justify(ui.Center).AlignItems(ui.Center).
		Label(label).Tooltip(label).Children(func() { ui.Icon(c, svg).Size(16, 16).TextColor(softText(c.Theme())) })
}

func (m *mainWindow) copyRecordButton(c *ui.Context, key, text, label string) ui.Element {
	const duration = 1500 * time.Millisecond
	now := time.Now()
	at, copied := m.recentCopied[key]
	if copied && now.Sub(at) >= duration {
		delete(m.recentCopied, key)
		copied = false
	}
	glyph := icCopy
	if copied {
		glyph = icCheck
		label = T("copied")
		c.After(duration - now.Sub(at))
	}
	b := journalAction(c.Key("copy"), glyph, label).Disabled(text == "")
	if b.Clicked() && text != "" {
		mygo.Clipboard.WriteText(text)
		if m.recentCopied == nil {
			m.recentCopied = map[string]time.Time{}
		}
		m.recentCopied[key] = time.Now()
		c.After(0)
	}
	return b
}

func (m *mainWindow) journal(c *ui.Context, records []store.Record, now time.Time) {
	t := c.Theme()
	if m.selected != "" && !slices.ContainsFunc(records, func(r store.Record) bool { return r.ID == m.selected }) {
		m.stopPlayer()
		m.selected = ""
	}
	if len(records) == 0 {
		ui.Text(c, T("noRecords")).Padding(24, 0).TextColor(t.TextMuted)
		return
	}
	day := ""
	for _, r := range records {
		group := journalDay(r.StartedAt, now)
		if group != day {
			ui.Text(c, group).FontSize(12).TextColor(t.TextMuted).Padding(16, 0, 8)
			day = group
		}
		expanded := m.selected == r.ID
		row := ui.Row(c.Key(r.ID)).FillWidth().Gap(10).AlignItems(ui.Start)
		row.Children(func() {
			ui.Text(c, r.StartedAt.In(now.Location()).Format("15:04")).Width(52).Padding(13, 0, 0, 0).TextAlign(ui.End).Font("Menlo").FontSize(12).TextColor(t.TextMuted)
			ui.Box(c).Width(18).AlignSelf(ui.Stretch).Draw(func(p *ui.Painter, rect ui.Rect) {
				x := rect.X + 8
				p.Fill(ui.Rect{X: x, Y: rect.Y, W: .5, H: rect.H}, settingsGroupLine(c), 0)
				color := t.TextMuted
				if r.CleanupRequested && r.CleanupError == "" {
					color = t.Accent
				}
				p.Fill(ui.Rect{X: x - 3, Y: rect.Y + 18, W: 7, H: 7}, color, 3.5)
				p.Fill(ui.Rect{X: x - 1.5, Y: rect.Y + 19.5, W: 4, H: 4}, t.Background, 2)
			})
			card := ui.Column(c.Key("card")).Grow(1).MinWidth(0).Radius(10).Margin(2, 0, 2)
			if expanded {
				card.Margin(2, 0, 10)
				card.Background(fillColor(t)).Border(.5, settingsGroupLine(c))
			}
			text, _ := primaryText(r)
			shown := text
			if shown == "" {
				shown = T("emptyText")
			}
			card.Children(func() {
				b := ui.ButtonBase(c.Key("body")).FillWidth().Padding(10, 12).Justify(ui.Start).AlignItems(ui.Start).Label(shown)
				b.Children(func() {
					ui.Column(c).FillWidth().Gap(5).Children(func() {
						if r.TargetApp != "" {
							ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() { appIdentity(c, r.TargetBundleID, r.TargetApp, false, true) })
						}
						body := ui.Text(c, shown).FontWeight(400).FontSize(14).LineHeight(1.6)
						if !expanded {
							body.MaxLines(2)
						}
						if expanded && r.RawText != "" && r.RawText != text {
							ui.Column(c).FillWidth().Padding(8, 0, 0).Gap(6).Children(func() {
								ui.Box(c).FillWidth().Height(.5).Background(settingsGroupLine(c))
								ui.Text(c, T("v3Raw")+"  "+r.RawText).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.6).Selectable()
							})
						}
						if !expanded {
							meta := ui.Row(c).Height(17).Gap(8).Children(func() { ui.Text(c, duration(r.DurationMs)).FontSize(11).TextColor(t.TextMuted); statusTag(c, r) })
							if !row.Hovered() && !row.FocusWithin() {
								meta.Opacity(0)
							}
						}
					})
				})
				if b.Clicked() {
					if expanded {
						m.stopPlayer()
						m.selected = ""
					} else {
						m.selectRecord(r)
					}
				}
				if expanded {
					ui.Column(c.Key("detail")).FillWidth().Padding(2, 12, 12).Gap(10).Children(func() { m.recordDetail(c, r) })
				}
			})
			actions := ui.Row(c.Key("actions")).Gap(2).Padding(10, 0, 0).Children(func() {
				m.copyRecordButton(c, r.ID, text, T("copy"))
				if journalAction(c.Key("delete"), icTrash, T("del")).Disabled(m.busy != nil).Clicked() {
					if expanded {
						m.stopPlayer()
						m.selected = ""
					}
					m.report(m.a.history.Delete(r.ID), "deleted")
				}
			})
			if !row.Hovered() && !row.FocusWithin() && !expanded {
				actions.Opacity(0)
			}
		})
	}
}
