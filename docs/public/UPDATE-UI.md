# Update interface acceptance

Updates live in the bottom-left rail, directly above Settings, on every page. The Home body has no persistent version or update status text. Click an available update to download immediately; click while downloading or after an error for details; Escape, the close button, and an outside click dismiss the popover without cancelling a download.

| State | Rail control | Panel action |
| --- | --- | --- |
| Idle / current | Quiet refresh icon | Check for Updates |
| Checking | Rotating mint arc (static with reduced motion) | Disabled check button |
| New version | Indigo download icon; “Update available” tooltip | Download update |
| Active dictation / model setup | Update available | Installation disabled, explanation shown |
| Downloading | Mint progress ring + percentage tooltip | Progress bar and Cancel Update |
| Download complete | Installing | Verification/installation message, no misleading cancel |
| Installed | Restarting | Automatic restart after active work ends |
| Cancelled | Update available | Retry installation |
| Failure | Indigo retry icon | Retry; technical details in tooltip |
| Relaunch failure | Indigo retry icon | Restart now, without downloading again |

Idle/current panels show the current version and check action without an automatic-check explanation. Errors and download status remain visible.

## Refresh icon

The idle/current and retry states use Apple's original SF Symbols `arrow.clockwise`, exported from macOS with Regular weight and Medium scale. The visible icon is 16×16 DIP, centered in the existing rail control; the click target remains unchanged. The 128×128 white alpha mask is tinted with the current theme color, including the existing retry accent. See [system symbol assets](../../resources/symbols/README.md) for the reproducible export command. Model-list refresh keeps its separate compact icon; download/progress states keep their existing indicators.

Visual acceptance: build and launch the locally signed app, inspect the idle icon at native display scale, open its popover, close with Escape, and confirm the smaller system symbol remains centered above Settings. Save a native screenshot locally under `artifacts/update-ui/`. The screenshot and a short result record are local evidence, not repository assets.

## Repeatable native E2E

1. Run the disposable signed-app installer acceptance from [UPDATES.md](UPDATES.md#repeatable-native-installer-acceptance). The former production-data fixture is retired. For UI/native launch acceptance use an explicitly authorized isolated build and record the actual feed and bundle identity; never replace the regular app with a differently signed fixture.
2. With an explicitly configured isolated native acceptance client, show an available version and click the rail to start downloading. Check that the Home body has no version/status section; capture the rail and progress panel.
3. Serve a corrupt archive: verification must fail and the installed executable hash must remain unchanged. Retry and cancel before completion; confirm dictation becomes usable again and Escape dismisses the panel.
4. Serve a current version, an unavailable feed and malformed metadata in separate runs. Confirm icon-only current state and retryable failure states. Navigate History and Dictionary: the rail entry remains available.
5. For an authorized real install/relaunch check, use a compatible officially signed candidate and preserve the original bundle. Verify the relaunched version and actual permission status. A development-only identity cannot receive the stable feed.
6. Keep screenshots, feed inputs, bundle metadata, executable hashes and unverified cases under `artifacts/update-ui/`; do not upload personal history to the repository. The automatic installer harness does not imply UI/relaunch or physical Fn acceptance.

## Interaction boundaries

Direct installation requires an available version, no check/install in progress, no error detail, and no active dictation/model setup. Otherwise the rail opens the details panel. Known-length downloads show determinate progress; checking, verification, installation and restart use an indeterminate arc. Reduced Motion freezes that arc. The indigo accent is scoped to the updater. Include busy-state clicks and Reduced Motion in native acceptance, and record unexecuted cases explicitly.
