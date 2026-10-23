package main

import (
	"slices"
	"strings"
	"unicode"

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
	ui.Scroll(c).Fill().Padding(24, 32).Gap(24).FontWeight(400).Children(func() {
		ui.Text(c, T("dictTitle")).FontSize(24).FontWeight(600)
		// Only an exception is said here: a dictionary the recognizer
		// cannot take. How terms normally travel needs no sentence.
		if _, _, err := d.HotwordParam(); err != nil {
			ui.Text(c, T("hotOver", err.Error())).FontSize(12).TextColor(t.Danger)
		}

		allEntries := d.Entries("")
		terms, rules := 0, 0
		for _, e := range allEntries {
			if e.Kind == dict.KindRule {
				rules++
			} else {
				terms++
			}
		}
		ui.Row(c).FillWidth().Gap(32).AlignItems(ui.Center).Children(func() {
			for i, v := range []struct {
				label string
				count int
			}{{T("v2Terms"), terms}, {T("v2Rules"), rules}, {T("v2Review"), len(d.Pending())}} {
				b := ui.ButtonBase(c.Key(i)).Padding(0).AlignItems(ui.Start).Children(func() {
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, v.label).FontSize(12).TextColor(t.TextMuted)
						ui.Text(c, statNumber(v.count)).FontSize(26).FontWeight(600)
					})
				})
				if b.Clicked() {
					if i == 2 {
						m.dictReviewJump = true
					} else {
						m.dictKind = i + 1
						m.dPage = 1
						m.dictReviewJump = false
					}
				}
			}
			ui.Spacer(c)
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
		m.dictEditor(c)

		// Candidates learned from corrections: nothing applies before the
		// user accepts it.
		pending := d.Pending()
		pendingSection := ui.Column(c.Key("pending")).Gap(12)
		if m.dictReviewJump {
			pendingSection.ScrollIntoView()
			m.dictReviewJump = false
		}
		pendingSection.Children(func() {
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
					ui.Row(c).FillWidth().Gap(8).Children(func() {
						ui.Text(c, cand.Wrong).FontSize(14).Strikethrough().TextColor(t.TextMuted)
						ui.Text(c, "→").TextColor(t.TextMuted)
						ui.Text(c, cand.Right).Grow(1).MinWidth(0).FontSize(14).FontWeight(600).Selectable()
					})
					source := T(sourceNames[cand.Source])
					if sourceNames[cand.Source] == "" {
						source = cand.Source
					}
					ui.Textf(c, "%s · %d", source, cand.Count).FontSize(11).TextColor(t.TextMuted)
					if cand.Conflict != "" {
						ui.Text(c, cand.Conflict).FontSize(12).TextColor(t.Danger)
					}
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

		// Most recently added first: by the time an entry was added and,
		// for entries of one moment or without a time, by their place in
		// the stored list, later ones first. The stored list is never
		// reordered, so this is the same every time. 100 to a page.
		entries := d.Entries(m.dictQuery)
		entries = slices.DeleteFunc(entries, func(e dict.Entry) bool {
			return m.dictKind == 1 && e.Kind != dict.KindTerm || m.dictKind == 2 && e.Kind != dict.KindRule
		})
		slices.Reverse(entries)
		slices.SortStableFunc(entries, func(a, b dict.Entry) int { return b.CreatedAt.Compare(a.CreatedAt) })
		from, to, pages := pageOf(len(entries), &m.dPage)
		ui.Column(c.Key("entries")).Gap(12).Children(func() {
			ui.Row(c).Gap(8).Children(func() {
				if segmentedSize(c.Key("kind"), &m.dictKind, true, T("v2All")+" "+statNumber(terms+rules), T("v2Terms")+" "+statNumber(terms), T("v2Rules")+" "+statNumber(rules)).Changed() {
					m.dPage = 1
				}
				ui.Spacer(c)
				if searchInput(c, "dict-search", &m.dictQuery, 220).Changed() {
					m.dPage = 1 // a search starts at its first page
				}
			})
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				if len(entries) == 0 {
					ui.Text(c, T("noRecords")).Padding(12, 14).TextColor(t.TextMuted)
				}
				for _, e := range entries[from:to] {
					row := ui.Row(c.Key("entry-"+e.ID)).FillWidth().MinWidth(0).Gap(12).Padding(14, 0).BorderWidth(0, 0, 1, 0).BorderColor(settingsGroupLine(c))
					row.Children(func() {
						kind := T("v2Terms")
						if e.Kind == dict.KindRule {
							kind = T("v2Rules")
						}
						ui.Text(c, kind).FontSize(11).Padding(3, 6).Radius(4).Background(settingsGroupColor(t)).TextColor(t.TextMuted)
						if e.Kind == dict.KindRule {
							text := e.Wrong + "  →  " + e.Term
							if len(e.Context) > 0 {
								text += "   (" + T("onlyWith", strings.Join(e.Context, ", ")) + ")"
							}
							ui.Text(c, text).Grow(1).MinWidth(0).Selectable()
						} else {
							ui.Text(c, e.Term).Grow(1).MinWidth(0).Selectable()
						}
						if !e.CreatedAt.IsZero() {
							ui.Text(c, e.CreatedAt.Format("2006/1/2")).FontSize(12).TextColor(t.TextMuted)
						}
						if ui.Button(c.Key("edit"), T("dictEdit")).Clicked() {
							m.dictEditID, m.newTerm, m.newWrong = e.ID, e.Term, e.Wrong
							m.newContext = strings.Join(e.Context, "; ")
							m.newDictKind = 0
							if e.Kind == dict.KindRule {
								m.newDictKind = 1
							}
							m.dictEditJump = true
						}
						// Deleting an entry undoes it from the next dictation on.
						del := iconButton(c.Key("del"), icTrash, T("remove", e.Term))
						if !row.Hovered() && !row.FocusWithin() {
							del.Opacity(0)
						}
						if del.Clicked() {
							err := d.Delete(e.ID)
							m.report(err, "removed", e.Term)
							if err == nil && m.dictEditID == e.ID {
								m.resetDictEditor()
							}
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

// Context phrases are separated by semicolons, never spaces or commas: a
// Chinese single-character scope may be a complete original sentence.
func ruleContexts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == '；' || r == '\n' })
}

func (m *mainWindow) resetDictEditor() {
	m.newTerm, m.newWrong, m.newContext, m.dictEditID = "", "", "", ""
}

func (m *mainWindow) saveDictEntry() {
	if !m.dictEntryReady() {
		return
	}
	e := dict.Entry{Kind: dict.KindTerm, Term: m.newTerm, Source: "manual"}
	if m.newDictKind == 1 {
		e.Kind, e.Wrong, e.Context = dict.KindRule, m.newWrong, ruleContexts(m.newContext)
	}
	var err error
	message := "added"
	if m.dictEditID == "" {
		err = m.a.dict.Add(e)
	} else {
		err = m.a.dict.Update(m.dictEditID, e)
		message = "dictSaved"
	}
	m.report(err, message, strings.TrimSpace(e.Term))
	if err == nil {
		m.resetDictEditor()
		m.dPage, m.dictKind, m.dictQuery = 1, 0, ""
	}
}

func (m *mainWindow) dictEntryReady() bool {
	term, wrong := strings.TrimSpace(m.newTerm), strings.TrimSpace(m.newWrong)
	return term != "" && !strings.ContainsAny(term, "\r\n") &&
		(m.newDictKind == 0 || wrong != "" && wrong != term && !strings.ContainsAny(wrong, "\r\n"))
}

func (m *mainWindow) dictEditor(c *ui.Context) {
	section := ui.Column(c.Key("add")).Gap(12).FillWidth()
	if m.dictEditJump {
		section.ScrollIntoView()
		m.dictEditJump = false
	}
	section.Children(func() {
		heading, action := "addEntry", "add"
		if m.dictEditID != "" {
			heading, action = "dictEditEntry", "save"
		}
		ui.Text(c, T(heading)).FontSize(15).FontWeight(600)
		segmentedSize(c.Key("add-kind"), &m.newDictKind, true, T("v2Terms"), T("rule"))
		rightLabel := "right"
		if m.newDictKind == 1 {
			rightLabel = "dictReplacement"
		}
		ui.Row(c.Key("fields")).Gap(8).FillWidth().Children(func() {
			if m.newDictKind == 1 {
				groupTextInput(c.Key("wrong"), &m.newWrong, false).Placeholder(T("dictOriginal")).Label(T("dictOriginal")).Grow(1).OnSubmit(m.saveDictEntry)
				ui.Text(c, "→").TextColor(c.Theme().TextMuted)
			}
			groupTextInput(c.Key("term"), &m.newTerm, false).Placeholder(T(rightLabel)).Label(T(rightLabel)).Grow(1).OnSubmit(m.saveDictEntry)
		})
		if m.newDictKind == 1 {
			scopeLabel := "dictScope"
			if r := []rune(strings.TrimSpace(m.newWrong)); len(r) == 1 && unicode.Is(unicode.Han, r[0]) {
				scopeLabel = "dictPhrase"
			}
			groupTextInput(c.Key("scope"), &m.newContext, false).Placeholder(T(scopeLabel)).Label(T(scopeLabel)).FillWidth().OnSubmit(m.saveDictEntry)
		}
		ui.Row(c.Key("actions")).Gap(8).FillWidth().Children(func() {
			ui.Spacer(c)
			if m.dictEditID != "" {
				if ui.Button(c.Key("cancel"), T("cancel")).Clicked() {
					m.resetDictEditor()
				}
			}
			if ui.PrimaryButton(c.Key("ok"), T(action)).Disabled(!m.dictEntryReady()).Clicked() {
				m.saveDictEntry()
			}
		})
	})
}
