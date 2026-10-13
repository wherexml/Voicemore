package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/store"
)

// The history shows, for a record, one transcript and a few actions. The
// other texts, the versions and the timings are under "Details"; reruns
// and deletion are in the menu; the revision editor opens on request.

// duration is a recording's length in compact form: 9s, 2m9s, 2m, 1h2m9s.
// Milliseconds are floored to seconds, a negative length is 0s, and parts
// that are zero are left out, except for 0s itself.
func duration(ms int64) string {
	s := max(ms, 0) / 1000
	h, m, sec := s/3600, s%3600/60, s%60
	out := ""
	if h > 0 {
		out += fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		out += fmt.Sprintf("%dm", m)
	}
	if sec > 0 || out == "" {
		out += fmt.Sprintf("%ds", sec)
	}
	return out
}

// clock is a playback position, m:ss.
func clock(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// statusTag quietly names an exceptional outcome in the record detail only.
func statusTag(c *ui.Context, r store.Record) {
	key := ""
	switch r.Status {
	case store.StatusNotInserted:
		key = "notInsertedTag"
	case store.StatusFailed:
		key = "failedTag"
	case store.StatusCancelled:
		key = "cancelledTag"
	default:
		return
	}
	ui.Text(c, T(key)).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
}

// primaryText is the one text that matters of a record: the latest
// revision, else what was inserted, else what was heard.
func primaryText(r store.Record) (text string, revised bool) {
	for i := len(r.Versions) - 1; i >= 0; i-- {
		if r.Versions[i].Kind == store.KindRevision {
			return r.Versions[i].Text, true
		}
	}
	if r.InsertedText != "" {
		return r.InsertedText, false
	}
	return r.Transcript(), false
}

// showRecord opens History on a record: the search is cleared and the page
// is the one the record is on.
func (m *mainWindow) showRecord(id string) {
	m.goTo("history")
	m.query, m.hPage = "", 1
	for i, r := range m.a.history.List("") {
		if r.ID == id {
			m.hPage = i/pageSize + 1
			m.selectRecord(r)
			return
		}
	}
	m.selected = ""
}

func (m *mainWindow) selectRecord(r store.Record) {
	m.stopPlayer()
	m.selected = r.ID
	m.revBase = r.Transcript()
	m.revText = m.revBase
	m.reviseOpen, m.detailsOpen = false, false
	m.status = nil
}

func (m *mainWindow) stopPlayer() {
	if m.player != nil {
		m.player.Close()
		m.player, m.playerID, m.position = nil, "", 0
	}
}

// openPlayer opens the saved audio of a record: the same samples that
// were sent to the recognizer.
func (m *mainWindow) openPlayer(r store.Record) error {
	if m.player != nil && m.playerID == r.ID {
		return nil
	}
	m.stopPlayer()
	f, err := os.Open(m.a.history.AudioPath(r.AudioFile))
	if err != nil {
		return err
	}
	p, err := audio.OpenStream(f)
	if err != nil {
		f.Close()
		return err
	}
	m.player, m.playerID = p, r.ID
	return nil
}

// searchInput is the search field of a list. MyGo v0.4.8's ui.SearchField
// always shows the English "Search" while empty: it sets that placeholder
// on its inner input, and Placeholder on the field it returns does not
// reach it. A plain text input takes Placeholder, so the visible prompt
// follows the language; the label names it for assistive technology.
func searchInput(c *ui.Context, query *string) ui.Element {
	return ui.TextInput(c, query).Placeholder(T("search")).Label(T("search"))
}

func (m *mainWindow) historyView(c *ui.Context) {
	t := c.Theme()
	records := m.a.history.List(m.query)
	// The time labels are relative to now. A frame at the next minute
	// redraws them as the minute or the day turns; it only draws, so the
	// selection, the playback and what is being typed stay as they are.
	now := time.Now()
	c.After(untilNextMinute(now))
	// 100 to a page, newest first as the history keeps them. The detail
	// only ever shows a record that is on the visible page.
	from, to, pages := pageOf(len(records), &m.hPage)
	visible := records[from:to]
	if m.selected != "" && !slices.ContainsFunc(visible, func(r store.Record) bool { return r.ID == m.selected }) {
		m.stopPlayer()
		m.selected = ""
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(290).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Column(c).Padding(12).Children(func() {
				if searchInput(c.Key("history-search"), &m.query).Changed() {
					m.hPage = 1 // a search starts at its first page
				}
			})
			if len(records) == 0 {
				ui.Text(c, T("noRecords")).Padding(24).TextColor(t.TextMuted)
			}
			ui.Scroll(c).Grow(1).Padding(0, 8, 12).Gap(2).Children(func() {
				for _, r := range visible {
					bg := ui.Transparent
					if r.ID == m.selected {
						bg = t.Selection
					}
					row := ui.Column(c.Key(r.ID)).Padding(9, 10).Radius(8).Gap(3).Background(bg).Cursor(ui.CursorPointer)
					row.Children(func() {
						ui.Row(c).Gap(6).Children(func() {
							// The time on the left and duration at the edge.
							ui.Text(c, timeLabel(r.StartedAt, now)).FontSize(11).TextColor(t.TextMuted).SingleLine()
							ui.Spacer(c)
							ui.Text(c, duration(r.DurationMs)).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
						text, _ := primaryText(r)
						if text == "" {
							text = T("emptyText")
						}
						ui.Text(c, text).FontSize(14).LineHeight(1.65).MaxLines(2)
					})
					if row.Clicked() {
						m.selectRecord(r)
					}
				}
			})
			pager(c, "pager", &m.hPage, pages)
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			r, ok := m.a.history.Get(m.selected)
			if !ok {
				ui.Text(c, T("pickRecord")).Padding(40, 26).TextColor(t.TextMuted)
				return
			}
			ui.Scroll(c.Key("detail-"+r.ID)).Fill().Padding(22, 26).Gap(14).Children(func() { m.detailView(c, r) })
		})
	})
}

// textBlock is one of a record's texts under "Details", with a small
// button copying it.
func (m *mainWindow) textBlock(c *ui.Context, key, title, text, empty string) {
	t := c.Theme()
	ui.Column(c.Key(key)).Gap(2).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(t.TextMuted).Grow(1)
			if text != "" && iconButton(c.Key("copy"), icCopy, T("copyOf", title)).Clicked() {
				mygo.Clipboard.WriteText(text)
				m.say("copied")
			}
		})
		if text == "" {
			ui.Text(c, empty).FontSize(14).LineHeight(1.65).TextColor(t.TextMuted)
		} else {
			ui.Text(c, text).FontSize(14).LineHeight(1.65).Selectable()
		}
	})
}

func (m *mainWindow) detailView(c *ui.Context, r store.Record) {
	t := c.Theme()
	primary, revised := primaryText(r)

	ui.Row(c.Key("head")).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, timeLabel(r.StartedAt, time.Now())).FontSize(15).Bold()
			meta := duration(r.DurationMs)
			if r.TargetApp != "" {
				meta += " · " + r.TargetApp
			}
			if revised {
				meta += " · " + T("revision")
			}
			ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted)
		})
		statusTag(c, r)
	})

	// Playback: an icon, a slim track and the time, when there is audio.
	ui.Row(c.Key("player")).Gap(8).Children(func() {
		switch {
		case r.HasAudio():
			playing := m.player != nil && m.playerID == r.ID && m.player.Playing()
			svg, label := icPlay, T("play")
			if playing {
				svg, label = icPause, T("pause")
			}
			if iconButton(c.Key("play"), svg, label).Clicked() {
				if err := m.openPlayer(r); err != nil {
					m.say("cantPlay", err.Error())
				} else if playing {
					m.player.Pause()
				} else {
					m.player.Play()
				}
			}
			total := max(float64(r.DurationMs)/1000, 0.1)
			if m.player != nil && m.playerID == r.ID && playing {
				m.position = m.player.Position().Seconds()
				c.After(100 * time.Millisecond)
			}
			if ui.Slider(c.Key("seek"), &m.position, 0, total).Grow(1).Label(T("seek")).Changed() {
				if err := m.openPlayer(r); err == nil {
					m.player.Seek(time.Duration(m.position * float64(time.Second)))
				}
			}
			ui.Textf(c, "%s / %s", clock(int64(m.position*1000)), clock(r.DurationMs)).FontSize(11).TextColor(t.TextMuted).SingleLine()
		case r.AudioRemoved != "":
			ui.Text(c, T("noAudioGone", r.AudioRemoved)).FontSize(12).TextColor(t.TextMuted)
		default:
			ui.Text(c, T("noAudioOff")).FontSize(12).TextColor(t.TextMuted)
		}
	})

	if primary == "" {
		ui.Text(c, T("emptyText")).FontSize(15).TextColor(t.TextMuted)
	} else {
		ui.Text(c, primary).FontSize(14).LineHeight(1.65).Selectable()
	}

	// The essential actions; the rest is in the menu.
	id, base := r.ID, r.Transcript()
	working := m.busy != nil
	ui.Row(c.Key("actions")).Gap(8).Children(func() {
		if labelButton(c.Key("copy"), icCopy, T("copy")).Disabled(primary == "").Clicked() {
			mygo.Clipboard.WriteText(primary)
			m.say("copied")
		}
		if labelButton(c.Key("revise"), icPencil, T("reviseShort")).Clicked() {
			m.reviseOpen = !m.reviseOpen
		}
		ui.Spacer(c)
		ui.Button(c.Key("more"), "").Label(T("more")).Tooltip(T("more")).Padding(4, 6).
			Children(func() { ui.Icon(c, icMore).FontSize(14) }).
			Menu(func(menu *ui.Menu) {
				if menu.Item(T("retranscribe")).Disabled(!r.HasAudio() || working).Chosen() {
					m.do("retranscribing", func() error { return m.a.ctrl.Retranscribe(id) }, "versionAdded")
					m.detailsOpen = true
				}
				// Re-cleanup uses the cleanup model of the settings, as
				// the dictation's own cleanup does.
				if menu.Item(T("reclean")).Disabled(base == "" || working).Chosen() {
					m.do("recleaning", func() error { return m.a.ctrl.Recleanup(id, base) }, "versionAdded")
					m.detailsOpen = true
				}
				menu.Separator()
				if menu.Item(T("del")).Chosen() {
					m.stopPlayer()
					err := m.a.history.Delete(id)
					m.selected = ""
					m.report(err, "deleted")
				}
			})
	})

	// A revision: saved as a version, and compared with its base to
	// propose dictionary candidates, which wait for confirmation.
	if m.reviseOpen {
		ui.Column(c.Key("revise-box")).Gap(8).Children(func() {
			ui.TextArea(c.Key("revision"), &m.revText).Lines(3, 10).Label(T("reviseShort"))
			ui.Text(c, T("reviseHint")).FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).Children(func() {
				if iconButton(c.Key("reset"), icUndo, T("restore")).Disabled(m.revText == m.revBase).Clicked() {
					m.revText = m.revBase
				}
				ui.Spacer(c)
				if ui.PrimaryButton(c.Key("save"), T("saveRevision")).Disabled(strings.TrimSpace(m.revText) == "" || m.revText == m.revBase).Clicked() {
					n, err := m.a.ctrl.Revise(r.ID, m.revBase, m.revText)
					switch {
					case err != nil:
						m.say("failed", err.Error())
					case n > 0:
						m.say("revSaved", n)
						m.reviseOpen = false
					default:
						m.say("revSavedNone")
						m.reviseOpen = false
					}
				}
			})
		})
	}

	ui.Divider(c)
	ui.Collapsible(c.Key("details"), T("details"), &m.detailsOpen, func() {
		ui.Column(c).Gap(12).Padding(8, 0, 0).Children(func() {
			m.textBlock(c, "raw", T("raw"), r.RawText, T("emptyText"))
			if r.CorrectedText != "" {
				m.textBlock(c, "corrected", T("corrected")+" · "+strings.Join(r.AppliedRules, ", "), r.CorrectedText, "")
			}
			title := T("cleaned")
			if r.CleanupModel != "" {
				title += " · " + r.CleanupModel
			}
			switch {
			case !r.CleanupRequested:
				m.textBlock(c, "cleaned", title, "", T("cleanOff"))
			case r.CleanupError != "":
				m.textBlock(c, "cleaned", title, "", T("cleanFailedWhy", r.CleanupError))
			default:
				m.textBlock(c, "cleaned", title, r.CleanedText, T("emptyText"))
			}
			m.textBlock(c, "inserted", T("insertedText"), r.InsertedText, T("nothingInserted"))
			for _, v := range r.Versions {
				kind := map[string]string{store.KindRetranscribe: "rerun", store.KindRecleanup: "recleaned", store.KindRevision: "revision"}[v.Kind]
				title := T(kind) + " · " + timeLabel(v.At, time.Now())
				if v.Model != "" {
					title += " · " + v.Model
				}
				if v.Note != "" && v.Kind != store.KindRevision {
					title += " · " + v.Note
				}
				m.textBlock(c, "v-"+v.ID, title, v.Text, T("emptyText"))
			}
			if r.Status == store.StatusInserted && r.StatusDetail != "" {
				ui.Text(c, r.StatusDetail).FontSize(11).TextColor(t.TextMuted)
			}
			for _, note := range []string{r.HotwordNote, r.ObserveNote} {
				if note != "" {
					ui.Text(c, note).FontSize(11).TextColor(t.TextMuted)
				}
			}
			ui.Text(c, T("timing", r.Timing.FirstPartialMs, r.Timing.StopToFinalMs, r.Timing.CleanupMs, r.Timing.FinalToInsertMs)).FontSize(11).TextColor(t.TextMuted)
		})
	})
}
