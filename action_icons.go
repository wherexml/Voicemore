package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// The approved SF Symbols are native macOS exports, not redrawn SVGs.
// MyGo cannot display NSSymbolImageRep directly; retain its alpha at 8×
// the timeline's 16 DIP size and tint the white mask in the current theme.
//
//go:embed resources/symbols/doc.on.doc.png
var copySymbolPNG []byte

//go:embed resources/symbols/trash.png
var trashSymbolPNG []byte

func actionSymbol(data []byte) *ui.Bitmap {
	b, err := ui.DecodeBitmap(data)
	if err != nil {
		panic("decode embedded SF Symbol: " + err.Error())
	}
	return b
}

// actionIcon accepts existing vector glyphs and native symbol masks, so the
// same copy/delete assets reach timelines, dictionary and model/settings actions.
func actionIcon(c *ui.Context, source ui.ImageSource, size float32, color ui.Color) ui.Element {
	if svg, ok := source.(*ui.SVG); ok {
		return ui.Icon(c, svg).Size(size, size).TextColor(color)
	}
	bitmap := source.(*ui.Bitmap)
	return ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		p.ImageRegion(bitmap, ui.Rect{}, r, ui.ImageOptions{Tint: color})
	})
}
