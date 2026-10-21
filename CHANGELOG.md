# Changelog

## Unreleased

- 更新就绪后保持“立即重启”可点击：忙碌时明确显示等待状态，当前任务结束后再提交更新；防止重复提交，保留失败恢复和签名校验。此行为尚未随正式版本发布。

## 0.3.12

- Preserve explicit device preferences during migration; do not invent a DJI receiver binding for older settings with no configured device.
- Preserve an empty auto-send application list across restarts and check actual local model files before showing an installed state.
- Improve model configuration, encrypted credential saving, AI application auto-send rules and Fn shortcut handling.
- Retry transient update connection failures and keep signed download credentials out of displayed errors.
- Dismiss concise dictation error summaries automatically while retaining history and diagnostics.

Existing legacy DJI entries are preserved because their origin is ambiguous; remove unwanted entries in external device button settings.

## 0.3.11

- Keep dictation, shortcuts and settings available while updates download and verify in the background. Install only after the user chooses Restart now while idle.
- Move the local ASR runtime out of the main installer and update archive. Download and verify it with the local model only when local recognition is requested; cloud recognition needs neither package.
- Reuse compatible installed runtimes and verified downloads, preserve existing runtimes on failure, and include the pinned runtime asset in release verification.
- Reduce updater icon sizes and progress-ring stroke weight.

更新下载和校验期间保持正常使用，就绪后提示重启，由用户选择立即重启再安装。本地识别运行环境与模型统一按需下载，使用豆包等云端识别无需下载本地资源；精简主安装包，保留环境缓存复用与完整性校验，并缩小更新图标。

## 0.3.10

- Show Voicemore Official as a dedicated built-in speech provider with the Voicemore mark and prefilled service address; retain existing sub2api settings and encrypted keys during upgrades.

- Preserve privacy when settings are damaged: require repair instead of trusting an older private-app list. Clear recovered history snapshots and orphan recordings, report deletion failures, and sanitize legacy API keys from settings backups after encrypted read-back.
- Save settings, history and dictionary atomically with recovery and visible persistence errors; avoid a save/quit deadlock and retain successfully inserted text when history cannot be saved.
- Reject typing shortcuts that could swallow normal input, constrain single-character corrections to unambiguous source phrases, and retain originals when cleanup output is truncated or exceeds its response limit.
- Add a persistent text-cleanup master switch, individual format preferences and editable polishing rules, with a fixed-height native rule editor.
- Add the configurable sub2api combined speech/cleanup service, per-record billing receipts, result retrieval and server-confirmed text preferences. Reduce queued-audio and verified native paste delays while preserving clipboard fallback protection.
- Improve native permission-card dragging, microphone start-state timing, and cloud speech-provider routing.

新增独立的 Voicemore Official 服务商入口，内置地址与模型，沿用已有 sub2api 配置和加密密钥。设置损坏时停止听写，避免旧备份撤销私密应用保护；清空历史同时删除恢复副本与孤立录音，删除失败明确提示；旧 API Key 经加密读回确认后净化所有配置快照。设置、历史和词典采用原子持久化并报告失败，修复保存通知与退出取消的死锁。限制危险快捷键和含糊单字替换，整理截断时保留原文。文字整理新增总开关、格式联动与持久化润色规则；接入可配置的 sub2api 组合语音服务、计费回执与原任务取回，改善录音积压和原生粘贴延迟，并修复权限卡片拖动及录音启动状态。

Known limitations / 已知限制：sub2api still performs server cleanup and billing when the client chooses raw ASR delivery; it does not support translation or term hints. Latency observations are individual measurements, not a performance guarantee. 客户端关闭整理仅选择原始 ASR 正文，组合服务仍按既有协议执行整理并计费；不支持翻译或热词。单次延迟样本不代表整体性能。官网方案不属于本次客户端发布。

## 0.3.9

- Add device button bindings for standard HID mouse auxiliary buttons, external keyboard keys and supported media keys; assign multiple buttons on the same device to dictation or translation.
- Suppress the original action only for the bound button on the matching device, while preserving unbound buttons and the same key on other devices. Group composite interfaces and show unsupported devices with a disabled reason.
- Add translation shortcuts and target-language selection, with separate translation prompts and history metadata.
- Keep the Developer ID identity consistent across local installs, official releases and authenticated automatic updates; provide an isolated development build.

新增通用设备按键绑定：鼠标辅助键、外接键盘普通键及支持的媒体键可分别绑定听写或翻译，同一设备可添加多个按键。仅拦截匹配设备上的已绑定按键，其他按键及其他设备同键正常使用；合并复合设备接口，并明确显示不支持原因。新增翻译快捷键与目标语言设置，统一正式安装和自动更新的 Developer ID 身份，开发构建使用独立应用与数据目录。

## 0.3.8

- Animate the native dictation capsule continuously into the send countdown: the shell, status text, icons and send controls now transition in one window with a fixed bottom edge and stable countdown width.
- Add a 24-hour bar chart for today on Home, selected by default, with hour filtering, keyboard navigation and empty-state handling; multi-day ranges keep the heatmap.
- Use the shared standard selector for Home trigger key, mode, recognition and cleanup models, microphone and date range, with a single visible check mark per choice.
- Open Settings at a centered 820×680 default size and disable full screen.
- Use macOS SF Symbols for copy, delete and update buttons, and keep the Home scrollbar from crowding the copy and delete actions.

原生听写胶囊连续过渡到发送倒计时：外框、状态文字、图标与发送控件在同一窗口内渐变，底边固定，倒计时宽度稳定。首页新增默认显示的今日 24 小时柱状图，支持按小时筛选、键盘导航与空态，多日范围保留热力图。首页触发键与方式、识别和整理模型、麦克风及日期范围统一使用标准选择器，选中项勾选清晰。设置窗口默认居中 820×680 并禁用全屏。复制、删除与更新按钮改用 macOS SF Symbols，首页滚动条不再挤占复制与删除按钮。

## 0.3.7

- Add local speech-language selection: Chinese by default, automatic detection, or English; use the same selection when retranscribing history.
- Add private applications: dictation still inserts, but a run touching a listed start or destination app saves no transcript or recording and produces no correction suggestions.
- Keep dictation and the optional send countdown in one continuous capsule. Default to insertion only; configure automatic sending by application, with a two-second delay, cancellation and editing.
- Improve AirPods playback and volume restoration without forcing sample rates or blocking transcript insertion.
- Introduce the five-step dark onboarding guide, native permission and shortcut detection, and inline speech-model setup.
- Add custom cleanup thinking modes and a real cleanup probe; disable DeepSeek thinking by default and improve paragraph grouping, disfluency removal and ambiguous-text preservation.
- Unify microphone and trigger-key selectors, restore keyboard focus after selection, and recover interrupted global keyboard listeners. Custom accelerators keep toggling even when an older setting retains hold mode.
- Refresh the multilingual website source, product screenshots and release links.

新增本地识别语言与私密应用：中文保持默认，支持自动识别和英语，历史重新转写共用语言设置；私密应用照常输入，但本次文字、录音及纠错学习不落盘。统一听写与发送倒计时胶囊，默认只输入，按应用配置自动发送，支持两秒延迟、取消与编辑。修复 AirPods 播放与音量恢复及听写收尾阻塞。更新固定深色五步引导、区域内模型操作、标准选择器及全局快捷键监听恢复。自定义整理服务支持思考模式与真实测试，DeepSeek 默认关闭思考，改善分段、去口吃和含糊字串保留；同步多语言官网源码与下载入口。

Known limitation / 已知限制：a concatenated Chinese/English fixture still loses its Chinese portion in automatic mode; this release does not claim that bilingual recognition quality is resolved. 拼接的中英混合样本在自动模式仍遗漏中文，不能据此宣称已解决混说准确率。

## 0.3.6

- Refresh the drag-to-install DMG with aligned waveform artwork, compact icon labels and a single-line English installation hint.
- Update native Home, history statistics, record cards and Dictionary layouts; restore window dragging and refine scrollbars.
- Keep history application names and icons consistent after insertion into another app.
- Improve numeric formatting and local ASR short-tail handling; enable Doubao DDC by default.
- Preserve terminology, qualifiers, tone and ambiguity during text cleanup, and report response-reading timeouts accurately.
- Support the DJI receiver connection button for dictation without conflicting with Bluetooth switching.

更新拖拽安装界面：声波与图标中心对齐，名称紧贴图标，采用单行英文安装提示。更新原生首页、历史统计与记录卡片、词典布局和窗口交互；修复跨应用写入后的历史名称与图标；优化数字格式、本地识别短尾处理及豆包 DDC 默认设置；改善文字整理的术语、限定、语气与歧义保留及超时诊断；支持 DJI 接收器连接键触发听写。

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
