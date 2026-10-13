package main

import (
	"context"
	"encoding/json"
	"errors"
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

func (m *mainWindow) updateView(c *ui.Context) {
	u := &m.a.updates
	t := c.Theme()
	ui.Column(c.Key("updates")).Gap(8).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Voicemore "+mygo.App.Version()).FontSize(13).TextColor(t.TextMuted).Grow(1)
			if u.available != nil {
				ui.Text(c, T("updateFound", u.available.Version)).FontSize(13).FontWeight(600)
				if ui.Button(c.Key("install"), T("updateInstall")).Disabled(u.installing || u.checking || m.a.updateBusy()).Clicked() {
					m.a.installUpdate()
				}
			} else {
				if ui.Button(c.Key("check"), T("updateCheck")).Disabled(u.checking || u.installing).Clicked() {
					m.a.checkUpdates()
				}
			}
		})
		if u.status != "" && u.status != "updateAvailable" {
			status := T(u.status)
			if u.status == "updateDownloading" && u.total > 0 {
				status = T("updateProgress", 100*u.downloaded/u.total)
			}
			ui.Text(c, status).FontSize(12).TextColor(t.TextMuted)
		}
		if u.available != nil && !u.installing && m.a.updateBusy() {
			ui.Text(c, T("updateBusy")).FontSize(12).TextColor(t.TextMuted)
		}
		if u.detail != "" {
			ui.Text(c, u.detail).FontSize(11).TextColor(t.TextMuted)
		}
		if u.installing && u.cancel != nil {
			if ui.Button(c.Key("cancel"), T("updateCancel")).Clicked() {
				u.cancel()
			}
		}
	})
}
