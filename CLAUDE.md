# 开发入口

遵循 [AGENTS.md](AGENTS.md)，本地 macOS 构建前读取其中链接的本机签名约定。

- 模块规格与验收入口：[文档索引](docs/public/README.md)。
- 文字整理、超时分类与延迟诊断：[整理模块规格](internal/cleanup/README.md)。
- 先列故障场景，再做隔离实现；优先真实链路验收，不在代码实现后补写单元测试。验收证据放在 Git 忽略的 `artifacts/`。
- 键盘与 DJI 接收器按钮触发：[全局触发键规格](docs/public/DJI-BUTTON.md)。

- 应用身份、系统图标、历史统计与 HTML demo 验证边界：[首页与历史规格](docs/public/HOME-HISTORY.md#应用身份与图标html-demo-验证)。

- 数字默认格式、保护范围与真实音频验收：[基础格式规格](docs/public/TEXT-FORMATTING.md)。

- 本地 ASR 的停顿切段、短尾提示防护与协议验收见 [分段规格](docs/public/ASR-SEGMENTATION.md)。
