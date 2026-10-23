# 模块文档索引

- [Voicemore Official 组合语音、文字设置、最终文字计费与恢复验收](../../internal/cloudasr/README.md#voicemore-officialsub2api-组合服务)

用户入口见 [中文 README](../../README.zh-CN.md) 与 [English README](../../README.md)。本目录保存可公开的行为规格；实现细节放在对应模块旁，按需继续阅读。

| 模块 | 规格与验收 | 实现入口 |
| --- | --- | --- |
| DMG 安装窗口 | [布局、固定英文提示与原生验收](DMG-INSTALLER.md) | `scripts/build-dmg.py`、`scripts/verify-dmg-layout.py` |
| 首页与历史 | [布局、功能与原生验收](HOME-HISTORY.md)、[首页标准选择器](HOME-HISTORY.md#首页标准选择器2026-10-17)、[立即听写](HOME-HISTORY.md#首页听写立即开始2026-10-23) | `home.go`、`journal.go`、`history.go`、`history_stats.go`、`app_identity.go`、`action_icons.go`、`prefs.go`、`select.go` |
| 权限授权 | [共享拖拽引导、签名授权恢复与原生验收](PERMISSIONS.md) | `permission_drag.go`、`internal/macos/file_drag_darwin.go`、`internal/macos/settings_window_darwin.go` |
| 开始使用引导 | [窗口布局与原生验收](ONBOARDING.md) | `onboarding.go`、`onboarding_bridge.go`、`resources/onboarding/` |
| 全局触发键 | [后台监听、主动恢复与原生验收](HOTKEY.md) | `internal/macos/hotkey_darwin.go` |
| 蓝牙音频恢复 | [AirPods 播放恢复与实机验收](BLUETOOTH-AUDIO.md) | `internal/macos/bluetooth_output_darwin.go`、`internal/macos/mic_darwin.go` |
| 翻译听写 | [快捷键、官方组合服务与失败恢复](TRANSLATION.md) | `dictation_controls.go`、`internal/session/controller.go`、`internal/cleanup/cleanup.go` |
| 外接设备按键 | [通用添加、识别与多绑定](DEVICE-BUTTONS.md) | `dictation_controls.go`、`internal/macos/device_buttons_darwin.go`、`internal/macos/button_capabilities.go`、`internal/macos/device_button_interception_darwin.go` |
| DJI 麦克风按钮 | [设备匹配与原生验收](DJI-BUTTON.md) | `internal/macos/dji_darwin.go` |
| 听写胶囊 | [显示规则与原生验收](CAPSULE.md) | `overlay.go`、`overlay_v2.go` |
| 远程桌面输入 | [匹配规则、剪贴板与原生验收](REMOTE-INPUT.md) | `prefs_remote.go`、`internal/macos/remote_darwin.go`、`internal/session/controller.go` |
| 本地语音模型 | [按需加载、句尾预览与验收](LOCAL-ASR.md)、[识别语言](LOCAL-ASR.md#识别语言)、[Qwen3-ASR 自定义能力与参数](QWEN3-ASR-CUSTOMIZATION.md) | `internal/localasr/`、`internal/session/controller.go` |
| 云端语音识别 | [Seed ASR 2.0、Agent Plan 与豆包 DDC 验收](../../internal/cloudasr/README.md) | `internal/cloudasr/volc.go`、`internal/providers/providers.go` |
| 私密应用 | [判定、延迟落盘与原生验收](PRIVATE-APPS.md) | `internal/session/privacy.go`、`prefs_privacy.go` |
| 配置持久化 | [状态保存、隐私恢复保护、清空与凭证净化验收](../../internal/config/README.md#状态持久化与恢复)、[保存与退出的锁边界](../../internal/config/README.md#保存与退出的锁边界) | `internal/config/config.go`、`internal/config/json.go`、`internal/config/legacy_key.go`、`internal/store/store.go`、`internal/session/privacy.go` |
| 词典与纠正学习 | [手动替换、词条编辑、热词与纠正验收](DICTIONARY.md) | `dictionary.go`、`internal/dict/`、`internal/cloudasr/volc.go`、`internal/session/controller.go` |
| 基础格式与数字 | [数字默认与保留边界](TEXT-FORMATTING.md) | `internal/textstyle/`、`prefs.go` |
| 文字整理 | [提示词、非拦截返回与验收边界](../../internal/cleanup/README.md) | `internal/cleanup/cleanup.go` |
| 客户端更新 | [界面状态与原生验收](UPDATE-UI.md)、[更新协议与发布](UPDATES.md) | `updates.go`、`internal/updateclient/`、`internal/distribution/`、`window.go` |
| 构建与分发 | [打包](PACKAGING.md)、[分发](DISTRIBUTION.md) | `scripts/` |

## Mock 预览

[统一导航](http://127.0.0.1:38127/) · [模块入口与启动约定](MOCK-NAVIGATION.md) · [旧稿归档](http://127.0.0.1:38127/archive/index.html)

按需查看[组件画廊](MOCK-NAVIGATION.md#组件画廊2026-10-20)、[独立交互 V2](MOCK-NAVIGATION.md#独立交互-v22026-10-20)与[更新入口方案](UPDATE-UI.md#更新入口-mock2026-10-19)。这些章节描述本地 HTML mock；原生行为与验收仍以对应模块规格为准。

## 维护约定

修改行为时更新对应模块规格，根 README 只保留用户可见摘要与链接；AGENTS.md 使用短引用，不复制模块细节。规格描述当前源码，不能据此认定已发布版本包含这些功能。

`spec/` 保存本机前端、设计与验收规格（入口 `spec/README.md`），不入 Git；克隆仓库后不要求该目录存在，公开模块规格仍由本索引维护。

`docs/sprint/`、拼写兼容路径 `doocs/sprint/` 和 `artifacts/` 都只留在本地。验收步骤不等于已通过验收：实际执行时记录构建提交、环境、输入、预期、实测和证据路径，未执行项标记为未验证。不要提交私人转录、截图或凭证。

- 分段更新：30 秒目标 / 32 秒硬上限与短尾提示防护见 [分段规格](ASR-SEGMENTATION.md)。

- [自动发送与标准选择器](AUTO-SEND.md)：全局默认、按应用覆盖、延迟取消与编辑、五行深色浮层、远程下拉和原生验收。

文字整理顶部总开关、规则编辑持久化、基础格式联动与组合云服务边界：[原生文字整理总开关](../../internal/cleanup/README.md#原生文字整理总开关2026-10-18)。

- [语音质量基线与迭代](VOICE-QUALITY.md)：冻结真实录音、识别与整理分层对照、术语协议、全零恢复边界和逐条验收。

语音质量迭代的弱声增益、确认规则云端作用域、编号换行、逐条复核和时延发布门槛见[语音质量规格](VOICE-QUALITY.md#第二轮质量与时延门槛)。
