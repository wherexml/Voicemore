# Changelog

## 0.3.5

- Restore visible Settings and Dictionary groups, clearer provider fields, and aligned model pickers.
- Simplify correction candidates to original input and direct keyword/replacement actions.
- Refresh Home, history timeline, onboarding and shared permission guidance.
- Load local ASR on demand, release idle model memory, and refresh sentence-tail previews after pauses.
- Improve cloud hotwords, correction learning after send/append, and remote desktop input.
- Keep dictation capsules with the active display and desktop.
- Fix README image links and update interface documentation.

恢复设置与词典分组底色，简化纠正候选，优化模型选择与输入框；更新首页、历史时间轴及权限引导；本地模型按需加载并在空闲时释放内存，改善句尾预览、云端热词、发送与追加输入后的纠正学习及远程输入；胶囊跟随当前屏幕和桌面。修复 README 图片链接。

## 0.3.4

- Ship Developer ID signed, Apple-notarized app and DMG with stapled tickets.
- Require notarization, Gatekeeper checks and native cross-Mac acceptance before stable publication.
- Add remote desktop input preferences and refine cleanup readiness and update navigation.

首次提供完成 Developer ID 正式签名与 Apple 公证的安装包，正式发布强制通过公证、Gatekeeper 与跨 Mac 验收；新增远程桌面输入配置，优化文字整理就绪检查及更新入口。由开发签名旧版升级后，可能需要重新确认系统权限。

## 0.3.3

- Add an indigo dictation capsule with live transcript expansion, animated status, and recovery actions; keep the classic capsule available in Settings.
- Keep copied-text feedback visible until dismissed in the new capsule.
- Refine Settings controls, shortcut labels, Home navigation, and the dictionary card layout.
- Include the onboarding, microphone, transcription, and packaging improvements documented in 0.3.2.

新增靛蓝听写胶囊，支持实时转写展开、状态动画与失败恢复操作；设置中可切回经典样式。复制反馈保留至主动关闭，优化设置控件、快捷键标识、首页导航和词典卡片布局。本次同时包含 0.3.2 的首次使用引导、麦克风和转写改进。

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
