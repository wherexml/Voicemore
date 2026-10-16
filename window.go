package main

import (
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
)

// mainWindow is the app's window: a narrow icon rail and three pages, laid
// out as mock/index.html specifies, over the real dictation, history and
// dictionary. Settings are a window of their own (prefs.go), opened from
// the rail's last icon. Its fields are read and written on the main thread.
type mainWindow struct {
	a          *app
	win        *mygo.Window
	page       string      // "home", "history" or "dict"
	loadErrs   [][2]string // what could not be read at launch: text key, cause
	status     func() string
	busy       func() string // the rerun in progress, if any
	updateOpen bool
	starting   bool // the 3-second countdown before a dictation

	// Shared copy feedback for the Home and History timelines.
	recentCopied map[string]time.Time // record ID → last successful copy

	// V3 home and export state.
	homeRange                         int
	exportOpen                        bool
	exportCalendar                    string
	exportMonth                       time.Time
	exportFrom, exportTo, exportError string
	waveID                            string
	wavePeaks                         []float32
	waveLoading                       bool

	// History.
	hRange        int
	hApp, hStatus string
	hHour         time.Time
	heatFocus     map[string]*ui.Handle
	tileHover     map[string]time.Time
	query         string
	hPage         int // from 1
	selected      string
	reviseOpen    bool
	detailsOpen   bool   // "Details" is left open: the saved preference
	detailsPeek   string // the record whose details a rerun opened, whatever the preference
	revBase       string // the text the revision started from
	revText       string
	player        *audio.Player
	playerID      string
	position      float64 // seconds

	legacyAppIDs map[string]string

	// Dictionary.
	dictKind       int
	dictReviewJump bool
	dictQuery      string
	dPage          int
	cPage          int // the page of the candidates, three to a page
	newTerm        string
	contexts       map[string]string // candidate ID → context words typed for it
	candOpen       map[string]bool   // candidate ID → its context sentence is showing
}

const pageSize = 100 // items per page of History and of the Dictionary

func newMainWindow(a *app) *mainWindow {
	m := &mainWindow{a: a, page: "home", hRange: 1, hPage: 1, dPage: 1, cPage: 1, detailsOpen: a.settings.Get().HistoryDetailsOpen, contexts: map[string]string{}, candOpen: map[string]bool{}}
	m.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Voicemore",
		Width:         1040,
		Height:        760,
		MinWidth:      760,
		MinHeight:     520,
		Hidden:        true,
		StateKey:      "main",
		TitleBarStyle: mygo.TitleBarHidden, // the window controls sit in the rail
		Content:       ui.View(m.view),
	})
	// Closing hides it: dictation keeps working from the trigger key.
	m.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			m.stopPlayer()
			m.win.Hide()
		}
	})
	return m
}

// show brings the app to the front: the first-run guide while it is
// open, which stands in for this window, and otherwise this window.
func (m *mainWindow) show() {
	if m.a.onboard.visible() {
		m.a.onboard.win.Show()
		m.a.onboard.win.Focus()
		return
	}
	m.reveal()
}

// reveal shows this window, whatever else is open.
func (m *mainWindow) reveal() {
	m.win.Show()
	m.win.Focus()
}

// refresh draws the window again, from any goroutine.
func (m *mainWindow) refresh() {
	if m != nil && m.win != nil {
		m.win.Invalidate()
	}
}

// say sets the status line to a text that follows the language.
func (m *mainWindow) say(key string, args ...any) {
	m.status = func() string { return T(key, args...) }
}

// report shows an error, or the outcome when there is none.
func (m *mainWindow) report(err error, key string, args ...any) {
	if err != nil {
		m.say("failed", err.Error())
	} else {
		m.say(key, args...)
	}
}

// do runs work off the main thread and shows its outcome.
func (m *mainWindow) do(busyKey string, fn func() error, okKey string) {
	m.busy = func() string { return T(busyKey) }
	go func() {
		err := fn()
		m.win.Update(func() {
			m.busy = nil
			m.report(err, okKey)
		})
	}()
}

// goTo changes the page.
func (m *mainWindow) goTo(page string) {
	if m.page != page {
		m.stopPlayer()
		m.status = nil
		m.page = page
		m.clearHistoryFilters()
		m.selected = ""
	}
}

var pageTitles = map[string]string{"home": "navHome", "history": "navHistory", "dict": "navDict"}

// railButton is a 44 px round button of the rail: an icon only, named by
// its label, which is also its tooltip.
func railButton(c *ui.Context, key string, svg *ui.SVG, label string, selected bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(44, 44).Radius(22).Justify(ui.Center).AlignItems(ui.Center).
		Label(label).Tooltip(label)
	b.Children(func() { ui.Icon(c, svg).Size(19, 19).TextColor(railText(t)) })
	switch {
	case selected:
		b.Background(railSelected(t))
	case b.Hovered():
		b.Background(railHover(t))
	}
	return b.Clicked()
}

func (m *mainWindow) view(c *ui.Context) {
	t := useTheme(c) // the mock's palette; every c.Theme() below returns it
	bar := c.TitleBar()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		// The rail: 78 px, icons only. The window controls take its top;
		// the three pages are centered in its height, and Settings is
		// anchored at the bottom.
		ui.Column(c.Key("rail")).Width(78).Background(railColor(t)).BorderWidth(0, 1, 0, 0).BorderColor(railLine(t)).
			AlignItems(ui.Center).Children(func() {
			ui.Box(c).Height(max(bar.Height, 116)).FillWidth().DragWindow()
			ui.Spacer(c)
			ui.Column(c).Gap(10).AlignItems(ui.Center).Children(func() {
				if railButton(c, "home", railHome, T("navHome"), m.page == "home") {
					m.goTo("home")
				}
				if railButton(c, "history", railHistory, T("navHistory"), m.page == "history") {
					m.goTo("history")
				}
				if railButton(c, "dict", railDict, T("navDict"), m.page == "dict") {
					m.goTo("dict")
				}
			})
			ui.Spacer(c)
			m.updateView(c)
			ui.Box(c).Height(10)
			// Settings is not a page: it opens the settings window, and
			// the page here stays as it is.
			if railButton(c, "settings", railSettings, T("navSettings"), m.a.prefs.visible()) {
				m.a.prefs.toggle("")
			}
			ui.Box(c).Height(16)
		})

		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			m.problems(c)
			ui.Column(c.Key("page-" + m.page)).Grow(1).MinHeight(0).Children(func() {
				switch m.page {
				case "history":
					m.historyView(c)
				case "dict":
					m.dictView(c)
				default:
					m.homeView(c)
				}
			})
			if m.busy != nil || m.status != nil {
				ui.Row(c).Gap(8).Padding(8, 28).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
					if m.busy != nil {
						ui.Spinner(c)
						ui.Text(c, m.busy()).FontSize(12).TextColor(t.TextMuted)
					} else {
						ui.Text(c, m.status()).FontSize(12).TextColor(t.TextMuted).Selectable()
					}
				})
			}
		})
	})
}

// problems lists what needs the user's attention, on every page.
func (m *mainWindow) problems(c *ui.Context) {
	t := c.Theme()
	var lines []string
	for _, e := range m.loadErrs {
		lines = append(lines, T("errLoad", T(e[0]), e[1]))
	}
	if m.a.triggerErr != "" {
		lines = append(lines, T("errTrigger", m.a.triggerErr))
	}
	m.a.keyMu.Lock()
	if m.a.keyErr != "" {
		lines = append(lines, m.a.keyErr)
	}
	m.a.keyMu.Unlock()
	if len(lines) == 0 {
		return
	}
	ui.Column(c).Padding(10, 28).Gap(4).Children(func() {
		for _, l := range lines {
			ui.Text(c, l).FontSize(12).TextColor(t.Danger).Selectable()
		}
	})
}

// pageOf cuts a list of n items to the page asked for, clamped to what
// exists: a search or a deletion can never leave an empty page.
func pageOf(n int, page *int) (from, to, pages int) {
	pages = max(1, (n+pageSize-1)/pageSize)
	*page = min(max(1, *page), pages)
	from = (*page - 1) * pageSize
	return from, min(n, from+pageSize), pages
}

// pager is the previous / "page a of b" / next control. A list of one
// page, which is any list of up to 100 items, gets none.
func pager(c *ui.Context, key string, page *int, pages int) {
	if pages < 2 {
		return
	}
	t := c.Theme()
	ui.Row(c.Key(key)).Gap(8).Padding(8, 12).Justify(ui.Center).Children(func() {
		if iconButton(c.Key("prev"), icBack, T("pagePrev")).Disabled(*page <= 1).Clicked() {
			*page--
		}
		ui.Text(c, T("pageOf", *page, pages)).FontSize(12).TextColor(t.TextMuted).SingleLine()
		if iconButton(c.Key("next"), icChevron, T("pageNext")).Disabled(*page >= pages).Clicked() {
			*page++
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }
