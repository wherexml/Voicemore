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

The idle/current and retry states use a dedicated 24 px rail icon with a 1.2-unit stroke in a 16-unit viewBox, matching the Settings control. The clockwise arc ends exactly at the arrow tip (13, 5.5), with a clear opening below it. Rounded caps and joins stay consistent with the rail. Model-list refresh keeps its separate compact icon.

Visual acceptance: inspect the idle icon at native display scale, open its popover, close with Escape, and confirm the icon remains centered above Settings without a detached arrow or heavy junction. Save a native screenshot locally under `artifacts/update-ui/`.

## Repeatable native E2E

1. Build the app, then run `scripts/prepare-update-fixture.sh` with `SIGNING_IDENTITY`. Quit the regular app, run `python3 scripts/update-fixture.py`, and launch `artifacts/updater-e2e/Voicemore.app`.
2. Within ten seconds the rail shows an available update. Click it to start downloading and check that the Home body contains no version/status section. Capture the rail and open progress panel.
3. The default `corrupt` mode rejects the signed archive after download. Capture progress, then failure; compare the executable with `before.sha256` to confirm it remains unchanged.
4. Retry and cancel before completion. Confirm the available-update state returns and dictation is usable again. Escape dismisses the panel. Navigate History and Dictionary: the rail update entry remains available.
5. Write `current` to `artifacts/updater-e2e/mode`, relaunch the fixture, and confirm the rail stays icon-only after automatic checking. Open the panel to see the up-to-date text. Write `unavailable`, click Check for Updates, and confirm a retryable failure.
6. For the full authenticated install/relaunch acceptance, switch to `valid` and follow [UPDATES.md](UPDATES.md). Preserve screenshots and accessibility observations under `artifacts/update-ui/` locally; screenshots may contain personal dictation history and should not be uploaded to the public repository.

## Interaction boundaries

Direct installation requires an available version, no check/install in progress, no error detail, and no active dictation/model setup. Otherwise the rail opens the details panel. Known-length downloads show determinate progress; checking, verification, installation and restart use an indeterminate arc. Reduced Motion freezes that arc. The indigo accent is scoped to the updater. Include busy-state clicks and Reduced Motion in native acceptance, and record unexecuted cases explicitly.
