# Update interface acceptance

Updates live in the header on every page. The Home body has no persistent version or update status text. Click the header control for details; Escape, the close button, and an outside click dismiss the popover without cancelling a download.

| State | Header | Panel action |
| --- | --- | --- |
| Idle / current | Quiet refresh icon | Check for Updates |
| Checking | Spinner | Disabled check button |
| New version | Blue “Update available” | Update & Restart |
| Active dictation / model setup | Update available | Installation disabled, explanation shown |
| Downloading | Spinner + percentage | Progress bar and Cancel Update |
| Download complete | Installing | Verification/installation message, no misleading cancel |
| Installed | Restarting | Automatic restart after active work ends |
| Cancelled | Update available | Retry installation |
| Failure | Amber attention badge | Retry; technical details in tooltip |
| Relaunch failure | Amber attention badge | Restart now, without downloading again |

## Repeatable native E2E

1. Build the app, then run `scripts/prepare-update-fixture.sh` with `SIGNING_IDENTITY`. Quit the regular app, run `python3 scripts/update-fixture.py`, and launch `artifacts/updater-e2e/Voicemore.app`.
2. Within ten seconds the header shows an available update. Open it and check that the Home body contains no version/status section. Capture the header and open panel.
3. The default `corrupt` mode rejects the signed archive after download. Capture progress, then failure; compare the executable with `before.sha256` to confirm it remains unchanged.
4. Retry and cancel before completion. Confirm the available-update state returns and dictation is usable again. Escape dismisses the panel. Navigate History and Dictionary: the header update entry remains available.
5. Write `current` to `artifacts/updater-e2e/mode`, relaunch the fixture, and confirm the header stays icon-only after automatic checking. Open the panel to see the up-to-date text. Write `unavailable`, click Check for Updates, and confirm a retryable failure.
6. For the full authenticated install/relaunch acceptance, switch to `valid` and follow [UPDATES.md](UPDATES.md). Preserve screenshots and accessibility observations under `artifacts/update-ui/` locally; screenshots may contain personal dictation history and should not be uploaded to the public repository.
