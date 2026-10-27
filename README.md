# Voicemore

Native macOS dictation. Press **Fn**, speak, and press **Fn** again to insert text into the focused app.

[简体中文](README.zh-CN.md) · [Download](https://github.com/wherexml/Voicemore/releases/latest) · [Release notes](https://github.com/wherexml/Voicemore/releases) · [Report an issue](https://github.com/wherexml/Voicemore/issues)

## Install

Requires **Apple Silicon and macOS 26.2 or later**.

1. Download the DMG from the latest release and drag **Voicemore** to **Applications**.
2. Open the app and grant Microphone, Accessibility and Input Monitoring permissions.
3. Choose local speech recognition or configure a cloud provider in Settings.

Local recognition downloads its runtime and model during setup and can work offline afterward. Cloud services require network access and provider credentials. Stable releases use Developer ID signing and Apple notarization; the app supports signed in-app updates.

## Features

- Dictation into the active app, with configurable shortcuts and optional automatic sending.
- Local or cloud speech recognition, optional text cleanup and translation.
- Transcript history, recording playback and a personal dictionary.
- Private-app rules for dictation without saved transcripts, recordings or correction learning.
- English, Simplified Chinese and Traditional Chinese interfaces.

Target-app behavior depends on macOS permissions and application compatibility. Source may include features not yet available in a release; see [CHANGELOG.md](CHANGELOG.md).

## Privacy

Local recognition processes audio on your Mac. Cloud recognition sends audio to the selected provider; hosted text cleanup sends transcripts to the configured service.

Settings, history, recordings, models and encrypted credentials are stored under `~/Library/Application Support/Voice Input/`. Recording storage is enabled by default with a 30-day retention setting. Adjust this in Settings. Credential encryption uses a local key and does not protect against software running as your macOS user.

## Development

See [BUILDING.md](BUILDING.md) for prerequisites, build commands and verification. This repository contains the macOS client and its build/acceptance tools. Website sources, internal design notes, operational service patches and experimental prototypes are maintained separately.

[Third-party notices](resources/licenses/README.md) cover bundled dependencies and artwork. When reporting an issue, include your macOS version, chip and reproduction steps; remove credentials and personal transcripts from logs.
