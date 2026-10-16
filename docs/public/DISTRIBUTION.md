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

`scripts/publish-release.sh` defaults to the Developer ID and Apple notarization pipeline. Only explicit `--development-preview` builds a development **prerelease**, never stable/latest. Stable publication fails closed on missing/rejected notarization, invalid tickets, mismatched app/DMG contents, changed candidate source, an invalid explicitly supplied E2E report, or an incorrect updater key. Existing release assets are never overwritten.

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

Require `Accepted` in both reports; obtain `notarytool log` for any rejection. Then run `python3 scripts/finalize-distribution.py /absolute/candidate-workspace --keychain-profile voicemore-skylance` from the project. This staples the app, creates a new DMG containing it and an Applications link, signs/submits/staples the DMG, and runs the read-only verification gate. It refuses an existing output DMG instead of overwriting a previous result. An interrupted finalization needs inspection before retrying. Successful local verification does not claim cross-Mac E2E. Publication still requires user authorization, which an explicit release request supplies.

## Production acceptance requirements

Before changing signing identity again, obtain explicit authorization under AGENTS.md, identify the approved Developer ID Application team, and provision a matching private key on the build machine. Do not export a colleague's private key. Keep Bundle ID `dev.steve.voice-input` unchanged.

1. Audit and sign the relocated Python/MLX runtime's Mach-O executables and native libraries with the approved distribution identity and appropriate hardened-runtime entitlements. Do not rely on signing the outer app to sign code compressed in runtime.tar.gz.
2. Repack the runtime, update its pinned digest/distribution source, and build/sign the final app through a separate distribution configuration.
3. Submit the final app to Apple, require Accepted status, staple and validate its ticket. Then package the DMG, sign/notarize/staple it and validate it too. Never modify sealed app contents afterwards.
4. Generate update archives and Ed25519 manifests from the final app, not an earlier development build.
5. Run `scripts/verify-distribution.sh /absolute/Voicemore.app /absolute/Voicemore.dmg /absolute/evidence-directory`. This read-only gate checks both app and DMG; it does not sign, notarize, grant exceptions or publish.
6. For first distribution setup, signing/permission/installation changes or a cross-Mac regression, download the exact candidate on a second Mac with normal quarantine, drag-install and launch through Finder. Verify the affected permissions, model, dictation, history or relaunch behavior. Preserve evidence. Routine releases use automatic artifact acceptance without this repeated manual gate. Never overwrite existing public assets.

The existing `verify-dmg.sh` remains a packaging check, not a public distribution acceptance check.

References: https://developer.apple.com/developer-id/ and https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution

## Small macOS icon compatibility

MyGo 0.4.8 embeds PNG data in the `icp4` / `icp5` entries of its generated AppIcon.icns. On the tested macOS version, extracting those with Apple iconutil produced corrupted 16/32-pixel images, matching the System Settings permission-list icon. `resources/Voicemore.icns` is generated from the existing artwork using `scripts/build-icon.sh` and Apple's encoder. CFBundleIconFile explicitly selects that resource; do not replace the artwork or reset TCC to repair icon rendering. Regenerate it when resources/icon.png changes.

## Text cleanup acceptance

A clean installation has no preselected cleanup URL/model. The bundled ASR model does not install oMLX or a text model. Enabling cleanup (home, settings, or tray) checks the selected provider and its model list asynchronously before saving the enabled state. Missing configuration, unreachable service, missing model, timeout, or a changed assignment keeps cleanup off without a modal. The home switch stays disabled until background readiness checks pass, and its hover hint explains that the text cleanup model is not configured. The adjacent chevron opens Models directly and clears stale provider dialogs. The redundant on/off text is omitted. Visible switches recheck readiness every 15 seconds and after configuration changes. Previously saved enabled state is rechecked at launch. Listing a model is a connectivity check, not a guarantee that inference succeeds; runtime errors must still preserve original text.

## Default stable release commands

1. Bump `mygo.json`, update `CHANGELOG.md` and commit the intended source. Preserve unrelated work in progress; publication compares the source inventory to HEAD.
2. Run `./scripts/publish-release.sh --publish` for an authorized one-command release. It reuses a matching verified runtime cache, signs the new app, automatically requires Apple Accepted for app and DMG, staples tickets, verifies the final artifacts, signs the update manifest and uploads a draft. Downloaded asset hashes must match before stable/latest. No extra approval or second-Mac ceremony is built into the routine path.
3. To prepare without publishing, omit `--publish`. Publish later with `--candidate /absolute/workspace --publish`; resume rechecks live Apple status and final artifacts. Use `--candidate /absolute/workspace --verify-only` to exercise the same automatic gate without publishing. A single uninterrupted prepare-and-publish invocation does not repeat the finalizer's just-completed gate.
4. Use focused native regression checks for runtime/audio/model changes, permissions/entitlements/signing identity changes, installation/updater changes or a known regression. First-time setup warrants full cross-Mac acceptance. An unchanged runtime does not need model redownload or virtual microphone setup on every release.
5. When comprehensive native acceptance is requested, add `--full-e2e --e2e /absolute/report.json`. The report must include `version`, `host`, `dmg_sha256`, and true `checks` for `quarantine`, `gatekeeper`, `installed_app_matches`, `launch`, `permissions`, `model`, `dictation`, `history`, and `relaunch`. An explicitly supplied invalid report always blocks, even without `--full-e2e`. Do not label automatic artifact acceptance as native E2E.
6. Check the public release and latest manifest. Python `cryptography` is required for update signing. Existing public releases are never overwritten. Inspect interrupted drafts before recovery.

### Signed runtime cache

`scripts/distribution_cache.py` stores one private content-addressed entry under `~/Library/Caches/voicemore-release/runtime-cache`. Its key combines the original runtime archive SHA-256, exact Developer ID certificate fingerprint, platform/architecture and explicit signing/entitlement policy. Increment the policy when signing behavior changes. Cache entries include the unchanged signed compressed archive, archive hash, Mach-O count and Accepted runtime notary receipt. Before reuse, hash and all Mach-O signatures, Developer ID identity, secure timestamps and hardened-runtime flags are verified. A hit copies the exact archive without re-signing, recompressing or re-submitting that runtime. The new app and DMG still need their own notarization. A corrupt entry fails closed; a changed key produces a fresh signing run.

After Apple accepts a newly signed runtime, the pipeline atomically caches it. The v0.3.4 Accepted workspace has been seeded locally so the next release can hit immediately. To seed another compatible accepted workspace, use `python3 scripts/distribution_cache.py --seed-workspace /absolute/workspace`. `--cache-dir` can isolate a cache for diagnostics. Do not store signing credentials in it. Legacy workspaces can be seeded only if their original runtime pin still matches the repository. Cache validation does not prove an app has passed functional tests.

Transient Apple timestamp failures are retried up to five times without removing the timestamp requirement. An interrupted signing workspace can be resumed with `build-distribution.py --output-parent /absolute/parent --resume /absolute/workspace`; completed candidates cannot be rebuilt in place.

## v0.3.4 distribution acceptance (2026-10-14)

The release app uses committed client source from `33c4b89`, with the version changed to 0.3.4 and distribution-only signing/runtime changes. Concurrent uncommitted client features are excluded. Apple accepted the separately signed runtime, app and DMG; the app and DMG have validated stapled tickets.

On the second Apple Silicon Mac (macOS 26.2), Chrome downloaded the final DMG over HTTPS with its normal quarantine attribute. Finder drag-install replaced the old app. Strict signature verification, Gatekeeper, app/DMG tickets and byte-for-byte installed-app comparison all passed. Gatekeeper remained enabled. The existing local model loaded using the new signed runtime. BlackHole 2ch supplied a synthetic Chinese speech fixture to the real microphone capture path; the transcript was automatically inserted into an empty TextEdit document. History, playback progress, permissions and relaunch were verified. This test used the tray start/stop command, not a physical microphone or a physical Fn key. It reused existing model weights and permissions rather than testing a fresh model download or first-time permission grants.

The exact DMG SHA-256 is `c90fd16ae149f3a6d012c9dffc299e17bbdc4f8745de408971c3309f7f1481c4`. Repeatable steps, native screenshots, browser quarantine evidence, verification logs and the publication gate report are kept locally under `artifacts/release-0.3.4/` and are not committed. The three static website language pages are updated; no production website has been deployed.

## Finder installer presentation

Both local and notarized packaging use `scripts/build-dmg.sh`; see [DMG installer specification](DMG-INSTALLER.md) for artwork, fixed-English instructions and acceptance. The shared builder preserves the signed/stapled app and refuses existing output paths. Formal signing/notarization gates remain after layout packaging.

## v0.3.6 distribution acceptance (2026-10-16)

Published stable/latest from committed source `4ad89af`, excluding concurrent uncommitted changes. The approved compact-label waveform DMG artwork is included. The existing signed runtime cache was reused after verifying 169 Mach-O signatures; Apple accepted the new app (`8d08f142-a318-42ca-9804-d46781a42c01`) and DMG (`8639d42d-0783-43c4-93b9-59fd192df948`).

App/DMG Developer ID signatures, stapled tickets, Gatekeeper, mounted app byte consistency, copied-install signatures, Finder layout, Applications link and runtime checksum passed. The update archive was extracted and its signature/ticket verified; its manifest uses the existing Ed25519 key. All four GitHub assets were downloaded and matched before stable/latest publication. Final DMG SHA-256: `84a6154ed48e8ee991aecd339546b65ef2775072f8c05095d9a6c39b8425bbe2`.

The candidate runtime and committed worker also passed real-audio WebSocket checks for the 30-second boundary, a short reply, silence, cancellation and recovery. This is automatic artifact acceptance plus protocol regression, not repeated cross-Mac microphone/permission/native dictation E2E. Repeatable scripts and reports are local under `artifacts/release-0.3.6/`. Release: https://github.com/wherexml/Voicemore/releases/tag/v0.3.6
