package main

import (
	"embed"
	"log"
	"regexp"

	"github.com/egoist/mygo/ui"

	"voiceinput/internal/providers"
)

// The provider logos are the files of mock/assets, copied unchanged to
// resources/providers (sources and licenses in its README.md) and compiled
// into the app. A file MyGo cannot parse leaves its provider with the
// neutral tile that custom providers have, rather than stopping the app.

//go:embed resources/providers/*.svg
var logoFiles embed.FS

var logos = map[string]*ui.SVG{}

func init() {
	for _, name := range []string{"alibabacloud", "volcengine", "deepseek", "omlx-light", "omlx-dark"} {
		data, err := logoFiles.ReadFile("resources/providers/" + name + ".svg")
		if err == nil {
			var svg *ui.SVG
			if svg, err = ui.ParseSVG(data); err != nil {
				svg, err = ui.ParseSVG(simplifySVG(data))
			}
			if err == nil {
				logos[name] = svg
			}
		}
		if err != nil {
			log.Printf("logo %s: %v", name, err)
		}
	}
}

var (
	svgEmSize = regexp.MustCompile(`\s(width|height)="1em"`)
	svgStyle  = regexp.MustCompile(`\sstyle="[^"]*"`)
	svgFilter = regexp.MustCompile(`\sfilter="[^"]*"`)
	svgDefs   = regexp.MustCompile(`(?s)<filter.*?</filter>`)
)

// simplifySVG removes from a logo what a parser may refuse and the picture
// does not need: sizes in em (the viewBox gives the proportions), inline
// styles, and drop-shadow filters. The files on disk stay as published.
func simplifySVG(data []byte) []byte {
	for _, re := range []*regexp.Regexp{svgEmSize, svgStyle, svgFilter, svgDefs} {
		data = re.ReplaceAll(data, nil)
	}
	return data
}

// logo draws the mark of a provider at a size: the local oMLX services,
// a catalog provider, or the neutral tile of a custom one.
func logo(c *ui.Context, provider string, size float32) {
	t := c.Theme()
	if provider == "builtin-asr" {
		ui.Row(c).Size(size, size).Radius(size * 0.28).Justify(ui.Center).AlignItems(ui.Center).Background(fillColor(t)).Children(func() {
			ui.Icon(c, icMic).FontSize(size * 0.65).TextColor(t.Accent)
		})
		return
	}
	var svg *ui.SVG
	if provider == providers.Local {
		svg = logos["omlx-light"]
		if t.Dark {
			svg = logos["omlx-dark"]
		}
	} else if e, ok := providers.Lookup(provider); ok {
		svg = logos[e.Logo]
	}
	if svg != nil {
		ui.Image(c, svg).Size(size, size)
		return
	}
	ui.Row(c).Size(size, size).Radius(size*0.28).Justify(ui.Center).AlignItems(ui.Center).
		Background(fillColor(t)).Border(1, t.Border).Children(func() {
		ui.Icon(c, icLink).FontSize(size * 0.45).TextColor(t.TextMuted)
	})
}
