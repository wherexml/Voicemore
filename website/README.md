# Voicemore website mock

本目录维护三种语言的官网源码。设计试稿、候选主视觉和本地验收证据单独保留，不随客户端 Release 发布；官网部署须另行执行。

Static mock of the official website. No build step and no dependencies:
open any page in a browser, or serve the folder with any static server.

| File | Language |
| --- | --- |
| `index.html` | Simplified Chinese (default, `x-default`) |
| `zh-hant.html` | Traditional Chinese |
| `en.html` | English |
| `assets/site.css` | Shared styles (light and dark) |
| `assets/site.js` | Shared enhancements: header state, scroll-linked motion, language links that keep the current section, and the simulated dictation demo |
| `assets/*.png` | App icon and mock interface screenshots |

## Page structure

1. Minimal hero: headline, short description, Download and GitHub buttons. A partial home screenshot is visible below the fold.
2. Reversible scroll story: the home screenshot grows and levels, then the live dictation capsule and model settings each take focus with one caption. Images are contained without cropping.
3. Click-to-start dictation demo, privacy facts, FAQ, and closing download requirements.

## Editing rules

- Keep the three pages in step: same sections, same order, same claims.
- Keep copy short: a brief benefit headline and at most one short sentence
  per feature. System requirements remain in the closing download section. Do not reintroduce mock or simulation notices as marketing copy.
- The `<head>` of each page carries metadata and FAQ structured data that
  must match the visible FAQ (generated from the same questions).
- Only describe capabilities the app actually has: Fn to start and stop,
  insertion into the focused field, offline local recognition after the
  model download, Alibaba Cloud or Volcengine for cloud recognition,
  optional text cleanup (off by default), dictionary corrections applied
  only after confirmation, local settings, history and recordings (30-day
  default retention), in-app updates, Apple Silicon with macOS 26.2 or later.
- The download buttons point at the current release tag
  (`https://github.com/wherexml/Voicemore/releases/tag/v0.3.7`); update all
  six links (two per page) plus the structured data on each release.
- Illustrations are existing assets or CSS. Screenshots come from the mock source; provenance is documented here, not shown as marketing copy. Pages reference `capsule-mock.png`,
  `models-mock.png` and `home-mock.png`; replacing those files with
  higher-resolution captures needs no HTML change (keep the filenames, and
  update the `width`/`height` attributes if the aspect ratio changes).
- The demo needs these element ids: `stage`, `key`, `hint`, `typed`,
  `pill-text`, `live`, `tidy`, `transport`, `replay`, `pause`; its text comes
  from the `data-*` attributes on `#stage`.

## Motion

The original three pages share a single scroll narrative in `site.js` / `site.css`. Desktop above 760px uses a 300svh track and a sticky viewport below the navigation. Scroll position directly determines title upward translation/fade, screenshot scale/tilt and the home → capsule → models transitions. Reverse scrolling reverses progress; no timer, scroll interception, hover tilt, glare or perpetual decorative animation is used. Only the dominant panel is exposed to accessibility APIs, and captions do not overlap.

Images fit the available viewport without cropping the feature being presented. Mobile, reduced motion (including live preference changes), and JavaScript-disabled rendering show all three figures in normal document flow. The separate simulated demo starts only on Fn / Replay interaction and never requests microphone access.

### Repeatable browser acceptance

1. Open `/`, confirm only headline, description and two hero actions, with a home-screen preview below.
2. Scroll down through the pinned region; pause at each panel, then scroll back. Confirm one caption at a time, full screenshot visibility and reversible progress.
3. Click Replay in the following demo and confirm the sentence is inserted.
4. Open `/en.html`, confirm all three screenshot sources use `-en.png`.
5. At 390px width and with reduced motion enabled, confirm all panels appear sequentially and there is no long sticky track or horizontal overflow.

## Before publishing

No production domain is decided yet. Add `<link rel="canonical">` and
`og:url`, make `og:image` and the `hreflang` links absolute, and add `url`
to the structured data.

## Current screenshot sources (2026-10-16)

- `home-mock.png`: `mock/home-v3.html`, the confirmed V3 homepage; 4320 × 3280. English uses `mock/home-v3-en.html`, generated from the V3 source by `python3 website/scripts/make-english-home.py`, including English labels and fictional records. Capture it using the same window crop and replace `home-en.png`.
- `models-mock.png` / `models-en.png`: `mock/claude-v2.html?lang=hans` / `?lang=en`, complete onboarding preview → enter app → Speech recognition settings; crop `#prefs`; 3920 × 3040.
- `capsule-mock.png` / `capsule-en.png`: `mock/capsule-states-v2.html`, select Recording in the gallery; capture the union of `#panel` and `#pill` with 30px horizontal / 20px vertical padding. Chinese 2262 × 924; English 2718 × 1038.
- Captured directly in Chrome at native Retina resolution using CDP `Page.captureScreenshot`, clip scale 2 for windows and 3 for capsules. No image upscaling or invented UI. Window crop sizes: home 1080 × 820 CSS pixels; settings 980 × 760.

The public pages omit the mock-image and simulated-microphone notices. The demo remains a simulation and does not request microphone access. English pages must use English screenshots for home, model settings, and capsule; update image URL versions after replacing assets.

## Background treatment (2026-10-17)

Shared static atmospheric layers add overhead and side light, a soft horizon, local fine grain and a spotlight behind the product. Grain is restricted to the hero and closing panel; no fixed fullscreen overlay, continuous animation or new image request is introduced. Decorative layers ignore pointer events and are hidden in forced colors. Light/dark palettes share the same geometry. The original scroll narrative and Q edition are unchanged.

## Local design variants and specification boundary (2026-10-18)

The tracked root pages remain the original static site. Local `approved/` is
the blue visual baseline; `v2/` is its independent three-language copy revision
with the remote-desktop demo card; `q/` is the linen-and-watch concept.
Candidate artwork lives in `assets/hero-concepts/`. Their presence does not
select a new production homepage or prove deployment.

When available, read local `spec/WEBSITE.md`, `spec/DESIGN.md` and
`spec/ACCEPTANCE.md` for the variant map, frontend contracts, design tokens and
repeatable review matrix. The entire `spec/` directory stays out of Git.
The repository's public behavior specifications remain in `docs/public/`.
Local preview routing is documented in
[Mock navigation](../docs/public/MOCK-NAVIGATION.md); use the shared five-digit
port. Draft sources and screenshots are not included in this documentation
closeout. Website deployment and release-link updates require their own
verification; this closeout does not publish either the website or an app.
