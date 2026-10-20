# 开发入口

遵循 [AGENTS.md](AGENTS.md)，本地 macOS 构建前读取其中链接的本机签名约定。

- 模块规格与验收入口：[文档索引](docs/public/README.md)。
- 设置默认尺寸、全屏按钮禁用及原生复验：[设置窗口规格](docs/public/HOME-HISTORY.md#设置窗口尺寸与窗口按钮)。
- 本地 ASR 调参与扩展：[Qwen3-ASR 自定义能力](docs/public/QWEN3-ASR-CUSTOMIZATION.md)。
- 文字整理、超时分类与延迟诊断：[整理模块规格](internal/cleanup/README.md)。
- 先列故障场景，再做隔离实现；优先真实链路验收，不在代码实现后补写单元测试。验收证据放在 Git 忽略的 `artifacts/`。
- 键盘与 DJI 接收器按钮触发：[全局触发键规格](docs/public/DJI-BUTTON.md)。

- V2 原生历史统计、系统图标与筛选：[首页与历史规格](docs/public/HOME-HISTORY.md#v2-原生历史统计)；词典页标题、布局与交互：[词典规格](docs/public/DICTIONARY.md#v2-原生词典页面)。

- 数字默认格式、保护范围与真实音频验收：[基础格式规格](docs/public/TEXT-FORMATTING.md)。

- 本地 ASR 的停顿切段、短尾提示防护与协议验收见 [分段规格](docs/public/ASR-SEGMENTATION.md)。

- V3 首页、历史统一记录卡片、真实声纹与 CSV 导出：[首页与历史规格](docs/public/HOME-HISTORY.md#v3-首页与历史)。

- 复制与删除共用 SF Symbols 原版，资源来源、主题着色及原生复验见[系统操作图标规格](docs/public/HOME-HISTORY.md#复制与删除系统图标)。

- 豆包默认 DDC 与整理保留边界：[云端 ASR 规格](internal/cloudasr/README.md)。

- 窗口背景拖动、底部拖动留白、细滚动条及首页操作按钮的独立滚动间距：[窗口交互规格](docs/public/HOME-HISTORY.md#窗口拖动与滚动条)。

- 历史应用名称与 Bundle ID 在成功写入时同步更新，跨应用验收见[应用身份与图标](docs/public/HOME-HISTORY.md#应用身份与图标)。

- 整理采用有效模型正文，不加数字/英文或语义拦截，不维护口语白名单；数量与原意由提示词约束；真实记录须逐条检查术语、限定、语气及歧义保留，并单列流畅度，见 [整理模块规格](internal/cleanup/README.md#校验复验)。

- DMG 图标中心对齐、名称间距、固定英文单行提示及打包验收：[安装窗口规格](docs/public/DMG-INSTALLER.md)。

- 整理须主动去口吃、修碎句、合冗余，逐条复核忠实性与流畅度；“一次/一遍/一回”自然写法见[基础格式规格](docs/public/TEXT-FORMATTING.md)。不增加输出拦截。

- V6 固定深色五步引导、双卡片模型选择与区域内下载、原生桥与防闪烁验收：[开始使用规格](docs/public/ONBOARDING.md)。

- 更新按钮使用 16 DIP 的 macOS SF Symbols `arrow.clockwise`，资源来源与原生验收见[更新界面规格](docs/public/UPDATE-UI.md#refresh-icon)。

- 自动发送、全局继承、应用身份、标准下拉与原生验收见 [自动发送规格](docs/public/AUTO-SEND.md)；发送键触发不等于消息送达。

- 普通单数对象及次数使用自然中文，整理与前后基础格式共同遵守[数字规格](docs/public/TEXT-FORMATTING.md)；保留数量限定、计量和编号，不全局反向替换阿拉伯数字。

- DeepSeek 整理默认关闭思考；长文按对象分组、含糊字串原位保留，协议与真实模型复验见[整理模块规格](internal/cleanup/README.md#deepseek-默认关闭思考与分组保留)。

- 延迟发送默认 2 秒、取消与编辑、固定深色五行浮层及远程统一下拉，见 [自动发送规格](docs/public/AUTO-SEND.md#延迟取消与编辑)。

- 听写触发键与麦克风使用共享标准选择器，UID 保存、键盘焦点恢复及原生复验见[标准选择器规格](docs/public/AUTO-SEND.md#听写原生标准选择器)。

- 浮层视觉验收须对照 HTML：透明窗口背景不能被 SetTheme 覆盖；24 DIP 行高、阴影与矢量叉号见 [自动发送规格](docs/public/AUTO-SEND.md#浮层视觉还原约束)。

- Send-preview dismissal: outside click, Escape and the cross close the panel without deleting history; see docs/public/AUTO-SEND.md.

- 自定义整理服务支持服务端默认、DeepSeek/oMLX关闭协议及非敏感JSON参数；保存、真实测试与协议验收见[整理模块规格](internal/cleanup/README.md#自定义服务的思考模式)。

- 默认只输入与自动发送见 [自动发送规格](docs/public/AUTO-SEND.md)；V13 移除最近文字入口，连续胶囊先在 HTML 验收。

- 听写至发送倒计时复用同一原生胶囊，底部锚点及取消复验见[自动发送规格](docs/public/AUTO-SEND.md#v14-原生连续胶囊2026-10-16覆盖旧浮层流程)。

- AirPods 不强制修改采样率；提前静音与独立切换保护须检查连续快照和非零 PCM；播放格式、静音和音量恢复见 [蓝牙音频规格](docs/public/BLUETOOTH-AUDIO.md)。

- 本地识别语言（缺省中文、自动、英语，仅本地 worker）与私密应用（任一端命中即不落盘、不学习）：[识别语言](docs/public/LOCAL-ASR.md#识别语言)、[私密应用](docs/public/PRIVATE-APPS.md)。

- Fn 等修饰键优先使用只读 HID 监听，保留会话层回退与中断恢复；原生回归及物理按键验收边界见[触发键规格](docs/public/HOTKEY.md)。

- Mock 统一入口为 `http://127.0.0.1:38127/`，当前模块、旧稿归档与启动约定见 [Mock 导航](docs/public/MOCK-NAVIGATION.md)。新增版本须同步导航，旧稿归档，不将 mock 入库；全部预览页复用共享“返回导航”入口。

- 胶囊原生与 mock 的连续伸缩、文字和图标过渡、倒计时及减少动态效果验收见[胶囊动效规格](docs/public/CAPSULE.md#原生连续状态动效2026-10-17)。

- 首页原生今日 24 小时柱状图与 mock 对齐；默认今日、小时筛选、未来时段、键盘与隔离空态验收见[首页今日柱状图](docs/public/HOME-HISTORY.md#首页今日-24-小时柱状图2026-10-17)。

- 首页触发键/方式、模型、麦克风和日期范围复用共享选择器；独立选中勾、键盘操作及原生复验见[首页标准选择器](docs/public/HOME-HISTORY.md#首页标准选择器2026-10-17)。

- 本地前端与设计规格按 `spec/README.md` 按需阅读（仅本机存在，不入 Git）；公开模块入口仍见[文档索引](docs/public/README.md)。

- 听写错误提示统一 3 秒后退场，保留正文与历史，旧计时器不得影响下一轮；见[胶囊错误提示规格](docs/public/CAPSULE.md#错误提示自动退出2026-10-19)。

- Capsule errors use concise localized summaries; retain diagnostic codes and task IDs only in diagnostics. See docs/public/CAPSULE.md.

- 真实语音基线、词表摘要、全零录音与学习质量的隔离验收见[语音质量规格](docs/public/VOICE-QUALITY.md)；先冻结基线，留出样本不参与调参，服务端配套更新后才能发布客户端。
