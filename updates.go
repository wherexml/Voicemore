package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"voiceinput/internal/config"
	"voiceinput/internal/session"
)

// All state below belongs to the UI thread; workers post their results to it.
type updateState struct {
	available                        *mygo.Update
	checking, installing, restarting bool
	status, detail                   string
	downloaded, total                int64
	cancel                           context.CancelFunc
	notified                         string
	statePath                        string
}

func (a *app) startUpdates(dir string) {
	a.updates.statePath = filepath.Join(dir, "update-notification.json")
	var saved struct{ Version string }
	if b, err := os.ReadFile(a.updates.statePath); err == nil {
		_ = json.Unmarshal(b, &saved)
	}
	a.updates.notified = saved.Version
	mygo.App.OnNotificationClick(func(id string) {
		if id == "voicemore-update" {
			a.main.goTo("home")
			a.main.updateOpen = true
			a.main.show()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		cancel()
		if a.updates.cancel != nil {
			a.updates.cancel()
		}
	})
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				mygo.RunOnMain(a.checkUpdates)
				timer.Reset(time.Hour)
			}
		}
	}()
}

func (a *app) checkUpdates() {
	u := &a.updates
	if a.quitting || u.checking || u.installing {
		return
	}
	if !mygo.Updater.Enabled() {
		u.status = "updateUnavailable"
		a.main.refresh()
		return
	}
	u.checking, u.detail = true, ""
	u.status = "updateChecking"
	a.main.refresh()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		up, err := mygo.Updater.Check(ctx)
		mygo.RunOnMain(func() {
			if a.quitting {
				return
			}
			u.checking = false
			if err != nil {
				u.status = "updateCheckFailed"
				u.detail = err.Error()
			} else {
				u.available = up
				u.status = "updateCurrent"
				if up != nil {
					u.status = "updateAvailable"
					a.tray.SetToolTip("Voicemore · " + T("updateFound", up.Version))
					if u.notified != up.Version {
						u.notified = up.Version
						_ = config.WriteJSON(u.statePath, struct{ Version string }{up.Version})
						// Home and tray remain available if the OS denies notifications.
						n := mygo.NewNotification(mygo.NotificationOptions{ID: "voicemore-update", Title: "Voicemore", Body: T("updateFound", up.Version), Silent: true})
						go func() { _ = n.Show() }()
					}
				}
			}
			a.main.refresh()
		})
	}()
}

func (a *app) updateBusy() bool {
	phase := a.ctrl.View().Phase
	return (phase >= session.Preparing && phase <= session.Inserting) || a.localASR.Snapshot().Busy()
}

func (a *app) installUpdate() {
	u := &a.updates
	if u.available == nil || u.installing || u.checking {
		return
	}
	if a.updateBusy() {
		u.status = "updateBusy"
		a.main.refresh()
		return
	}
	u.installing, u.status, u.detail = true, "updateDownloading", ""
	u.downloaded, u.total = 0, 0
	// Avoid a new dictation while installation/relaunch is in progress.
	a.bindTrigger()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	u.cancel = cancel
	up := u.available
	a.main.refresh()
	go func() {
		err := up.Install(ctx, func(done, total int64) {
			mygo.RunOnMain(func() { u.downloaded, u.total = done, total; a.main.refresh() })
		})
		cancel()
		mygo.RunOnMain(func() {
			u.cancel = nil
			if err != nil {
				u.installing = false
				u.status, u.detail = "updateInstallFailed", err.Error()
				if errors.Is(err, context.Canceled) {
					u.status, u.detail = "updateCancelled", ""
				}
				a.bindTrigger()
				a.main.refresh()
				return
			}
			u.status = "updateRestarting"
			a.main.refresh()
			a.restartAfterUpdate()
		})
	}()
}

func (a *app) restartAfterUpdate() {
	if a.updateBusy() {
		time.AfterFunc(time.Second, func() { mygo.RunOnMain(a.restartAfterUpdate) })
		return
	}
	// Relaunch through LaunchServices after this process exits. Direct exec
	// after a bundle swap can leave macOS associating UI with the removed bundle.
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		cmd := exec.Command("/bin/sh", "-c", `
old_pid=$1
bundle=$2
attempt=0
while kill -0 "$old_pid" 2>/dev/null; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 60 ] || exit 1
  sleep 1
done
exec /usr/bin/open -n "$bundle"
`, "voicemore-relaunch", strconv.Itoa(os.Getpid()), bundle)
		cmd.Dir = "/"
		err = cmd.Start()
		if err == nil {
			_ = cmd.Process.Release()
		}
	}
	if err != nil {
		a.updates.installing = false
		a.updates.status, a.updates.detail = "updateRestartFailed", err.Error()
		a.bindTrigger()
		a.main.refresh()
		return
	}
	a.updates.restarting = true
	mygo.App.Quit()
}

// updatePresentation supplies the rail tooltip and the optional details panel.
func (m *mainWindow) updatePresentation() (string, string, string) {
	u := &m.a.updates
	switch {
	case u.status == "updateRestarting":
		return T("updateRestartShort"), T("updateRestartShort"), T("updateRestarting")
	case u.installing && u.total > 0 && u.downloaded >= u.total:
		return T("updateVerifyShort"), T("updateVerifyShort"), T("updateVerifyNote")
	case u.installing:
		label := T("updateDownloadShort")
		if u.total > 0 {
			label = fmt.Sprintf("%s %d%%", label, min(int64(99), 100*u.downloaded/u.total))
		}
		return label, label, T("updatePauseNote")
	case u.checking:
		return "", T("updateChecking"), ""
	case u.status == "updateCheckFailed" || u.status == "updateInstallFailed" || u.status == "updateRestartFailed":
		return T("updateAttention"), T("updateAttention"), T(u.status)
	case u.available != nil:
		detail := T("updateReadyNote")
		if m.a.updateBusy() {
			detail = T("updateBusy")
		}
		if u.status == "updateCancelled" {
			detail = T("updateCancelled")
		}
		return T("updateNewShort"), T("updateFound", u.available.Version), detail
	case u.status == "updateUnavailable":
		return "", T("updateCheck"), T("updateUnavailable")
	case u.status == "updateCurrent":
		return "", T("updateCurrent"), ""
	default:
		return "", T("updateCheck"), ""
	}
}

func (m *mainWindow) updateView(c *ui.Context) {
	u := &m.a.updates
	t := c.Theme()
	label, title, note := m.updatePresentation()
	// Scope the brand accent to the updater; other controls keep their palette.
	brand := *t
	brand.Accent = ui.Hex("#343b85")
	brand.AccentHover = ui.Hex("#454e9c")
	brand.AccentPressed = ui.Hex("#252d67")
	brand.AccentText = ui.Hex("#f7faff")
	c.SetTheme(&brand)
	defer c.SetTheme(t)
	active := u.available != nil || u.installing || u.checking
	attention := u.detail != ""
	tooltip := title
	if label != "" {
		tooltip = label
	}
	if u.available != nil && !u.installing && !u.checking && m.a.updateBusy() {
		tooltip = T("updateBusy")
	}
	anchor := ui.ButtonBase(c.Key("update-trigger")).Size(44, 44).Radius(22).
		Justify(ui.Center).AlignItems(ui.Center).Label(tooltip)
	if anchor.Hovered() || m.updateOpen {
		anchor.Background(railHover(t))
	}
	anchor.Children(func() {
		color := t.TextMuted
		face := ui.Box(c).Size(36, 36).Radius(18).Justify(ui.Center).AlignItems(ui.Center)
		if active || attention {
			face.Background(brand.Accent)
			color = brand.AccentText
		}
		face.Children(func() {
			switch {
			case u.installing || u.checking:
				progress := -1.0
				if u.installing && u.status == "updateDownloading" && u.total > 0 && u.downloaded < u.total {
					progress = max(0.0, float64(u.downloaded)/float64(u.total))
				}
				angle := -math.Pi / 2
				if progress < 0 && !c.Preferences().ReduceMotion {
					angle += float64(c.Now().UnixMilli()%1200) / 1200 * 2 * math.Pi
					c.After(30 * time.Millisecond)
				}
				ui.Box(c).Size(24, 24).Draw(func(p *ui.Painter, r ui.Rect) { drawUpdateRing(p, r, progress, angle) })
			case attention:
				actionIcon(c, railUpdate, 16, color)
			case u.available != nil:
				ui.Icon(c, icImport).FontSize(24).TextColor(color)
			default:
				actionIcon(c, railUpdate, 16, color)
			}
		})
	})
	if !m.updateOpen {
		ui.TooltipBase(c.Key("update-tooltip"), anchor, func(tip ui.Element) {
			tip.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8).
				Padding(8, 12).Radius(9).Background(ui.Hex("#30312d")).Children(func() {
				ui.Text(c, tooltip).FontSize(13).TextColor(ui.Hex("#ffffff")).SingleLine()
			})
		})
	}
	if anchor.Clicked() {
		if u.available != nil && !u.installing && !u.checking && !attention && !m.a.updateBusy() {
			m.updateOpen = false
			m.a.installUpdate()
		} else {
			m.updateOpen = !m.updateOpen
		}
	}

	ui.PopoverBase(c.Key("update-panel"), anchor, &m.updateOpen, func(panel ui.Element) {
		panel.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8).
			Padding(8).Radius(12).Background(t.Background).Border(1, t.Border).
			Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.18))
		ui.Column(c).Width(300).Gap(14).Padding(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(600).Grow(1)
				if iconButton(c, icClear, T("updateClose")).Clicked() {
					m.updateOpen = false
				}
			})
			ui.Text(c, T("updateVersion", mygo.App.Version())).FontSize(11).TextColor(t.TextMuted)
			if note != "" {
				ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
			}
			if u.installing && u.status != "updateRestarting" {
				progress := -1.0
				if u.total > 0 {
					progress = min(1.0, float64(u.downloaded)/float64(u.total))
				}
				ui.Progress(c, progress)
			}
			if u.detail != "" {
				// Technical diagnostics remain available without crowding the main page.
				ui.Text(c, T("updateErrorDetails")).FontSize(11).TextColor(t.TextMuted).Tooltip(u.detail)
			}
			switch {
			case u.status == "updateRestartFailed":
				if ui.PrimaryButton(c, T("updateRestartAction")).Clicked() {
					u.installing, u.status, u.detail = true, "updateRestarting", ""
					m.a.bindTrigger()
					m.a.restartAfterUpdate()
				}
			case u.installing:
				if u.cancel != nil && (u.total <= 0 || u.downloaded < u.total) {
					if ui.Button(c, T("updateCancel")).Clicked() {
						u.cancel()
					}
				}
			case u.available != nil && u.status != "updateCheckFailed":
				if ui.PrimaryButton(c, T("updateInstall")).Disabled(u.checking || m.a.updateBusy()).Clicked() {
					m.a.installUpdate()
				}
			default:
				if ui.Button(c, T("updateCheck")).Disabled(u.checking).Clicked() {
					m.a.checkUpdates()
				}
			}
		})
	})
}

// drawUpdateRing renders determinate download progress or a rotating arc while
// checking, verifying, installing and restarting. Reduced motion freezes the arc.
func drawUpdateRing(p *ui.Painter, r ui.Rect, progress, angle float64) {
	cx, cy, radius := r.X+r.W/2, r.Y+r.H/2, float64(r.W/2-2)
	arc := func(start, sweep float64, color ui.Color) {
		var path ui.Path
		steps := max(2, int(math.Ceil(sweep*16)))
		for i := 0; i <= steps; i++ {
			a := start + sweep*float64(i)/float64(steps)
			x, y := cx+float32(math.Cos(a)*radius), cy+float32(math.Sin(a)*radius)
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		p.StrokePath(&path, 2.5, color)
	}
	arc(0, 2*math.Pi, ui.RGBA(247, 250, 255, 0.25))
	sweep := 0.28 * 2 * math.Pi
	if progress >= 0 {
		sweep = max(0.035, min(1, progress)) * 2 * math.Pi
	}
	arc(angle, sweep, ui.Hex("#69e7c3"))
}

// drawAppMark paints the app's mark small: the indigo tile of
// resources/logo.svg with its five voice strokes and the mint dot.
func drawAppMark(p *ui.Painter, r ui.Rect) {
	p.Fill(r, ui.RGB(32, 40, 79), r.W*0.23)
	u := r.W / 1024 // the logo's own units
	bars := [][3]float32{{292, 366, 142}, {402, 410, 190}, {512, 468, 218}, {622, 410, 190}, {732, 366, 142}}
	w := 62 * u
	for _, b := range bars {
		p.Fill(ui.Rect{X: r.X + b[0]*u - w/2, Y: r.Y + b[1]*u - w/2, W: w, H: b[2]*u + w}, ui.RGB(247, 250, 255), w/2)
	}
	d := 62 * u
	p.Fill(ui.Rect{X: r.X + 732*u - d/2, Y: r.Y + 268*u - d/2, W: d, H: d}, ui.RGB(105, 231, 195), d/2)
}
