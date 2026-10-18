# Signed client updates / 客户端签名更新

Voicemore uses its own staged installer with a pinned Ed25519 public key and the shared macOS signing policy in `internal/distribution/policy.json`. The production arm64 feed is:

`https://github.com/wherexml/Voicemore/releases/latest/download/update-darwin-arm64.json`

The app checks after launch and hourly. The bottom-left rail above Settings shows a quiet icon when current, an indigo download icon when available, a mint progress ring during transfer, and a retry icon on failure. Click an available update to start downloading immediately; click during a download or after a failure for details and actions; the Home body stays free of update notices. Escape or an outside click dismisses the panel. Clicking the available-update icon downloads and authenticates the archive before replacement, then relaunches through the normal shutdown path. The app's data directory is separate and never replaced. In-app notices work even when macOS notifications are denied. Update checks need GitHub access; offline failures allow manual retry.

## Publish a stable release

Run `./scripts/publish-release.sh --publish` for the automatic signed/notarized release workflow; omit `--publish` to prepare only. Reuse unchanged signed runtimes and select native regression tests by change scope. Full commands and required evidence are in [DISTRIBUTION.md](DISTRIBUTION.md). Both the DMG and update archive come from the final stapled app. No stable release is allowed without Apple notarization.

## Signing continuity and development isolation

Normal production builds, local repair installs and public updates use the same Developer ID team (`T695TT4HT9`) and bundle ID (`dev.steve.voice-input`). Use `./scripts/build-macos.sh` to prepare a notarized candidate, then `python3 scripts/install-macos.py /absolute/candidate-workspace` after quitting the installed app. The installer verifies both final artifacts and identity continuity. A certificate renewal in the same approved team is accepted only if the old designated requirement also accepts it; the runtime does not pin a leaf certificate fingerprint.

Development builds use `python3 scripts/build-development.py`: a separate **Voicemore Dev.app**, bundle ID, data directory and output directory, with no stable feed. The old development-preview publishing and production-data fixture are retired. Do not replace the regular app with a development-signed build.

A legacy development-signed app using the production bundle ID cannot silently transfer its TCC grants to the official team. That existing mismatch needs an explicitly managed transition and may require one final permission confirmation. A code change to these guards does not retrofit already installed old binaries. Do not reset permissions or replace a currently working app merely to validate the pipeline.

## Before replacement

1. Require HTTPS (loopback HTTP is supported for local acceptance fixtures), a bounded manifest/archive and a strictly newer three-component stable version.
2. Download into a unique staging directory next to the app. Verify the exact size and Ed25519 signature over SHA-256 using the original pinned key.
3. Extract through a confined filesystem root, rejecting traversal, external links, hardlinks, duplicate entries and excessive expansion. Defer symlinks until file extraction finishes.
4. Check manifest/bundle version agreement, the approved bundle ID and Developer ID requirement, the actual installed app's designated requirement and Gatekeeper acceptance of the candidate.
5. Recheck cancellation and that the target has not changed. Rename the old bundle to a unique retained backup, then install the staged candidate. Restore the old bundle if replacement fails. Relaunch through LaunchServices after the original process exits.

Download, validation and cancellation failures preserve the installed bundle. Successful upgrades retain a hidden `.Voicemore.app.update-*.previous.app` backup next to it; they never replace app data. Installation requires a writable directory, and never requests an administrator password or changes directory permissions. Dictation/model work blocks installation, and hotkeys pause during installation.

Archive authentication is independent of macOS signing/notarization; both must pass. Keep the existing private signing key outside Git and do not print or regenerate it. The stable publisher creates its archive only from the final stapled app, re-extracts it and verifies identity and ticket before signing the manifest. Required assets remain the DMG, tar.gz, manifest and checksums.

## Repeatable native installer acceptance

```sh
python3 scripts/accept-update-identity.py \
  --old /absolute/0.3.7/Voicemore.app \
  --archive /absolute/voicemore-0.3.8-darwin-arm64.tar.gz \
  --development /absolute/development/Voicemore.app \
  --out /absolute/private-evidence
```

The tagged acceptance executable uses disposable copies of real signed bundles and a loopback feed on port **48149**. Its transport fixtures use a temporary in-memory Ed25519 key; real app signatures and notarization are unchanged. It never launches the user's app, reads update private keys, touches TCC or user data, or publishes. `results.json` records the expected failure boundary, before/after executable hashes and the real successful upgrade with a retained original bundle. Generated fixture inputs are discarded after a successful run; the upgraded bundle, backup and JSON evidence remain.

Run a focused native launch/permission check on the actual installed update when installation is explicitly requested. A successful installer fixture proves signature continuity and replacement behavior, not physical Fn behavior or TCC grant preservation in a live process.

0.2.0 lacks automatic updates and requires a manual first upgrade. Drafts and prereleases are excluded from the stable/latest feed.
