package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"slices"
	"time"
	"unicode"
	"voiceinput/internal/store"
)

type historyApp struct {
	key, name, bundle string
	chars             int
}
type historySummary struct {
	start           time.Time
	chars, sessions int
	duration        int64
	outcomes        map[string]int
	hours           map[string]int
	apps            []historyApp
	peak            int
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
func hourKey(t time.Time) string { return t.Format("2006-01-02/15") }
func (m *mainWindow) historyAppKey(r store.Record) string {
	if r.TargetBundleID == "" && m.legacyAppIDs[r.TargetApp] != "" {
		return m.legacyAppIDs[r.TargetApp]
	}
	if r.TargetBundleID != "" {
		return r.TargetBundleID
	}
	return "name:" + r.TargetApp
}
func insertedChars(r store.Record) int {
	if r.Status != store.StatusInserted {
		return 0
	}
	n := 0
	for _, ch := range r.InsertedText {
		if !unicode.IsSpace(ch) {
			n++
		}
	}
	return n
}
func (m *mainWindow) historySummary(records []store.Record, now time.Time) historySummary {
	m.legacyAppIDs = map[string]string{}
	ambiguous := map[string]bool{}
	for _, r := range records {
		if r.TargetBundleID != "" {
			if old := m.legacyAppIDs[r.TargetApp]; old != "" && old != r.TargetBundleID {
				ambiguous[r.TargetApp] = true
			}
			m.legacyAppIDs[r.TargetApp] = r.TargetBundleID
		}
	}
	for name := range ambiguous {
		delete(m.legacyAppIDs, name)
	}
	start := dayStart(now)
	switch m.hRange {
	case 1:
		start = start.AddDate(0, 0, -6)
	case 2:
		start = start.AddDate(0, 0, -29)
	case 3:
		for _, r := range records {
			if r.StartedAt.Before(start) {
				start = dayStart(r.StartedAt.In(now.Location()))
			}
		}
	}
	s := historySummary{start: start, outcomes: map[string]int{}, hours: map[string]int{}}
	apps := map[string]*historyApp{}
	for _, r := range records {
		if r.StartedAt.Before(start) || r.StartedAt.After(now) {
			continue
		}
		s.sessions++
		s.duration += max(0, r.DurationMs)
		s.outcomes[r.Status]++
		n := insertedChars(r)
		s.chars += n
		key := hourKey(r.StartedAt.In(now.Location()))
		s.hours[key] += n
		s.peak = max(s.peak, s.hours[key])
		if n > 0 {
			k := m.historyAppKey(r)
			if apps[k] == nil {
				name := r.TargetApp
				if name == "" {
					name = T("v2UnknownApp")
				}
				apps[k] = &historyApp{key: k, name: name, bundle: r.TargetBundleID}
				if apps[k].bundle == "" {
					apps[k].bundle = m.legacyAppIDs[r.TargetApp]
				}
			}
			apps[k].chars += n
		}
	}
	for _, a := range apps {
		s.apps = append(s.apps, *a)
	}
	slices.SortFunc(s.apps, func(a, b historyApp) int {
		if a.chars != b.chars {
			return b.chars - a.chars
		}
		if a.key < b.key {
			return -1
		}
		return 1
	})
	return s
}
func (m *mainWindow) filteredHistory(records []store.Record, start, now time.Time) []store.Record {
	return slices.DeleteFunc(records, func(r store.Record) bool {
		return r.StartedAt.Before(start) || r.StartedAt.After(now) || (m.hApp != "" && m.historyAppKey(r) != m.hApp) || (m.hStatus != "" && r.Status != m.hStatus) || (!m.hHour.IsZero() && hourKey(r.StartedAt.In(now.Location())) != hourKey(m.hHour))
	})
}
func (m *mainWindow) clearHistoryFilters() {
	m.hApp, m.hStatus, m.query = "", "", ""
	m.hHour = time.Time{}
	m.hPage = 1
}
func statNumber(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func minuteDuration(ms int64) string {
	mins := max(0, ms) / 60000
	if ms > 0 && mins == 0 {
		return T("v2LessMinute")
	}
	if mins >= 60 {
		return T("v2HoursMinutes", mins/60, mins%60)
	}
	return T("v2Minutes", mins)
}
func (m *mainWindow) historyStatsView(c *ui.Context, s historySummary, now time.Time) {
	t := c.Theme()
	ui.Row(c).Role(ui.RoleGroup).Label(T("v2Period")).AlignItems(ui.Center).Gap(12).Children(func() {
		if segmentedSize(c.Key("range"), &m.hRange, true, T("today"), T("v2Seven"), T("v2Thirty"), T("v2All")).Changed() {
			m.clearHistoryFilters()
			c.After(0)
		}
		ui.Spacer(c)
		ui.Text(c, s.start.Format("1/2")+" – "+now.Format("1/2")).FontSize(12).TextColor(t.TextMuted)
	})
	ui.Grid(c).Columns(func() int {
		w, _ := c.Size()
		if w < 840 {
			return 2
		}
		return 4
	}()).Role(ui.RoleGroup).Label(T("v2Summary")).FillWidth().Gap(24).Padding(4, 0, 18).BorderWidth(0, 0, 1, 0).BorderColor(settingsGroupLine(c)).Children(func() {
		for i, v := range [][2]string{{T("v2Chars"), statNumber(s.chars)}, {T("v2Sessions"), statNumber(s.sessions)}, {T("v2Duration"), minuteDuration(s.duration)}, {T("v2Rate"), ""}} {
			ui.Column(c.Key(i)).Grow(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, v[0]).FontSize(12).TextColor(t.TextMuted).SingleLine()
				value := v[1]
				if i == 3 {
					value = "—"
					if s.sessions > 0 {
						value = fmt.Sprintf("%.0f%%", 100*float64(s.outcomes[store.StatusInserted])/float64(s.sessions))
					}
				}
				size := float32(25)
				if i == 0 {
					size = 30
				}
				ui.Text(c, value).FontSize(size).FontWeight(600).SingleLine()
				if i == 3 {
					m.outcomeBar(c, s)
				}
			})
		}
	})
	func() ui.Element {
		w, _ := c.Size()
		if w < 840 {
			return ui.Column(c)
		}
		return ui.Row(c)
	}().Role(ui.RoleGroup).Label(T("v2Trend")).FillWidth().Gap(28).Children(func() {
		m.heatmap(c, s, now)
		ui.Column(c.Key("app-distribution")).Role(ui.RoleGroup).Label(T("v2Apps")).FillWidth().Grow(1).MinWidth(0).Height(300).Gap(16).Children(func() {
			ui.Text(c, T("v2Apps")).FontSize(15).FontWeight(600)
			ui.Scroll(c.Key("apps-scroll")).Grow(1).FillWidth().Gap(8).Children(func() {
				if len(s.apps) == 0 {
					ui.Text(c, T("noRecords")).FontSize(13).TextColor(t.TextMuted)
				}
				for _, a := range s.apps {
					row := ui.ButtonBase(c.Key(a.key)).FillWidth().Padding(8, 6).Radius(6).Label(T("v2FilterApp", a.name)).Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
								appIdentity(c, a.bundle, a.name, true)
								ui.Text(c, statNumber(a.chars)).FontSize(13).SingleLine()
								ui.Textf(c, "%.0f%%", 100*float64(a.chars)/float64(max(1, s.chars))).Width(38).TextAlign(ui.End).FontSize(12).TextColor(t.TextMuted).SingleLine()
							})
							ui.Box(c).FillWidth().Height(3).Margin(0, 0, 0, 28).Draw(func(p *ui.Painter, r ui.Rect) {
								p.Fill(r, settingsGroupColor(t), 1.5)
								r.W *= float32(a.chars) / float32(max(1, s.chars))
								p.Fill(r, t.Accent, 1.5)
							})
						})
					})
					if m.hApp == a.key {
						row.Background(settingsGroupColor(t)).Border(1, t.Accent)
					}
					if row.Clicked() {
						if m.hApp == a.key {
							m.hApp = ""
						} else {
							m.hApp = a.key
						}
						m.hPage = 1
					}
				}
			})
		})
	})
}

var outcomeKeys = []string{store.StatusInserted, store.StatusNotInserted, store.StatusFailed, store.StatusCancelled}
var outcomeLabels = []string{"v2Inserted", "notInsertedTag", "failedTag", "cancelledTag"}

func (m *mainWindow) outcomeBar(c *ui.Context, s historySummary) {
	t := c.Theme()
	colors := []ui.Color{t.Accent, t.TextMuted, t.Danger, settingsGroupLine(c)}
	ui.Row(c).FillWidth().Height(7).Gap(2).Children(func() {
		for i, k := range outcomeKeys {
			n := s.outcomes[k]
			if n == 0 {
				continue
			}
			tip := fmt.Sprintf("%s · %d · %.0f%%", T(outcomeLabels[i]), n, 100*float64(n)/float64(max(1, s.sessions)))
			b := ui.ButtonBase(c.Key(k)).Grow(float32(n)).MinWidth(0).Height(7).Padding(0).Radius(2).Background(colors[i]).Label(tip).Tooltip(tip)
			if m.hStatus == k {
				b.Border(1, t.Text)
			}
			if b.Clicked() {
				if m.hStatus == k {
					m.hStatus = ""
				} else {
					m.hStatus = k
				}
				m.hPage = 1
			}
		}
	})
}
func (m *mainWindow) heatmap(c *ui.Context, s historySummary, now time.Time) {
	t := c.Theme()
	if m.heatFocus == nil {
		m.heatFocus = map[string]*ui.Handle{}
	}
	panel := ui.Column(c.Key("trend")).Role(ui.RoleGroup).Label(T("v2Trend")).FillWidth().Grow(1.35).MinWidth(0).Height(300).Gap(16)
	panel.Children(func() {
		ui.Text(c, T("v2Trend")).FontSize(15).FontWeight(600)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				for h := 0; h < 24; h++ {
					label := ""
					if h == 0 || h == 6 || h == 12 || h == 18 || h == 23 {
						label = fmt.Sprint(h)
					}
					ui.Text(c, label).Grow(1).MinWidth(0).FontSize(11).TextColor(t.TextMuted).TextAlign(ui.Center)
				}
			})
			ui.Box(c).Width(34)
		})
		ui.Scroll(c.Key(fmt.Sprint("heat-", m.hRange))).Grow(1).FillWidth().Gap(3).Children(func() {
			for day := dayStart(now); !day.Before(s.start); day = day.AddDate(0, 0, -1) {
				ui.Row(c.Key(day.Format("2006-01-02"))).Role(ui.RoleGroup).Label(day.Format("2006/1/2")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Row(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						for h := 0; h < 24; h++ {
							at := time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, now.Location())
							key := hourKey(at)
							n := s.hours[key]
							future := at.After(now)
							color := settingsGroupColor(t)
							if n > 0 {
								color = t.Accent
								color.A = uint8(70 + 185*float32(n)/float32(max(1, s.peak)))
							}
							tip := fmt.Sprintf("%s %02d:00 · %s %s", day.Format("2006/1/2"), h, statNumber(n), T("v2CharUnit"))
							ref := m.heatFocus[key]
							if ref == nil {
								ref = &ui.Handle{}
								m.heatFocus[key] = ref
							}
							b := ui.ButtonBase(c.Key(key)).Bind(ref).Grow(1).MinWidth(0).Height(16).Padding(0).Radius(3).Background(color).Label(tip).Tooltip(tip).Disabled(future)
							if !m.hHour.IsZero() && hourKey(m.hHour) == key {
								b.Border(1, t.Text)
							}
							if b.Clicked() {
								if hourKey(m.hHour) == key {
									m.hHour = time.Time{}
								} else {
									m.hHour = at
								}
								m.hPage = 1
							}
							for _, move := range []struct {
								key  ui.Key
								date time.Time
							}{{ui.KeyLeft, at.Add(-time.Hour)}, {ui.KeyRight, at.Add(time.Hour)}, {ui.KeyUp, at.AddDate(0, 0, 1)}, {ui.KeyDown, at.AddDate(0, 0, -1)}, {ui.KeyHome, day}, {ui.KeyEnd, day.Add(23 * time.Hour)}} {
								b.OnShortcut(0, move.key, func() {
									if !move.date.Before(s.start) && !move.date.After(now) {
										if ref := m.heatFocus[hourKey(move.date)]; ref != nil {
											ref.Focus(c)
										}
									}
								})
							}
						}
					})
					label := ui.Text(c, day.Format("1/2")).Width(34).FontSize(11).TextColor(t.TextMuted).SingleLine()
					if !panel.Hovered() && !panel.FocusWithin() {
						label.Opacity(0)
					}
				})
			}
		})
	})
}
func (m *mainWindow) historyFilterChips(c *ui.Context) {
	if m.hApp == "" && m.hStatus == "" && m.hHour.IsZero() && m.query == "" {
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		if m.hApp != "" {
			name := m.hApp
			for _, r := range m.a.history.List("") {
				if m.historyAppKey(r) == m.hApp {
					name = r.TargetApp
					break
				}
			}
			if ui.Button(c.Key("app-chip"), name+" ×").Clicked() {
				m.hApp = ""
				m.hPage = 1
			}
		}
		if !m.hHour.IsZero() {
			if ui.Button(c.Key("hour-chip"), m.hHour.Format("1/2 15:00")+" ×").Clicked() {
				m.hHour = time.Time{}
				m.hPage = 1
			}
		}
		if m.hStatus != "" {
			label := m.hStatus
			for i, k := range outcomeKeys {
				if k == m.hStatus {
					label = T(outcomeLabels[i])
				}
			}
			if ui.Button(c.Key("status-chip"), label+" ×").Clicked() {
				m.hStatus = ""
				m.hPage = 1
			}
		}
		if ui.Button(c.Key("clear"), T("v2Clear")).Clicked() {
			m.clearHistoryFilters()
		}
	})
}
func init() {
	for k, v := range map[string][3]string{
		"v2Minutes": {"%d min", "%d 分鐘", "%d 分钟"}, "v2HoursMinutes": {"%dh %dmin", "%d 小時 %d 分鐘", "%d 小时 %d 分钟"}, "v2LessMinute": {"< 1 min", "不足 1 分鐘", "不足 1 分钟"}, "v2Records": {"Input records", "輸入記錄", "输入记录"},
		"v2Period": {"Date range", "日期範圍", "日期范围"}, "v2Summary": {"Usage summary", "使用統計", "使用统计"},
		"v2Seven": {"7 days", "近 7 天", "近 7 天"}, "v2Thirty": {"30 days", "近 30 天", "近 30 天"}, "v2All": {"All", "全部", "全部"},
		"v2Chars": {"Characters inserted", "寫入字元", "写入字符"}, "v2Sessions": {"Dictation sessions", "語音輸入次數", "语音输入次数"}, "v2Duration": {"Recording time", "錄音時長", "录音时长"}, "v2Rate": {"Insertion rate", "寫入率", "写入率"}, "v2Trend": {"Input trend", "輸入趨勢", "输入趋势"}, "v2Apps": {"App distribution", "應用分佈", "应用分布"}, "v2CharUnit": {"characters", "字元", "字符"}, "v2UnknownApp": {"Unknown app", "未知應用", "未知应用"}, "v2FilterApp": {"Filter %s", "篩選 %s", "筛选 %s"}, "v2Inserted": {"Inserted", "已寫入", "已写入"}, "v2Clear": {"Clear filters", "清除篩選", "清除筛选"},
		"v2Terms": {"Term hints", "詞語提示", "词语提示"}, "v2Rules": {"Replacement rules", "替換規則", "替换规则"}, "v2Review": {"To review", "待確認", "待确认"},
	} {
		texts[k] = v
	}
}
