# Public macOS distribution

## Failure conditions to reject before publishing

- A valid Apple Development signature is not a Developer ID distribution signature.
- Signature verification alone does not establish notarization or Gatekeeper acceptance.
- A valid DMG must not hide an unacceptable app inside it.
- An unavailable Developer ID private key must stop signing; a downloaded certificate alone is insufficient.
- A failed/pending notarization must not become a stable GitHub Release.
- Changing the app or runtime after notarization invalidates the final-artifact evidence.
- Repacking/re-signing an update archive requires regenerating its Ed25519 manifest signature and checksums.
- Changing signing identity can affect existing TCC permissions even if the Bundle ID is unchanged.

## Confirmed v0.3.1 finding (2026-10-14)

On a separate Apple Silicon Mac running macOS 26.2, the GitHub DMG checksum matched and the installed app passed strict codesign verification. Its identity was the project's fixed Apple Development certificate. `spctl --assess --type execute` rejected it, and launching with download quarantine produced the visible “Apple cannot verify” warning. The test added quarantine because the asset was fetched by curl; it was not a full browser-download/drag-install acceptance test. Application runtime and dictation were not reached.

## Keep local development separate

`scripts/build-macos.sh` retains the fixed development identity required by AGENTS.md. Do not replace it with Developer ID, ad-hoc signing or another team for local builds.

`scripts/publish-release.sh` currently builds development artifacts only. It now refuses its default invocation; `--development-preview` explicitly publishes a **prerelease**, never the stable/latest release. This prevents repeating the stable-feed mistake; it does not make the preview pass Gatekeeper.

## Runtime audit (2026-10-14)

The currently pinned runtime archive contains 169 Mach-O files. A read-only codesign metadata audit found none bearing a Developer ID Application authority. This inventory is not proof of runtime failure or a notarization verdict, but confirms that signing only the outer app does not complete the distribution work. The audit extracted a separate copy outside the repository and did not alter the development runtime.

## Distribution candidate workflow

Formal distribution with `Developer ID Application: Skylance LLC (Apps) (T695TT4HT9)` was explicitly authorized on 2026-10-14. The local development identity remains unchanged. `scripts/build-distribution.py --output-parent /absolute/private-output` creates an isolated source copy, signs the embedded runtime before recompressing it, updates only the copy's digest, and builds the distribution candidate. It does not publish.

Configure notarytool interactively, using an app-specific password at its hidden prompt, never in command arguments or a project file:

```sh
xcrun notarytool store-credentials voicemore-skylance --apple-id YOUR_APPLE_ID --team-id T695TT4HT9
```

Inside the resulting candidate workspace, submit both executable runtime contents and the application:

```sh
ditto -c -k --keepParent runtime-stage/runtime runtime-notary.zip
xcrun notarytool submit runtime-notary.zip --keychain-profile voicemore-skylance --wait --output-format json > runtime-notary.json
ditto -c -k --keepParent build/darwin-arm64/Voicemore.app app-notary.zip
xcrun notarytool submit app-notary.zip --keychain-profile voicemore-skylance --wait --output-format json > app-notary.json
```

Require `Accepted` in both reports; obtain `notarytool log` for any rejection. Then run `python3 scripts/finalize-distribution.py /absolute/candidate-workspace --keychain-profile voicemore-skylance` from the project. This staples the app, creates a new DMG containing it and an Applications link, signs/submits/staples the DMG, and runs the read-only verification gate. It refuses an existing staging directory instead of overwriting a previous result. An interrupted finalization needs inspection before retrying. Successful local verification is not cross-Mac E2E or publication approval.

## Production acceptance requirements

Before changing signing identity again, obtain explicit authorization under AGENTS.md, identify the approved Developer ID Application team, and provision a matching private key on the build machine. Do not export a colleague's private key. Keep Bundle ID `dev.steve.voice-input` unchanged.

1. Audit and sign the relocated Python/MLX runtime's Mach-O executables and native libraries with the approved distribution identity and appropriate hardened-runtime entitlements. Do not rely on signing the outer app to sign code compressed in runtime.tar.gz.
2. Repack the runtime, update its pinned digest/distribution source, and build/sign the final app through a separate distribution configuration.
3. Submit the final app to Apple, require Accepted status, staple and validate its ticket. Then package the DMG, sign/notarize/staple it and validate it too. Never modify sealed app contents afterwards.
4. Generate update archives and Ed25519 manifests from the final app, not an earlier development build.
5. Run `scripts/verify-distribution.sh /absolute/Voicemore.app /absolute/Voicemore.dmg /absolute/evidence-directory`. This read-only gate checks both app and DMG; it does not sign, notarize, grant exceptions or publish.
6. Download the exact candidate on the second Mac with normal quarantine, drag-install and launch through Finder. Verify permissions, model deployment, dictation into TextEdit, history and relaunch. Preserve screenshots, hashes and reports. Only then promote a new version to stable; do not overwrite v0.3.1 assets.

The existing `verify-dmg.sh` remains a packaging check, not a public distribution acceptance check.

References: https://developer.apple.com/developer-id/ and https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution

## Small macOS icon compatibility

MyGo 0.4.8 embeds PNG data in the `icp4` / `icp5` entries of its generated AppIcon.icns. On the tested macOS version, extracting those with Apple iconutil produced corrupted 16/32-pixel images, matching the System Settings permission-list icon. `resources/Voicemore.icns` is generated from the existing artwork using `scripts/build-icon.sh` and Apple's encoder. CFBundleIconFile explicitly selects that resource; do not replace the artwork or reset TCC to repair icon rendering. Regenerate it when resources/icon.png changes.

## Text cleanup acceptance

A clean installation has no preselected cleanup URL/model. The bundled ASR model does not install oMLX or a text model. Enabling cleanup (home, settings, or tray) checks the selected provider and its model list asynchronously before saving the enabled state. Missing configuration, unreachable service, missing model, timeout, or a changed assignment keeps cleanup off without a modal. The home switch stays disabled until background readiness checks pass, and its hover hint explains that the text cleanup model is not configured. The adjacent chevron opens Models directly and clears stale provider dialogs. The redundant on/off text is omitted. Visible switches recheck readiness every 15 seconds and after configuration changes. Previously saved enabled state is rechecked at launch. Listing a model is a connectivity check, not a guarantee that inference succeeds; runtime errors must still preserve original text.
