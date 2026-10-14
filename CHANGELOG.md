# Changelog

## 0.3.2

- Add first-run onboarding with a safe dictation practice flow.
- Refresh Home, Settings, history, and the recording overlay.
- Support microphone selection, device-loss recovery, and configurable recording sounds.
- Add configurable transcript styling and retain text when dictation cannot be inserted.
- Preserve recognition context across thinking pauses in local ASR.
- Add a multilingual product website, refreshed app icon, and distribution verification tools.
- Restrict the preview publishing workflow to development prereleases.

新增首次使用引导和听写练习，调整首页、设置、历史与录音浮层；支持麦克风选择、设备断开恢复、录音提示音和转写文本整理，改善本地识别在思考停顿时的上下文保留。新增多语言官网、应用图标及分发验证工具，开发预览发布限定为 prerelease。

## 0.3.1

- Move updates to a compact top-right header control on every page.
- Remove persistent update/version text from the Home body.
- Add a dismissible panel for available versions, download progress, verification and installation, cancellation, restart and retry.
- Open update details from system notifications and the menu-bar check action.

更新入口移至右上角，首页不再常驻版本提示。新增紧凑浮层，展示新版本、下载进度、安装、取消、重启及失败重试等状态。

## 0.3.0

- Check for signed updates at startup and hourly while running.
- Show available versions on Home, in the menu-bar tooltip, and via an optional macOS notification.
- Update from Home in one click: download progress, Ed25519 verification, app replacement and automatic restart.
- Support cancellation and retry; protect active dictation/model work and retain existing data.
- Add a repeatable signed-release publishing workflow.

客户端启动时和每小时检查更新；首页一键下载、验签、安装并自动重启。支持取消和重试，保留原有数据，避免打断听写和模型部署。

Distribution remains development-signed and unnotarized; update archive signatures are separate from Apple notarization.
