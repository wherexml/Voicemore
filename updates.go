package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// updatePresentation keeps the header short; explanations belong in the popover.
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
		return label, T("updateDownloadShort"), T("updatePauseNote")
	case u.checking:
		return "", T("updateChecking"), T("updateAutoNote")
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
		return "", T("updateCurrent"), T("updateAutoNote")
	default:
		return "", T("updateCheck"), T("updateAutoNote")
	}
}

func (m *mainWindow) updateView(c *ui.Context) {
	u := &m.a.updates
	t := c.Theme()
	label, title, note := m.updatePresentation()
	color := t.TextMuted
	if u.available != nil || u.installing {
		color = t.Accent
	}
	if u.detail != "" {
		color = t.Warning
	}
	anchor := ui.Button(c.Key("update-trigger"), "").Label(T("updateCheck")).Tooltip(title).
		Padding(5, 9).Radius(8).BorderWidth(0).TextColor(color).Children(func() {
		if u.checking || u.installing {
			ui.Spinner(c).Size(14, 14)
		} else {
			ui.Icon(c, icRefresh).FontSize(14).TextColor(color)
		}
		if label != "" {
			ui.Text(c, label).FontSize(12).TextColor(color).SingleLine()
		}
	})
	if anchor.Clicked() {
		m.updateOpen = !m.updateOpen
	}
	ui.Popover(c.Key("update-panel"), anchor, &m.updateOpen, func() {
		ui.Column(c).Width(300).Gap(14).Padding(8).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(15).FontWeight(600).Grow(1)
				if iconButton(c, icClear, T("updateClose")).Clicked() {
					m.updateOpen = false
				}
			})
			ui.Text(c, T("updateVersion", mygo.App.Version())).FontSize(11).TextColor(t.TextMuted)
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
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
