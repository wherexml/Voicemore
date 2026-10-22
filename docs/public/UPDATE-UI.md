# Update interface acceptance

Updates live in the bottom-left rail, directly above Settings, on every page. The Home body has no persistent version or update status text. Click an available update to download immediately; click while downloading or after an error for details; Escape, the close button, and an outside click dismiss the popover without cancelling a download.

| State | Rail control | Panel action |
| --- | --- | --- |
| Idle / current | Quiet refresh icon | Check for Updates |
| Checking | Rotating mint arc (static with reduced motion) | Disabled check button |
| New version | Indigo download icon; “Update available” tooltip | Download update |
| Active dictation / model setup | Update available / downloading | Background download allowed; Restart now queues a request until idle |
| Downloading | Mint progress ring + percentage tooltip | Progress bar and Cancel Update |
| Download complete | Verifying | Background verification; normal use remains available |
| Verified / ready | Compact refresh icon | Restart now; dismiss to continue working |
| User requests restart while idle | Restarting | Commit verified update, then restart |
| Cancelled | Update available | Retry installation |
| Failure | Indigo retry icon | Retry; technical details in tooltip |
| Relaunch failure | Indigo retry icon | Restart now, without downloading again |

Idle/current panels show the current version and check action without an automatic-check explanation. Errors and download status remain visible.

## Refresh icon

The idle/current and retry states use Apple's original SF Symbols `arrow.clockwise`, exported from macOS with Regular weight and Medium scale. Refresh and download icons are 16×16 DIP, centered in a 26×26 DIP circular face; the click target remains 44×44 DIP. Checking and download progress use an 18×18 DIP canvas with a 1.5 DIP stroke. These compact dimensions apply across all updater states, including retry and installation. The 128×128 white alpha mask is tinted with the current theme color, including the existing retry accent. See [system symbol assets](../../resources/symbols/README.md) for the reproducible export command. Model-list refresh keeps its separate compact icon.

Visual acceptance: build and launch the locally signed app, inspect the idle icon at native display scale, open its popover, close with Escape, and confirm the smaller system symbol remains centered above Settings. Save a native screenshot locally under `artifacts/update-ui/`. The screenshot and a short result record are local evidence, not repository assets.

## Repeatable native E2E

1. Run the disposable signed-app installer acceptance from [UPDATES.md](UPDATES.md#repeatable-native-installer-acceptance). The former production-data fixture is retired. For UI/native launch acceptance use an explicitly authorized isolated build and record the actual feed and bundle identity; never replace the regular app with a differently signed fixture.
2. With an explicitly configured isolated native acceptance client, show an available version and click the rail to start downloading. Check that the Home body has no version/status section; capture the rail and progress panel.
3. Serve a corrupt archive: verification must fail and the installed executable hash must remain unchanged. Retry and cancel before completion; confirm dictation remains usable throughout and Escape dismisses the panel.
4. Serve a current version, an unavailable feed and malformed metadata in separate runs. Confirm icon-only current state and retryable failure states. Navigate History and Dictionary: the rail entry remains available.
5. For an authorized real install/relaunch check, use a compatible officially signed candidate and preserve the original bundle. Verify the relaunched version and actual permission status. A development-only identity cannot receive the stable feed.
6. Keep screenshots, feed inputs, bundle metadata, executable hashes and unverified cases under `artifacts/update-ui/`; do not upload personal history to the repository. The automatic installer harness does not imply UI/relaunch or physical Fn acceptance.

## Interaction boundaries

Direct download requires an available version, no check/download/commit in progress, no staged update, and no error detail. Downloads and verification do not disable dictation, translation, hotkeys, settings, model setup or ordinary quit. After preparation, show a restart reminder and wait for explicit Restart now. Restart now remains clickable during active work and shows a waiting title after the request. Commit starts automatically once dictation/model setup and pending Home startup are idle; only this short phase suspends triggers and quit. Closing the panel does not cancel an explicit restart request; ordinary quit remains available while waiting. Otherwise the rail opens the details panel. Progress redraws are limited to 10 per second, plus the final byte count. Known-length downloads show determinate progress; checking, verification, installation and restart use an indeterminate arc. Reduced Motion freezes that arc. The indigo accent is scoped to the updater. Include busy-state clicks and Reduced Motion in native acceptance, and record unexecuted cases explicitly.

## Background update regression (2026-10-18)

The installer separates `Prepare` (download, authentication, extraction and verification) from `Prepared.Commit` (revalidation, bundle swap with rollback). Closing a prepared update discards its staging folder. The native installer harness also covers prepare/discard, cancelled commit and target replacement between prepare and commit; `--next-version` selects the fixture archive version. These fixtures do not establish physical Fn or live audio acceptance. Native regression must additionally exercise listening/cleanup/translation and settings during a throttled download, defer restart while busy, dismiss the ready reminder and explicitly restart when idle.

## Update network failures (2026-10-19)

Update GET requests retry timeouts and HTTP 408/429/502/503/504 at most three times, with cancellable one- and two-second delays. Each attempt starts from the original URL so expiring release redirects are refreshed. Check operations have a 45-second total deadline; TLS verification and HTTPS downgrade protection remain mandatory. Response-body or signature failures are not retried. Errors shown by the update panel omit URL query strings by displaying the server hostname and underlying request error. Check/download failures do not suspend dictation.

Repeatable HTTPS acceptance: `go run -tags updateacceptance ./cmd/update-network-acceptance /absolute/path/to/Voicemore.app > artifacts/update-network/results.json`. Use an existing officially signed app; the harness only reads its identity/version and contacts local fixtures, without installation. It covers a real TLS handshake timeout followed by recovery, retry exhaustion, cancellation, HTTP failures, untrusted certificates, downgrade rejection and signed-link redaction. This does not establish the network route or native visual behavior on another Mac.

## 更新入口 Mock（2026-10-19）

当前原生代码启动约 10 秒后检查更新，此后每小时检查；原生左下角仍有常驻入口。新 mock 方案仅在已发现新版本或下载/安装过程中显示左下角入口，已是最新版、无已知新版本的检查中及检查失败均不显示。手动“检查更新”位于设置 → 通用 → 软件更新，检查中禁用按钮，结果在同一行反馈；安装失败保留入口。

共享实现：`mock/components/update-entry.js`，用于 `auto-send.html` 与 `history-v3.html`。统一导航新增“检查更新”和“发现新版本”预览；`?update=current|available|checking|downloading|failed` 固定预览状态，普通入口使用 10 秒/1 小时模拟调度。检查结果为 mock 数据，不访问发布服务。尚未修改原生入口或发布。

可重跑交互验收：`ego-browser nodejs < artifacts/update-entry-mock-20261019/verify.mjs`（脚本中的 taskSpace ID 需替换为当前任务空间）。覆盖两页的手动检查、检查失败、新版本入口、详情、Escape、下载中与安装失败；结果保存在同目录 `results.json`。

该结果属于 2026-10-19 的本地浏览器记录。2026-10-22 文档核对仅确认共享脚本、状态参数、原生调度与既有证据文件存在，未重跑交互；本节不覆盖下述原生重启请求规格。

## 重启请求验收（2026-10-21）

更新就绪后，“立即重启”须响应点击；当前听写、模型准备或首页启动倒计时未结束时，显示“等待当前任务结束…”，保留已校验候选包。每 250 毫秒复查空闲状态，仅在空闲后提交；重复点击不得并发提交，退出时不执行迟到回调。关闭面板不撤销已确认的重启请求。

原生隔离夹具：先运行 `python3 scripts/build-development.py`，再以相同绝对证据目录设置 `VOICE_INPUT_SEND_PREVIEW_ACCEPTANCE` 与 `VOICE_INPUT_UPDATE_UI_ACCEPTANCE` 后启动开发版可执行文件。`VOICE_INPUT_UPDATE_UI_BUSY=1` 模拟首页未完成的启动；点击面板按钮，应显示等待标题，`state.json` 中 `restartRequested=true`、`committing=false`。向证据目录 `command` 文件写入 `idle` 后应触发提交并安全显示校验失败。夹具只允许独立 Dev 名称，使用空候选对象，身份校验前置失败，绝不安装或重启正式应用。另以默认空闲启动验证鼠标点击，关闭/Escape 后重开验证候选保留。结果与操作记录保存到 `artifacts/update-restart-20261021/`。这不代表另一台 Mac 的失效根因或正式更新重启已通过验收。
