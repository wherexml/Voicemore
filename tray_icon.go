package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"golang.org/x/image/vector"
)

// trayIcon keeps the voice mark from resources/logo.svg, without its tile.
// MyGo displays this at 18 points; render at 2x for Retina. AppKit supplies
// the menu bar's foreground color through IconIsTemplate.
func trayIcon() []byte {
	const size = 36
	raster := vector.NewRasterizer(size, size)
	// Fit the original mark's 502 x 480 bounds into 16 x 16 points.
	coord := func(x, y float32) (float32, float32) {
		return 2 + (x-261)*32/502, 2 + (y-226)*32/502
	}
	move := func(x, y float32) { a, b := coord(x, y); raster.MoveTo(a, b) }
	line := func(x, y float32) { a, b := coord(x, y); raster.LineTo(a, b) }
	curve := func(x1, y1, x2, y2, x3, y3 float32) {
		a, b := coord(x1, y1)
		c, d := coord(x2, y2)
		e, f := coord(x3, y3)
		raster.CubeTo(a, b, c, d, e, f)
	}
	// Filled capsules reproduce the logo's round-ended, 62-unit strokes.
	capsule := func(x, top, bottom float32) {
		const r, k = float32(31), float32(31 * 0.55228475)
		move(x-r, top)
		curve(x-r, top-k, x-k, top-r, x, top-r)
		curve(x+k, top-r, x+r, top-k, x+r, top)
		line(x+r, bottom)
		curve(x+r, bottom+k, x+k, bottom+r, x, bottom+r)
		curve(x-k, bottom+r, x-r, bottom+k, x-r, bottom)
		raster.ClosePath()
	}
	capsule(292, 366, 508)
	capsule(402, 410, 600)
	capsule(512, 468, 686)
	capsule(622, 410, 600)
	capsule(732, 366, 508)
	capsule(732, 268, 268)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	raster.Draw(img, img.Bounds(), image.NewUniform(color.Black), image.Point{})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
