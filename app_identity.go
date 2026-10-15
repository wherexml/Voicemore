package main

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"image/png"
	"time"
	"voiceinput/internal/macos"
)

type cachedAppIcon struct {
	image   *ui.Bitmap
	checked time.Time
}

var appIcons = map[string]cachedAppIcon{}

// Cached on the UI thread. Retry missing applications after a minute.
func appIdentity(c *ui.Context, bundle, name string, grow bool) {
	key := bundle + "\x00" + name
	cached, ok := appIcons[key]
	if !ok || (cached.image == nil && time.Since(cached.checked) > time.Minute) {
		cached = cachedAppIcon{checked: time.Now()}
		if data := macos.AppIconPNG(bundle, name); len(data) > 0 {
			if img, err := png.Decode(bytes.NewReader(data)); err == nil {
				cached.image = ui.NewBitmap(img)
			}
		}
		appIcons[key] = cached
	}
	if cached.image != nil {
		ui.Image(c, cached.image).Size(20, 20)
	} else {
		ui.Box(c).Size(20, 20).Radius(5).Background(settingsGroupColor(c.Theme())).Justify(ui.Center).AlignItems(ui.Center).Children(func() { ui.Text(c, "▧").FontSize(15).TextColor(c.Theme().TextMuted) })
	}
	text := ui.Text(c, name).FontSize(13).SingleLine().MinWidth(0)
	if grow {
		text.Grow(1)
	}
}
