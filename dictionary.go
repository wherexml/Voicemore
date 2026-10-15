package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"voiceinput/internal/dict"
)

func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '、' })
}

var sourceNames = map[string]string{"history-revision": "srcRevision", "observed-edit": "srcObserved"}

func (m *mainWindow) dictView(c *ui.Context) {
	t := c.Theme()
	d := m.a.dict
	ui.Scroll(c).Fill().Padding(28, 22).Gap(28).FontWeight(400).Children(func() {
		// Only an exception is said here: a dictionary the recognizer
		// cannot take. How terms normally travel needs no sentence.
		if _, _, err := d.HotwordParam(); err != nil {
			ui.Text(c, T("hotOver", err.Error())).FontSize(12).TextColor(t.Danger)
		}

		// Candidates learned from corrections: nothing applies before the
		// user accepts it.
		pending := d.Pending()
		ui.Column(c.Key("pending")).Gap(12).Children(func() {
			ui.Text(c, T("pending", len(pending))).FontSize(12).TextColor(t.TextMuted)
			if len(pending) == 0 {
				ui.Text(c, T("noPending")).TextColor(t.TextMuted)
			}
			// Three to a page. A page emptied by an accept or an ignore falls
			// back to the last one that exists. Original input stays visible.
			const candPerPage = 3
			cpages := max(1, (len(pending)+candPerPage-1)/candPerPage)
			m.cPage = min(max(1, m.cPage), cpages)
			cfrom := (m.cPage - 1) * candPerPage
			for _, cand := range pending[cfrom:min(len(pending), cfrom+candPerPage)] {
				ui.Column(c.Key("cand-"+cand.ID)).Padding(18, 20).Radius(14).Gap(10).Background(settingsGroupColor(t)).Children(func() {
					ui.Textf(c, "%s  →  %s", cand.Wrong, cand.Right).FontSize(14).Selectable()
					if cand.Sentence != "" {
						ui.Text(c, T("candOriginal")+cand.Sentence).FontSize(12).TextColor(softText(t)).Selectable()
					}
					ui.Row(c).Gap(8).Padding(12, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(settingsGroupLine(c)).Children(func() {
						if ui.Button(c.Key("hint"), T("acceptHint")).Clicked() {
							m.report(d.Accept(cand.ID, dict.KindTerm, "", cand.Right, nil), "acceptedHint", cand.Right)
							m.dPage = 1 // the new entry is first
						}
						if ui.Button(c.Key("rule"), T("acceptRule")).Clicked() {
							m.report(d.Accept(cand.ID, dict.KindRule, cand.Wrong, cand.Right, nil), "acceptedRule", cand.Wrong, cand.Right)
							m.dPage = 1
						}
						ui.Spacer(c)
						if ui.Button(c.Key("ignore"), T("ignore")).Clicked() {
							m.report(d.Ignore(cand.ID), "ignored")
						}
					})
				})
			}
			pager(c, "cand-pager", &m.cPage, cpages)
		})

		// Manual additions are keywords, one entry at a time.
		ui.Column(c.Key("add")).Gap(12).Padding(18, 20).Radius(14).Background(settingsGroupColor(t)).Children(func() {
			ui.Text(c, T("addEntry")).FontSize(12).TextColor(t.TextMuted)
			term := strings.TrimSpace(m.newTerm)
			blocked := term == "" || strings.ContainsAny(term, "\r\n")
			ui.Row(c.Key("fields")).Gap(8).MaxWidth(460).Children(func() {
				groupTextInput(c.Key("term"), &m.newTerm, false).Placeholder(T("right")).Label(T("right")).Grow(1)
				if ui.PrimaryButton(c.Key("ok"), "").Label(T("add")).Disabled(blocked).Children(func() {
					ui.Icon(c, icPlus).FontSize(13)
					ui.Text(c, T("add")).SingleLine()
				}).Clicked() {
					e := dict.Entry{Kind: dict.KindTerm, Term: term, Source: "manual"}
					err := d.Add(e)
					m.report(err, "added", term)
					m.dPage = 1
					if err == nil {
						m.newTerm = ""
					}
				}
			})
			ui.Row(c.Key("io")).Gap(8).Children(func() {
				if labelButton(c.Key("import"), icImport, T("importFile")).Tooltip(T("importHelp")).Clicked() {
					go func() {
						paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: m.win, Title: T("importFile"), Message: T("importHelp"), Filters: []mygo.FileFilter{{Name: "TXT", Extensions: []string{"txt"}}}})
						if err == nil && len(paths) > 0 {
							m.win.Update(func() { m.importFile(paths[0]) })
						}
					}()
				}
				if labelButton(c.Key("export"), icExport, T("exportDict")).Clicked() {
					go func() {
						path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: m.win, DefaultPath: "voice-input-dictionary.txt", Filters: []mygo.FileFilter{{Name: "TXT", Extensions: []string{"txt"}}}})
						if err == nil && path != "" {
							err = d.Export(path)
							m.win.Update(func() { m.report(err, "exported", path) })
						}
					}()
				}
			})
		})

		// Most recently added first: by the time an entry was added and,
		// for entries of one moment or without a time, by their place in
		// the stored list, later ones first. The stored list is never
		// reordered, so this is the same every time. 100 to a page.
		entries := d.Entries(m.dictQuery)
		slices.Reverse(entries)
		slices.SortStableFunc(entries, func(a, b dict.Entry) int { return b.CreatedAt.Compare(a.CreatedAt) })
		from, to, pages := pageOf(len(entries), &m.dPage)
		ui.Column(c.Key("entries")).Gap(12).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, T("entries", len(entries))).FontSize(12).TextColor(t.TextMuted)
				ui.Spacer(c)
				if searchInput(c, "dict-search", &m.dictQuery, 220).Changed() {
					m.dPage = 1 // a search starts at its first page
				}
			})
			ui.Grid(c).Columns(2).Gap(12).Children(func() {
				if len(entries) == 0 {
					ui.Text(c, T("noRecords")).ColumnSpan(2).Padding(12, 14).TextColor(t.TextMuted)
				}
				for _, e := range entries[from:to] {
					ui.Row(c.Key("entry-"+e.ID)).MinWidth(0).Gap(12).Padding(12, 14).Radius(10).Background(settingsGroupColor(t)).Children(func() {
						if e.Kind == dict.KindRule {
							text := e.Wrong + "  →  " + e.Term
							if len(e.Context) > 0 {
								text += "   (" + T("onlyWith", strings.Join(e.Context, ", ")) + ")"
							}
							ui.Text(c, text).Grow(1).MinWidth(0).Selectable()
						} else {
							ui.Text(c, e.Term).Grow(1).MinWidth(0).Selectable()
						}
						// Deleting an entry undoes it from the next dictation on.
						if iconButton(c.Key("del"), icTrash, T("remove", e.Term)).Clicked() {
							m.report(d.Delete(e.ID), "removed", e.Term)
						}
					})
				}
			})
			pager(c, "pager", &m.dPage, pages)
		})
	})
}

// importFile imports terms and says how many of the file's were added:
// all of them are read, none is dropped silently.
func (m *mainWindow) importFile(path string) {
	m.dPage = 1
	added, total, err := m.a.dict.Import(path)
	m.report(err, "imported", total, added, total-added)
}
