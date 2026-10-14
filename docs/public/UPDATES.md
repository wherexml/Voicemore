# Signed client updates / 客户端签名更新

Voicemore uses MyGo's native updater with a pinned Ed25519 public key. The production arm64 feed is:

`https://github.com/wherexml/Voicemore/releases/latest/download/update-darwin-arm64.json`

The app checks after launch and hourly. The bottom-left rail above Settings shows a quiet icon when current, an indigo download icon when available, a mint progress ring during transfer, and a retry icon on failure. Click an available update to start downloading immediately; click during a download or after a failure for details and actions; the Home body stays free of update notices. Escape or an outside click dismisses the panel. Clicking the available-update icon downloads and authenticates the archive before replacement, then relaunches through the normal shutdown path. The app's data directory is separate and never replaced. In-app notices work even when macOS notifications are denied. Update checks need GitHub access; offline failures allow manual retry.

## Publish a stable release

Run `./scripts/publish-release.sh` for the default signed/notarized candidate workflow, complete cross-Mac E2E, then publish that verified candidate. Full commands and required evidence are in [DISTRIBUTION.md](DISTRIBUTION.md). Both the DMG and update archive come from the final stapled app. No stable release is allowed without Apple notarization.

## Publish a development preview

Stable distribution requires the Developer ID and notarization workflow. See [distribution requirements](DISTRIBUTION.md). The following command intentionally publishes a prerelease, which existing clients do not receive through the stable feed.

1. Change `version` in `mygo.json` and add a matching `## x.y.z` section in `CHANGELOG.md`.
2. Complete acceptance, commit all changes, and make sure the working tree is clean.
3. Provide the original update signing key through `MYGO_UPDATER_PRIVATE_KEY`, or a private file via `VOICEMORE_UPDATE_KEY_FILE` (default `~/.config/voicemore/update-keys/mygo-update.key`). Never commit it, print it, regenerate it for each release, or replace the pinned public key casually.
4. Run:

   ```sh
   SIGNING_IDENTITY='Apple Development: geekmooc@gmail.com (5QLDC77G39)' ./scripts/publish-release.sh --development-preview
   ```

The script builds the DMG and signed archive, verifies packaging, pushes the matching commit/tag, uploads a **draft prerelease** with all assets, then publishes it with **latest=false**. If an upload fails, the draft stays invisible to update checks. Existing releases are not overwritten. GitHub **prereleases and drafts do not become this feed's latest release**. Publishing a DMG alone is insufficient.

Required assets: `Voicemore-x.y.z-arm64.dmg`, `voicemore-x.y.z-darwin-arm64.tar.gz`, `update-darwin-arm64.json`, `SHA256SUMS.txt`.

The private signing key stays outside Git and is required for all future updates. Keep a secure backup. Archive authentication is separate from macOS signing/notarization: stable distribution additionally requires Developer ID signing, Apple notarization and stapled tickets.

## Repeat the native E2E acceptance

Build a signed release first, then:

```sh
SIGNING_IDENTITY='Your macOS signing identity' ./scripts/prepare-update-fixture.sh
python3 scripts/update-fixture.py
```

Quit normal Voicemore before launching `artifacts/updater-e2e/Voicemore.app`. This is a local acceptance client with version 0.2.9 and a loopback feed on port **48139**, never a public release. It uses the existing data directory; do not run it concurrently with the regular app.

- Default `corrupt` mode: discover the update and click install. Signature verification must fail; compare the installed executable with `before.sha256` and confirm the client remains usable.
- Start a download and cancel: app stays on 0.2.9 and the update can be retried.
- Write `valid` to `artifacts/updater-e2e/mode`, then retry. The signed real release must replace the fixture and restart; the update panel must show the new version. Compare bundle version/executable with the built release and confirm existing data is preserved.
- `unavailable` / `malformed` modes exercise feed errors before an update is discovered.
- Verify a current production build checks the public feed and reports up to date.

Save AX snapshots, screenshots, before/after hashes and process/bundle versions under `artifacts/`; these private acceptance records are excluded from Git. The fixture server can be stopped with Ctrl-C after acceptance.

## 中文

当前发布脚本默认执行正式签名和 Apple 公证流程；正式通道必须通过票据校验与跨 Mac 安装验收后才能发布，见 DISTRIBUTION.md。只有显式的 `--development-preview` 才允许开发签名，并限制为 prerelease、不设为 latest。GitHub draft/prerelease 不参与当前更新通道。

0.2.0 没有更新功能，需手动安装一次 0.3.0。此后左下角设置上方的更新入口可一键更新并自动重启。更新包经过独立签名验证；下载取消、验签失败不会替换原应用。录音或模型部署时暂不可开始更新；安装时暂停快捷键听写。安装目录不可写时提示移动到可写位置，不索取管理员密码或修改目录权限。
