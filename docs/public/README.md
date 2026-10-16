# 模块文档索引

用户入口见 [中文 README](../../README.zh-CN.md) 与 [English README](../../README.md)。本目录保存可公开的行为规格；实现细节放在对应模块旁，按需继续阅读。

| 模块 | 规格与验收 | 实现入口 |
| --- | --- | --- |
| 首页与历史 | [布局、功能与原生验收](HOME-HISTORY.md) | `home.go`、`journal.go`、`history.go`、`history_stats.go`、`app_identity.go`、`prefs.go` |
| 权限授权 | [共享拖拽引导与原生验收](PERMISSIONS.md) | `permission_drag.go`、`internal/macos/settings_window_darwin.go` |
| 开始使用引导 | [窗口布局与原生验收](ONBOARDING.md) | `onboarding.go` |
| DJI 麦克风按钮 | [设备匹配与原生验收](DJI-BUTTON.md) | `internal/macos/dji_darwin.go` |
| 听写胶囊 | [显示规则与原生验收](CAPSULE.md) | `overlay.go`、`overlay_v2.go` |
| 远程桌面输入 | [匹配规则、剪贴板与原生验收](REMOTE-INPUT.md) | `prefs_remote.go`、`internal/macos/remote_darwin.go`、`internal/session/controller.go` |
| 本地语音模型 | [按需加载、句尾预览与验收](LOCAL-ASR.md) | `internal/localasr/`、`internal/session/controller.go` |
| 云端语音识别 | [豆包 DDC 默认与验收](../../internal/cloudasr/README.md) | `internal/cloudasr/volc.go` |
| 配置持久化 | [远程规则结构及兼容约定](../../internal/config/README.md) | `internal/config/config.go`、`remote.go` |
| 词典与纠正学习 | [V2 页面、热词与纠正验收](DICTIONARY.md) | `dictionary.go`、`internal/dict/`、`internal/cloudasr/volc.go`、`internal/session/controller.go` |
| 基础格式与数字 | [数字默认与保留边界](TEXT-FORMATTING.md) | `internal/textstyle/`、`prefs.go` |
| 文字整理 | [提示词、非拦截返回与验收边界](../../internal/cleanup/README.md) | `internal/cleanup/cleanup.go` |
| 客户端更新 | [界面状态与原生验收](UPDATE-UI.md)、[更新协议与发布](UPDATES.md) | `updates.go`、`window.go` |
| 构建与分发 | [打包](PACKAGING.md)、[分发](DISTRIBUTION.md) | `scripts/` |

## 维护约定

修改行为时更新对应模块规格，根 README 只保留用户可见摘要与链接；AGENTS.md 使用短引用，不复制模块细节。规格描述当前源码，不能据此认定已发布版本包含这些功能。

`docs/sprint/`、拼写兼容路径 `doocs/sprint/` 和 `artifacts/` 都只留在本地。验收步骤不等于已通过验收：实际执行时记录构建提交、环境、输入、预期、实测和证据路径，未执行项标记为未验证。不要提交私人转录、截图或凭证。

- 分段更新：30 秒目标 / 32 秒硬上限与短尾提示防护见 [分段规格](ASR-SEGMENTATION.md)。
