package main

import "github.com/egoist/mygo/ui"

// The interface icons, 16×16 outlines drawn in the text color. They are
// the set of mock/index.html.
func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// railIcon is an icon of the navigation rail: drawn at 24 px, where a
// stroke of 1.2 in its 16-unit box comes out at 1.8 px.
func railIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var (
	icAllApps    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="6.5" height="6.5" rx="1.8"/><rect x="14" y="3.5" width="6.5" height="6.5" rx="1.8"/><rect x="3.5" y="14" width="6.5" height="6.5" rx="1.8"/><rect x="14" y="14" width="6.5" height="6.5" rx="1.8"/></svg>`))
	railHome     = railIcon(`<path d="M2.6 7.6L8 2.9l5.4 4.7M4.1 6.6v6.3h7.8V6.6"/>`)
	railHistory  = railIcon(`<circle cx="8" cy="8" r="6"/><path d="M8 4.6V8l2.3 1.5"/>`)
	railDict     = railIcon(`<path d="M3 2.6h8.2a1.8 1.8 0 0 1 1.8 1.8v9H4.6A1.6 1.6 0 0 1 3 11.8z"/><path d="M3 11.8a1.6 1.6 0 0 1 1.6-1.6H13M6 5.6h4"/>`)
	railSettings = railIcon(`<path d="M3 5.6h10M3 10.4h10"/>`) // two plain strokes
	// The arc meets the arrow tip exactly; keep the rail stroke at 1.2 units.
	railUpdate = railIcon(`<path d="M13 5.5C12.1 3.7 10.3 2.7 8.3 2.7C5.35 2.7 3 5.05 3 8s2.35 5.3 5.3 5.3c2.65 0 4.7-1.9 5.2-4.3M13 2.3v3.2H9.8"/>`)

	icBack    = icon(`<path d="M10 4L6 8l4 4"/>`)
	icUp      = icon(`<path d="M4 10l4-4 4 4"/>`)
	icDown    = icon(`<path d="M4 6l4 4 4-4"/>`)
	icCheck   = icon(`<path d="M3.4 8.4l3 3 6.2-7"/>`)
	icSparkle = icon(`<path d="M7 2.5l1.2 3.3L11.5 7 8.2 8.2 7 11.5 5.8 8.2 2.5 7l3.3-1.2zM12.2 10.6l.5 1.3 1.3.5-1.3.5-.5 1.3-.5-1.3-1.3-.5 1.3-.5z"/>`)
	icShield  = icon(`<path d="M8 1.8l5 1.9v3.9c0 3.1-2.1 5.3-5 6.6-2.9-1.3-5-3.5-5-6.6V3.7z"/>`)
	icLock    = icon(`<rect x="3.4" y="7" width="9.2" height="6.4" rx="1.6"/><path d="M5.4 7V5.2a2.6 2.6 0 0 1 5.2 0V7"/>`)

	icCalendar = icon(`<rect x="2" y="3" width="12" height="11" rx="2"/><path d="M5 1v4M11 1v4M2 7h12"/>`)
	icWave     = icon(`<path d="M2 6v4M5 3v10M8 5v6M11 2v12M14 6v4"/>`)
	icMic      = icon(`<rect x="5.6" y="1.6" width="4.8" height="8" rx="2.4"/><path d="M3.2 7.6a4.8 4.8 0 0 0 9.6 0M8 12.4v2.2"/>`)
	icHistory  = icon(`<circle cx="8" cy="8" r="6"/><path d="M8 4.6V8l2.3 1.5"/>`)
	icBook     = icon(`<path d="M3 2.6h8.2a1.8 1.8 0 0 1 1.8 1.8v9H4.6A1.6 1.6 0 0 1 3 11.8z"/><path d="M3 11.8a1.6 1.6 0 0 1 1.6-1.6H13M6 5.6h4"/>`)
	icSliders  = icon(`<path d="M2.4 4.6h6.4M11.8 4.6h1.8M2.4 11.4h1.8M7.2 11.4h6.4"/><circle cx="10.3" cy="4.6" r="1.5"/><circle cx="5.7" cy="11.4" r="1.5"/>`)
	icPlay     = icon(`<path d="M5.4 3.6v8.8l7-4.4z" fill="currentColor"/>`)
	icPause    = icon(`<path d="M5.6 3.8v8.4M10.4 3.8v8.4" stroke-width="2"/>`)
	icCopy     = icon(`<rect x="5.5" y="5.5" width="8" height="8" rx="1.8"/><path d="M10.5 3.2H4.3a1.6 1.6 0 0 0-1.6 1.6v6.2"/>`)
	icMore     = icon(`<g fill="currentColor" stroke="none"><circle cx="3.5" cy="8" r="1.2"/><circle cx="8" cy="8" r="1.2"/><circle cx="12.5" cy="8" r="1.2"/></g>`)
	icDetails  = icon(`<circle cx="8" cy="8" r="6"/><path d="M8 4.5v4M8 11h.01"/>`)
	icTrash    = icon(`<path d="M3 4.5h10M6.2 4.5V3h3.6v1.5M4.4 4.5l.6 8.3h6l.6-8.3"/>`)
	icRefresh  = icon(`<path d="M12.6 8a4.6 4.6 0 1 1-1.5-3.4M12.8 2.4v2.8H10"/>`)
	icPencil   = icon(`<path d="M3 13l.6-2.8 7-7a1.4 1.4 0 0 1 2 2l-7 7zM9.6 4.2l2.2 2.2"/>`)
	icUndo     = icon(`<path d="M5.5 3.5L3 6l2.5 2.5M3 6h6.2a3.3 3.3 0 0 1 0 6.6H6.5"/>`)
	icPlus     = icon(`<path d="M8 3.5v9M3.5 8h9"/>`)
	icImport   = icon(`<path d="M8 2.5V10M5.2 7.4L8 10.2l2.8-2.8M3 12.8h10"/>`)
	icExport   = icon(`<path d="M8 10V2.6M5.2 5.2L8 2.4l2.8 2.8M3 12.8h10"/>`)
	icChevron  = icon(`<path d="M6 4l4 4-4 4"/>`)
	icPerson   = icon(`<circle cx="8" cy="8" r="6.2"/><circle cx="8" cy="5" r=".9" fill="currentColor" stroke="none"/><path d="M5 7h6M8 7v2.4M6.4 12L8 9.4 9.6 12"/>`)
	icKeyboard = icon(`<rect x="1.8" y="4" width="12.4" height="8" rx="1.8"/><path d="M4.4 6.6h.1M6.8 6.6h.1M9.2 6.6h.1M11.6 6.6h.1M5.4 9.4h5.2"/>`)
	icEye      = icon(`<path d="M1.6 8s2.3-4.2 6.4-4.2S14.4 8 14.4 8s-2.3 4.2-6.4 4.2S1.6 8 1.6 8z"/><circle cx="8" cy="8" r="1.9"/>`)
	icEyeOff   = icon(`<path d="M1.6 8s2.3-4.2 6.4-4.2S14.4 8 14.4 8s-2.3 4.2-6.4 4.2S1.6 8 1.6 8z"/><circle cx="8" cy="8" r="1.9"/><path d="M3 13L13 3"/>`)
	icClear    = icon(`<circle cx="8" cy="8" r="6"/><path d="M5.9 5.9l4.2 4.2M10.1 5.9l-4.2 4.2"/>`)
	icSearch   = icon(`<circle cx="7" cy="7" r="4.3"/><path d="M10.3 10.3l3.2 3.2"/>`)
	icLink     = icon(`<path d="M6.8 9.2a2.6 2.6 0 0 0 3.7 0l2-2a2.6 2.6 0 0 0-3.7-3.7l-.7.7M9.2 6.8a2.6 2.6 0 0 0-3.7 0l-2 2a2.6 2.6 0 0 0 3.7 3.7l.7-.7"/>`)
)

// iconButton is a button showing only an icon. Its label names it for
// assistive technology and is its tooltip.
func iconButton(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	return ui.Button(c, "").Label(label).Tooltip(label).Padding(4, 6).Children(func() { ui.Icon(c, svg).FontSize(14) })
}

// labelButton is a button with an icon before its label.
func labelButton(c *ui.Context, svg *ui.SVG, label string) ui.Element {
	return ui.Button(c, "").Label(label).Children(func() {
		ui.Icon(c, svg).FontSize(13)
		ui.Text(c, label).SingleLine()
	})
}
