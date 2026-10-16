# 项目约定

本地开发前，如存在 [本机开发约定](docs/macos-local-signing.md)，须先读取并遵守。该文档仅保存在本机，不纳入版本控制。

正式 GitHub Release 默认且必须完成 Developer ID 签名、Apple 公证、票据校验及最终安装包自动校验；普通版本不强制重复跨机手工验收，按改动范围增补原生回归。使用 `scripts/publish-release.sh`，遵循[正式分发流程](docs/public/DISTRIBUTION.md)。不得将开发签名或公证未通过的包发布为正式版／latest；开发预览仅允许显式 `--development-preview`。

## 模块规格

- 修改相关行为时同步维护[文档索引](docs/public/README.md)引用的模块规格与验收步骤。
- Sprint 草稿与本地验收证据不入库，目录约定见[文档索引](docs/public/README.md#维护约定)。

- 文字整理的错误分类、延迟排查和 HTTP 验收见 [整理模块规格](internal/cleanup/README.md)；保留响应体读取阶段的超时与取消原因。
- DJI 接收器按钮与键盘触发的边界、原生验收见[触发键规格](docs/public/DJI-BUTTON.md)。

- V2 原生历史统计、系统图标与筛选：[首页与历史规格](docs/public/HOME-HISTORY.md#v2-原生历史统计)；词典页标题、布局与交互：[词典规格](docs/public/DICTIONARY.md#v2-原生词典页面)。

- 数字默认格式、保护范围与真实音频验收：[基础格式规格](docs/public/TEXT-FORMATTING.md)。

- 本地 ASR 的停顿切段、短尾提示防护与协议验收见 [分段规格](docs/public/ASR-SEGMENTATION.md)。

- V3 首页、历史统一记录卡片、真实声纹与 CSV 导出：[首页与历史规格](docs/public/HOME-HISTORY.md#v3-首页与历史)。

- 豆包默认 DDC 与整理保留边界：[云端 ASR 规格](internal/cloudasr/README.md)。

- 窗口背景拖动、底部拖动留白与细滚动条：[窗口交互规格](docs/public/HOME-HISTORY.md#窗口拖动与滚动条)。

- 历史应用名称与 Bundle ID 在成功写入时同步更新，跨应用验收见[应用身份与图标](docs/public/HOME-HISTORY.md#应用身份与图标)。

- 整理采用有效模型正文，不加数字/英文或语义拦截，不维护口语白名单；数量与原意由提示词约束；真实记录须逐条检查术语、限定、语气及歧义保留，并单列流畅度，见 [整理模块规格](internal/cleanup/README.md#校验复验)。

- DMG 图标中心对齐、名称间距、固定英文单行提示及打包验收：[安装窗口规格](docs/public/DMG-INSTALLER.md)。

- 整理须主动去口吃、修碎句、合冗余，逐条复核忠实性与流畅度；“一次/一遍/一回”自然写法见[基础格式规格](docs/public/TEXT-FORMATTING.md)。不增加输出拦截。

- 更新按钮的圆弧箭头、侧栏线宽与原生验收见[更新界面规格](docs/public/UPDATE-UI.md#refresh-icon)。

- 自动发送、全局继承、应用身份、标准下拉与原生验收见 [自动发送规格](docs/public/AUTO-SEND.md)；发送键触发不等于消息送达。

- 普通单数对象及次数使用自然中文，整理与前后基础格式共同遵守[数字规格](docs/public/TEXT-FORMATTING.md)；保留数量限定、计量和编号，不全局反向替换阿拉伯数字。
