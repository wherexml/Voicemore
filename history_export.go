package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"strings"
	"time"
	"voiceinput/internal/store"
)

func (m *mainWindow) openHistoryExport() {
	now := time.Now()
	m.exportFrom = now.AddDate(0, 0, -29).Format("2006-01-02")
	m.exportTo = now.Format("2006-01-02")
	m.exportError = ""
	m.exportOpen = true
	m.exportCalendar = ""
}
func exportCell(s string) string {
	if strings.HasPrefix(s, "=") || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "@") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
		return "'" + s
	}
	return s
}
func historyCSV(records []store.Record, from, to time.Time) ([]byte, int, error) {
	var b bytes.Buffer
	b.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	if err := w.Write([]string{T("v3CSVTime"), T("v3CSVApp"), T("v3Raw"), T("v3CSVResult")}); err != nil {
		return nil, 0, err
	}
	count := 0
	for _, r := range records {
		if r.StartedAt.Before(from) || !r.StartedAt.Before(to) {
			continue
		}
		clean := ""
		if r.CleanupRequested && r.CleanupError == "" {
			clean = r.CleanedText
		}
		row := []string{r.StartedAt.In(from.Location()).Format("2006-01-02 15:04:05"), r.TargetApp, r.RawText, clean}
		for i := range row {
			row[i] = exportCell(row[i])
		}
		if err := w.Write(row); err != nil {
			return nil, count, err
		}
		count++
	}
	w.Flush()
	return b.Bytes(), count, w.Error()
}
func (m *mainWindow) exportDialog(c *ui.Context) {
	ui.Modal(c.Key("history-export"), &m.exportOpen, func() {
		ui.Column(c).Width(380).Gap(16).Children(func() {
			ui.Text(c, T("v3ExportTitle")).FontSize(17).FontWeight(600)
			for _, field := range []struct {
				key   string
				value *string
			}{{"v3From", &m.exportFrom}, {"v3To", &m.exportTo}} {
				ui.Row(c.Key(field.key)).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, T(field.key)).Grow(1).FontSize(13)
					ui.TextInput(c, field.value).Width(170).Border(.5, settingsGroupLine(c)).Background(fillColor(c.Theme())).Radius(7).Label(T(field.key)).Placeholder("YYYY-MM-DD")
					anchor := iconButton(c.Key("calendar"), icCalendar, T(field.key)).Size(28, 28)
					if anchor.Clicked() {
						m.exportCalendar = field.key
						m.exportMonth, _ = time.ParseInLocation("2006-01-02", *field.value, time.Local)
						if m.exportMonth.IsZero() {
							m.exportMonth = time.Now()
						}
						m.exportMonth = time.Date(m.exportMonth.Year(), m.exportMonth.Month(), 1, 0, 0, 0, 0, time.Local)
					}
					opened := m.exportCalendar == field.key
					ui.Popover(c.Key("date-picker"), anchor, &opened, func() {
						ui.Column(c).Width(238).Padding(8).Gap(6).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								if iconButton(c, icBack, T("v3PrevMonth")).Clicked() {
									m.exportMonth = m.exportMonth.AddDate(0, -1, 0)
								}
								ui.Text(c, m.exportMonth.Format("2006 / 01")).Grow(1).TextAlign(ui.Center).FontSize(13)
								if iconButton(c, icChevron, T("v3NextMonth")).Clicked() {
									m.exportMonth = m.exportMonth.AddDate(0, 1, 0)
								}
							})
							start := time.Date(m.exportMonth.Year(), m.exportMonth.Month(), 1, 0, 0, 0, 0, time.Local)
							ui.Grid(c).Columns(7).FillWidth().Gap(2).Children(func() {
								for i := 0; i < int(start.Weekday()); i++ {
									ui.Box(c).Height(28)
								}
								for d := start; d.Month() == start.Month(); d = d.AddDate(0, 0, 1) {
									day := d.Format("2006-01-02")
									b := ui.Button(c.Key(day), fmt.Sprint(d.Day())).Padding(0).Height(28).Label(day).FontSize(12)
									if day == *field.value {
										b.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
									}
									if b.Clicked() {
										*field.value = day
										m.exportCalendar = ""
									}
								}
							})
						})
					})
					if !opened && m.exportCalendar == field.key {
						m.exportCalendar = ""
					}

				})
			}
			ui.Text(c, T("v3ExportHint")).FontSize(12).TextColor(c.Theme().TextMuted).LineHeight(1.6)
			if m.exportError != "" {
				ui.Text(c, m.exportError).FontSize(12).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, T("cancel")).Clicked() {
					m.exportOpen = false
				}
				if ui.PrimaryButton(c, T("v3Export")).Clicked() {
					from, e1 := time.ParseInLocation("2006-01-02", m.exportFrom, time.Local)
					to, e2 := time.ParseInLocation("2006-01-02", m.exportTo, time.Local)
					if e1 != nil || e2 != nil || from.After(to) {
						m.exportError = T("v3DateError")
						return
					}
					data, n, err := historyCSV(m.a.history.List(""), from, to.AddDate(0, 0, 1))
					if err != nil {
						m.exportError = err.Error()
						return
					}
					if n == 0 {
						m.exportError = T("noRecords")
						return
					}
					home, _ := os.UserHomeDir()
					defaultPath := filepath.Join(home, "Downloads", fmt.Sprintf("Voicemore-history-%s-%s.csv", m.exportFrom, m.exportTo))
					m.exportOpen = false
					// Leave the render/modal cycle before presenting a native save panel.
					go func() {
						path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{DefaultPath: defaultPath, CreateDirectories: true})
						if err == nil && path != "" {
							err = os.WriteFile(path, data, 0600)
						}
						if err != nil {
							m.win.Update(func() { m.exportError = err.Error(); m.exportOpen = true })
						}
					}()

				}
			})
		})
	})
}
func init() {
	for k, v := range map[string][3]string{
		"v3PrevMonth": {"Previous month", "上個月", "上个月"}, "v3NextMonth": {"Next month", "下個月", "下个月"},
		"v3AppUnits": {"Characters / share", "字元 / 佔比", "字符 / 占比"},
		"v3Recent":   {"Recent dictations", "最近聽寫", "最近听写"},
		"v3Fourteen": {"14 days", "近 14 天", "近 14 天"}, "v3Raw": {"Original input", "原文", "原文"}, "v3CopyRaw": {"Copy original", "複製原文", "复制原文"},
		"v3First": {"First word", "首字", "首字"}, "v3Final": {"Stop to final", "結束至定稿", "结束至定稿"}, "v3Insert": {"Final to insert", "定稿至寫入", "定稿至写入"},
		"v3Export": {"Export CSV", "匯出 CSV", "导出 CSV"}, "v3ExportTitle": {"Export history", "匯出歷史記錄", "导出历史记录"}, "v3From": {"Start date", "開始日期", "开始日期"}, "v3To": {"End date", "結束日期", "结束日期"},
		"v3ExportHint": {"Last 30 days by default. Use YYYY-MM-DD; includes all records in the selected dates. CSV contains local time, app, original input and cleaned result.", "預設近 30 天。請輸入 YYYY-MM-DD，匯出日期內全部記錄；包含本地時間、應用、原文與整理結果。", "默认近 30 天。请输入 YYYY-MM-DD，导出日期内全部记录；包含本地时间、应用、原文与整理结果。"},
		"v3DateError":  {"Enter valid dates; the end must not precede the start.", "請輸入有效日期，結束日期不能早於開始日期。", "请输入有效日期，结束日期不能早于开始日期。"},
		"v3CSVTime":    {"Time (local)", "時間（本地）", "时间（本地）"}, "v3CSVApp": {"Application", "使用的應用", "使用的应用"}, "v3CSVResult": {"Cleaned result", "整理後的結果", "整理后的结果"},
	} {
		texts[k] = v
	}
}
