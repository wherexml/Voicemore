<p align="center"><img src="https://raw.githubusercontent.com/wherexml/Voicemore/main/resources/icon.png" width="112" alt="Voicemore icon"></p>
<h1 align="center">Voicemore</h1>
<p align="center">Speak naturally. Write where your cursor is.</p>
<p align="center"><a href="README.zh-CN.md">简体中文</a> · <a href="https://github.com/wherexml/Voicemore/releases/tag/v0.3.7">Download for Apple Silicon</a></p>

Voicemore is a native macOS dictation app. Press **Fn**, speak, and press **Fn** again to insert the transcript into the focused text field. Run speech recognition locally, connect a cloud provider, and optionally tidy your words with a language model.

## What’s new in 0.3.7

- Local speech-language selection: Chinese, automatic detection, or English, including history retranscription.
- Private applications: insert text without saving this dictation's transcript, recording, or correction suggestions.
- A continuous dictation/send-countdown capsule; insertion only by default, with optional per-app automatic sending, cancellation and editing.
- Improved AirPods audio restoration and global shortcut recovery; shared microphone and trigger-key selectors.
- A five-step onboarding guide with native readiness checks and inline local-model setup.
- Custom cleanup thinking modes and a real cleanup probe; DeepSeek thinking disabled by default, with better grouping and text preservation.

## A look inside

### Speak naturally. Keep your flow.

Press **Fn** to start, speak, and press it again to insert your words into the app you are using. Home keeps your shortcut, speech model, and recent transcripts close at hand.

![Voicemore Home in English, with dictation controls, setup overview, and sample transcripts](https://raw.githubusercontent.com/wherexml/Voicemore/main/docs/public/images/home-en-v035.jpg)

### A small capsule that follows your words

See your transcript as you speak. Expand it to read the full text, finish with one click, or cancel with **Esc**. The indigo capsule shows listening, transcription, optional cleanup, and the final result. If insertion fails, your words stay available to copy.

![Voicemore indigo capsule: Listening, Transcribing, Tidying up, Inserted, Copied, and Text kept](https://raw.githubusercontent.com/wherexml/Voicemore/main/docs/public/images/capsule-en.jpg)

### Your choice of speech and language models

Run speech recognition on your Mac, or configure a cloud provider. Choose a separate language model for optional text cleanup.

![Voicemore model settings in English, showing local speech recognition and configurable providers](https://raw.githubusercontent.com/wherexml/Voicemore/main/docs/public/images/models-en.jpg)

<sub>Images captured from the project's English interface demos, using sample text and configuration. They illustrate the interface; native macOS rendering may differ.</sub>

## Download and install

**Apple Silicon (M-series), macOS 26.2 or later.** Intel Macs are not supported by this package. The current bundled MLX runtime sets the minimum OS requirement. Native installation and local dictation have been verified on macOS 26.2 arm64; local development was also verified on macOS 27.0 arm64.

1. Download **Voicemore-0.3.7-arm64.dmg** from [v0.3.7 Releases](https://github.com/wherexml/Voicemore/releases/tag/v0.3.7).
2. Open the disk image and drag **Voicemore** onto **Applications**.
3. Eject the disk image, then open Voicemore from Applications.
4. Grant **Microphone**, **Accessibility**, and **Input Monitoring** in System Settings → Privacy & Security. Restart after granting keyboard permissions.
5. In **Settings → Models**, open the local speech model and choose download/deploy. Wait until it is ready, then try dictation in a text editor.

**Signed and notarized:** v0.3.7 uses Developer ID signing and Apple notarization, with stapled app and DMG tickets. Upgrading from a development-signed preview may require confirming microphone, Accessibility and Input Monitoring permissions again.

The DMG includes the inference runtime, but **not model weights**. Initial local setup downloads roughly **2.47 GB** of model files and extracts roughly **0.65 GB** of runtime. Internet access is needed for the download; local recognition works offline after setup. You can also copy a compatible existing model. Cloud recognition needs your own provider credentials and network access.

## Updates

From **0.3.0**, Voicemore checks GitHub Releases about 10 seconds after launch and every hour while running. New versions appear in the bottom-left update control above Settings on every page and in the menu-bar tooltip; macOS notifications are attempted once per version and depend on system notification permissions.

Click the indigo update icon above Settings to download, verify the Ed25519-signed archive, replace the application, and restart automatically. You can cancel the download or retry a failed update. Dictation is paused during installation; updates wait for active dictation/model setup to finish. Settings, models, history and credentials are preserved. The app must be installed in a writable Applications location, not running from a read-only DMG.

**0.2.0 users must install 0.3.0 manually once.** Future releases must include the signed update archive and manifest; uploading only a DMG does not notify existing clients. See [publishing updates](docs/public/UPDATES.md).

## Features

- **Dictate into the active app:** configurable trigger key, toggle/hold behavior, recording feedback, and a compact transcription overlay.
- **Local speech recognition:** app-managed Qwen3-ASR-1.7B-8bit on Apple Silicon, with Chinese, automatic, and English modes and a separate inference process. No existing oMLX installation required for speech recognition.
- **Cloud options:** Alibaba Cloud and Volcengine speech recognition using your own accounts.
- **Optional text cleanup:** connect a local or compatible hosted language-model endpoint; cleanup is off by default.
- **Private applications:** choose apps whose dictations are inserted without saved history, recordings or correction learning. Existing history is preserved.
- **History and playback:** review transcripts, edit results, and replay saved recordings.
- **Personal dictionary:** vocabulary hints, correction rules, and suggestions from edits. Suggested rules need confirmation; observing edits in other apps is opt-in.
- **Native interface:** Dock app, menu-bar controls, and English, Simplified Chinese, and Traditional Chinese UI.

Input behavior depends on the target app and macOS permissions. TextEdit dictation has been exercised; some editors and protected fields may behave differently. Fn can also trigger macOS's own action: set “Press 🌐 key to” to “Do Nothing”, or choose another shortcut.

## Module documentation

Remote desktop profiles in Settings → Text & Paste select a running client, paste shortcut, and clipboard sync delay. Profiles start disabled; text stays on the clipboard and remote insertion is unverified. See the [remote input specification](docs/public/REMOTE-INPUT.md). Cleanup supports filler removal, explicit corrections, and spoken lists; see its [behavior and protection limits](internal/cleanup/README.md). The [module index](docs/public/README.md) links specifications and acceptance procedures. These describe the current source, not necessarily the downloadable release.

## Privacy and storage

Local speech recognition sends audio only to the app's loopback worker (`127.0.0.1:48132`). Selecting a cloud speech provider sends audio to that provider. Enabling hosted text cleanup sends transcript text to the configured endpoint.

Settings, history, recordings, dictionary, models and encrypted credentials stay under:

```text
~/Library/Application Support/Voice Input/
```

The legacy directory name preserves existing installations after the Voicemore rename. API credentials use a local AES-GCM encrypted store; its encryption key is also stored locally with restrictive file permissions. This protects against casual plaintext exposure, not software running as your macOS user. Recording storage is enabled by default with a 30-day retention setting; adjust it in Settings → History & Privacy.

## Build from source

Requires an Apple Silicon Mac on macOS 26.2+, Xcode Command Line Tools, and **Go 1.27.1+**. Go dependencies are pinned in `go.mod`/`go.sum`.

```sh
git clone https://github.com/wherexml/Voicemore.git
cd Voicemore
# Downloads the versioned runtime asset and checks its SHA-256.
./scripts/fetch-asr-runtime.sh
# Isolated development build with the fixed Apple Development identity:
python3 scripts/build-development.py
```

Development builds use a separate Voicemore Dev identity, data directory and output, without the production update feed. Daily production installs and official releases share `./scripts/build-macos.sh`, the approved Developer ID and Apple notarization pipeline; it prepares a candidate without publishing. Missing credentials stop the build, with no ad-hoc fallback. No signing credentials are included in this repository. See [packaging and verification](docs/public/PACKAGING.md).

The runtime archive is a Release asset rather than a Git blob. Python dependencies and model revision/checksums live in `internal/localasr/assets/`. Model weights are fetched during setup. The archive retains third-party runtime licenses; [dependency notices](resources/licenses/README.md) cover bundled source dependencies and artwork.

## Project status

This is an early release. Stable builds require Developer ID signing, Apple notarization, Gatekeeper and final-artifact checks; native regression testing is selected by change scope. Intel support and broad target-app compatibility remain outside the verified scope. [Report an issue](https://github.com/wherexml/Voicemore/issues) with your macOS version, chip and reproduction steps; remove recordings, transcripts and API keys before sharing logs.
