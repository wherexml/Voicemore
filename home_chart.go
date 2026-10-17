package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// The zero value is Today. History keeps its independent hRange mapping.
var homeRangeDays = [...]int{1, 7, 14, 30}

func homeChartColor(t *ui.Theme, strong bool) ui.Color {
	if strong {
		return pick(t, "#4C2FA8", "#CBB9FF")
	}
	return pick(t, "#6E56CF", "#A28BF5")
}

// todayChart shares the summary and hour filter with the multi-day heatmap.
// Use local wall-clock hours (not elapsed-hour arithmetic) across DST changes.
func (m *mainWindow) todayChart(c *ui.Context, s historySummary, now time.Time) {
	t := c.Theme()
	day := dayStart(now)
	atHour := func(h int) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, now.Location())
	}
	peakHour := 0
	for h := 1; h < 24; h++ {
		if s.hours[hourKey(atHour(h))] > s.hours[hourKey(atHour(peakHour))] {
			peakHour = h
		}
	}
	ui.Column(c.Key("today-chart")).FillWidth().Gap(0).Children(func() {
		ui.Row(c).FillWidth().Height(26).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, statNumber(s.chars)).FontSize(18).FontWeight(600).SingleLine()
			ui.Text(c, T("homeAsOf", now.Format("15:04"))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			ui.Spacer(c)
			if s.peak > 0 {
				ui.Text(c, T("homePeak", peakHour, statNumber(s.peak))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
			ui.Box(c).Width(31)
		})
		ui.Row(c.Key("hours")).Role(ui.RoleGroup).Label(T("homeHourly")).FillWidth().Height(116).Gap(3).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Height(116).Gap(3).Children(func() {
				for h := 0; h < 24; h++ {
					at := atHour(h)
					key := hourKey(at)
					n := s.hours[key]
					future := h > now.Hour()
					selected := !m.hHour.IsZero() && hourKey(m.hHour) == key
					tip := fmt.Sprintf("%s %02d:00–%02d:59 · %s %s", day.Format("2006/1/2"), h, h, statNumber(n), T("v2CharUnit"))
					if future {
						tip = fmt.Sprintf("%s %02d:00–%02d:59 · %s", day.Format("2006/1/2"), h, h, T("homeUpcoming"))
					}
					ref := m.heatFocus[key]
					if ref == nil {
						ref = &ui.Handle{}
						m.heatFocus[key] = ref
					}
					b := ui.ButtonBase(c.Key(key)).Bind(ref).Grow(1).MinWidth(0).Height(116).Padding(0).Radius(3).
						Label(tip).Tooltip(tip).Selected(selected).Disabled(future)
					active := selected || b.Hovered() || b.Focused()
					b.Draw(func(p *ui.Painter, r ui.Rect) {
						line := settingsGroupLine(c)
						p.Line(r.X, r.Y+r.H, r.X+r.W, r.Y+r.H, .5, line)
						p.Line(r.X, r.Y, r.X+r.W, r.Y, .5, line)
						if future {
							p.Clip(r, 3, func() {
								var stripes ui.Path
								for x := r.X - r.H; x < r.X+r.W; x += 5 {
									stripes.MoveTo(x, r.Y+r.H).LineTo(x+r.H, r.Y)
								}
								p.StrokePath(&stripes, 1, line)
							})
							return
						}
						color := homeChartColor(t, active)
						height := max(float32(4), float32(n)/float32(max(1, s.peak))*r.H)
						if n == 0 {
							height = 2
							if active {
								height = 4
							} else {
								color = line
							}
						}
						if !m.hHour.IsZero() && !selected {
							color.A = uint8(float32(color.A) * .4)
						}
						p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - height, W: r.W, H: height}, color, 3)
					})
					if b.Clicked() {
						m.hHour = at
						if selected {
							m.hHour = time.Time{}
						}
						m.hPage = 1
					}
					for _, move := range []struct {
						key  ui.Key
						hour int
					}{
						{ui.KeyLeft, h - 1}, {ui.KeyRight, h + 1}, {ui.KeyHome, 0}, {ui.KeyEnd, now.Hour()},
					} {
						b.OnShortcut(0, move.key, func() {
							if move.hour >= 0 && move.hour <= now.Hour() {
								if ref := m.heatFocus[hourKey(atHour(move.hour))]; ref != nil {
									ref.Focus(c)
								}
							}
						})
					}
				}
				if s.chars == 0 {
					ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Left(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, T("homeEmptyToday")).FontSize(12).TextColor(t.TextMuted).Padding(5, 10).Radius(6).Background(cardColor(t))
					})
				}
			})
			ui.Column(c).Width(34).Height(116).Justify(ui.SpaceBetween).Children(func() {
				label := ""
				if s.peak > 0 {
					label = statNumber(s.peak)
				}
				ui.Text(c, label).FillWidth().FontSize(10).Font("Menlo").TextColor(t.TextMuted).TextAlign(ui.End)
				ui.Text(c, "0").FillWidth().FontSize(10).Font("Menlo").TextColor(t.TextMuted).TextAlign(ui.End)
			})
		})
		ui.Row(c).FillWidth().Height(22).Gap(3).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Height(22).Gap(3).Children(func() {
				for h := 0; h < 24; h++ {
					label := ""
					if h%3 == 0 || h == 23 {
						label = fmt.Sprint(h)
					}
					ui.Box(c.Key(h)).Grow(1).MinWidth(0).Height(22).Draw(func(p *ui.Painter, r ui.Rect) {
						if h == now.Hour() {
							p.Fill(ui.Rect{X: r.X + r.W/2 - 2, Y: r.Y + 2, W: 4, H: 4}, homeChartColor(t, false), 2)
						}
					}).Children(func() {
						ui.Text(c, label).FillWidth().Padding(7, 0, 0).FontSize(10).Font("Menlo").TextColor(t.TextMuted).TextAlign(ui.Center)
					})
				}
			})
			ui.Box(c).Width(34)
		})
	})
}

func init() {
	for k, v := range map[string][3]string{
		"homeToday":      {"Today", "今日", "今日"},
		"homeAsOf":       {"characters · as of %s", "字元 · 截至 %s", "字符 · 截至 %s"},
		"homePeak":       {"Peak %d:00 · %s", "高峰 %d:00 · %s", "高峰 %d:00 · %s"},
		"homeHourly":     {"Characters per hour today", "今日 24 小時輸入量", "今日 24 小时输入量"},
		"homeUpcoming":   {"Upcoming", "尚未到來", "尚未到来"},
		"homeClearHour":  {"Clear hour ×", "清除時段 ×", "清除时段 ×"},
		"homeEmptyToday": {"No input yet today", "今日暫無輸入", "今日暂无输入"},
	} {
		texts[k] = v
	}
}
