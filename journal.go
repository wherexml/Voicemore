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
// existing player, revision editor, reruns and diagnostics without duplicating state.
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
			ui.Text(c, group).FontSize(12).TextColor(t.TextMuted).Padding(22, 0, 10)
			day = group
		}
		ui.Column(c.Key(r.ID)).Role(ui.RoleGroup).Label(r.StartedAt.Format("1/2 15:04")).FillWidth().Children(func() {
			row := ui.Row(c.Key("row")).FillWidth().MinHeight(66).Gap(10).AlignItems(ui.Start)
			row.Children(func() {
				ui.Text(c, r.StartedAt.In(now.Location()).Format("15:04")).Width(64).Padding(13, 0, 0, 0).TextAlign(ui.End).Font("Menlo").FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.Box(c).Width(18).AlignSelf(ui.Stretch).Draw(func(p *ui.Painter, rect ui.Rect) {
					x := rect.X + 8
					p.Fill(ui.Rect{X: x, Y: rect.Y, W: 0.5, H: rect.H}, pick(t, "#dddddd", "#333333"), 0)
					color := t.TextMuted
					if r.CleanupRequested && r.CleanupError == "" {
						color = t.Accent
					}
					p.Fill(ui.Rect{X: x - 3, Y: rect.Y + 18, W: 7, H: 7}, color, 3.5)
					p.Fill(ui.Rect{X: x - 1.5, Y: rect.Y + 19.5, W: 4, H: 4}, t.Background, 2)
				})
				text, revised := primaryText(r)
				shown := text
				if shown == "" {
					shown = T("emptyText")
				}
				open := ui.ButtonBase(c.Key("body")).Grow(1).MinWidth(0).Padding(10, 12).Margin(2, 0).Justify(ui.Start).AlignItems(ui.Start).Label(shown)
				var meta ui.Element
				open.Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						if r.TargetApp != "" {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() { appIdentity(c, r.TargetBundleID, r.TargetApp, false) })
						}
						ui.Text(c, shown).FontWeight(400).FontSize(14).LineHeight(1.6).MaxLines(2)
						meta = ui.Row(c).Height(17).Gap(8).Children(func() {
							ui.Text(c, timeLabel(r.StartedAt, now)).FontSize(11).TextColor(t.TextMuted)
							ui.Text(c, duration(r.DurationMs)).FontSize(11).TextColor(t.TextMuted)
							if revised {
								ui.Text(c, T("revision")).FontSize(11).TextColor(t.Accent)
							} else if r.CleanupRequested && r.CleanupError == "" {
								ui.Text(c, T("cleaned")).FontSize(11).TextColor(t.Accent)
							}
							statusTag(c, r)
						})
					})
				})
				toggle := func() {
					if m.selected == r.ID {
						m.stopPlayer()
						m.selected = ""
					} else {
						m.selectRecord(r)
						m.detailsPeek = r.ID
					}
				}
				if open.Clicked() {
					toggle()
				}
				actions := ui.Row(c.Key("actions")).Gap(6).Padding(10, 0, 0).AlignItems(ui.Center)
				actions.Children(func() {
					m.copyRecordButton(c, r.ID, text, T("copyOf", shown))
					if journalAction(c.Key("details"), icDetails, T("details")).Clicked() {
						toggle()
					}
					if journalAction(c.Key("delete"), icTrash, T("del")).Disabled(m.busy != nil).Clicked() {
						if m.selected == r.ID {
							m.stopPlayer()
							m.selected = ""
						}
						m.report(m.a.history.Delete(r.ID), "deleted")
					}
				})
				_, copied := m.recentCopied[r.ID]
				active := row.Hovered() || row.FocusWithin() || m.selected == r.ID || copied
				if !active {
					meta.Opacity(0)
					actions.Opacity(0)
				}
			})
			if m.selected == r.ID {
				ui.Column(c.Key("detail-inset")).FillWidth().Padding(0, 0, 14, 102).Children(func() {
					ui.Column(c.Key("detail")).FillWidth().MinWidth(0).Padding(18, 20).Radius(10).Background(cardColor(t)).Gap(16).Children(func() { m.detailView(c, r) })
				})
			}
		})
	}
}
