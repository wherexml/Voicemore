# 开发入口

遵循 [AGENTS.md](AGENTS.md)，本地 macOS 构建前读取其中链接的本机签名约定。

- 模块规格与验收入口：[文档索引](docs/public/README.md)。
- 文字整理、超时分类与延迟诊断：[整理模块规格](internal/cleanup/README.md)。
- 先列故障场景，再做隔离实现；优先真实链路验收，不在代码实现后补写单元测试。验收证据放在 Git 忽略的 `artifacts/`。
- 键盘与 DJI 接收器按钮触发：[全局触发键规格](docs/public/DJI-BUTTON.md)。

- V2 原生历史统计、系统图标与筛选：[首页与历史规格](docs/public/HOME-HISTORY.md#v2-原生历史统计)；词典页标题、布局与交互：[词典规格](docs/public/DICTIONARY.md#v2-原生词典页面)。

- 数字默认格式、保护范围与真实音频验收：[基础格式规格](docs/public/TEXT-FORMATTING.md)。

- 本地 ASR 的停顿切段、短尾提示防护与协议验收见 [分段规格](docs/public/ASR-SEGMENTATION.md)。

- V3 首页、历史统一记录卡片、真实声纹与 CSV 导出：[首页与历史规格](docs/public/HOME-HISTORY.md#v3-首页与历史)。

- 豆包默认 DDC 与整理保留边界：[云端 ASR 规格](internal/cloudasr/README.md)。

- 窗口背景拖动、底部拖动留白与细滚动条：[窗口交互规格](docs/public/HOME-HISTORY.md#窗口拖动与滚动条)。
