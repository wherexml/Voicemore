# 项目约定

本地开发前，如存在 [本机开发约定](docs/macos-local-signing.md)，须先读取并遵守。该文档仅保存在本机，不纳入版本控制。

正式 GitHub Release 默认且必须完成 Developer ID 签名、Apple 公证、票据校验及最终安装包自动校验；普通版本不强制重复跨机手工验收，按改动范围增补原生回归。使用 `scripts/publish-release.sh`，遵循[正式分发流程](docs/public/DISTRIBUTION.md)。不得将开发签名或公证未通过的包发布为正式版／latest；日常安装与正式发布共用固定 Developer ID 身份；开发调试必须使用独立名称、Bundle ID、数据及无更新源的构建，见[统一签名策略](docs/public/DISTRIBUTION.md#unified-signing-identity-2026-10-17)。

## 新页面开发流程

- 设计产品页面时，禁止使用解释性文案；产品 UI 中的统计类需求须使用数值、图表等形式呈现，禁止用描述性文案代替统计展示。
- 所有新页面均须先制作 mock，先把视觉效果、布局和关键交互打磨好，再以 mock 为依据进行正式实现和后续优化。
- 开发顺序统一为：制作 mock → 打磨页面效果 → 正式实现 → 持续优化。

## 模块规格

- 设置窗口默认居中 820×680 DIP，全屏按钮灰色禁用，不恢复旧最大化状态；边缘缩放和原生复验见[设置窗口规格](docs/public/HOME-HISTORY.md#设置窗口尺寸与窗口按钮)。

- 本地 ASR 自定义与调参先查 [Qwen3-ASR 能力、参数及接入边界](docs/public/QWEN3-ASR-CUSTOMIZATION.md)。

- 修改相关行为时同步维护[文档索引](docs/public/README.md)引用的模块规格与验收步骤。
- Sprint 草稿与本地验收证据不入库，目录约定见[文档索引](docs/public/README.md#维护约定)。

- 文字整理的错误分类、延迟排查和 HTTP 验收见 [整理模块规格](internal/cleanup/README.md)；保留响应体读取阶段的超时与取消原因。
- DJI 接收器按钮与键盘触发的边界、原生验收见[触发键规格](docs/public/DJI-BUTTON.md)。

- V2 原生历史统计、系统图标与筛选：[首页与历史规格](docs/public/HOME-HISTORY.md#v2-原生历史统计v3-沿用的数据口径)；词典页标题、布局与交互：[词典规格](docs/public/DICTIONARY.md#v2-原生词典页面)。

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

- DeepSeek 整理默认关闭思考；无编号单段保持单段、明确编号逐项换行、含糊字串原位保留，协议与真实模型复验见[整理模块规格](internal/cleanup/README.md#deepseek-默认关闭思考与分组保留)。

- 陌生实义字串内部重复也原位保留；同批交替复测记录连接、首字节和共享负载，未达质量/延迟门槛不更新正式版，见[含糊叠字与配对延迟复验](internal/cleanup/README.md#含糊叠字与配对延迟复验)。

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

- 设置损坏时禁止隐私回退；历史清空覆盖恢复副本与孤立录音，旧 Key 迁移须净化备份，见[配置恢复与隐私验收](internal/config/README.md#状态持久化与恢复)。

- 本地前端与设计规格按 `spec/README.md` 按需阅读（仅本机存在，不入 Git）；公开模块入口仍见[文档索引](docs/public/README.md)。

- 听写错误提示统一 3 秒后退场，保留正文与历史，旧计时器不得影响下一轮；见[胶囊错误提示规格](docs/public/CAPSULE.md#错误提示自动退出2026-10-19)。

- Capsule errors use concise localized summaries; retain diagnostic codes and task IDs only in diagnostics. See docs/public/CAPSULE.md.

- 真实语音基线、词表摘要、全零录音与学习质量的隔离验收见[语音质量规格](docs/public/VOICE-QUALITY.md)；先冻结基线，留出样本不参与调参，服务端配套更新后才能发布客户端。

- 低音量识别增益、已确认规则的云端快照、编号真实换行及逐条质量/延迟发布门槛见[语音质量规格](docs/public/VOICE-QUALITY.md#第二轮质量与时延门槛)。未过门槛不得发布正式版。

- 整理回归先标明应用实际路由；独立本地回放不能代表组合云端听写，提示词实验不等于部署。路由字段与分阶段时延见[整理验收归属](internal/cleanup/README.md#实际路由与实验归属)。

- 更新就绪按钮接收重启请求，忙碌时显示等待并在空闲后提交；隔离原生夹具及跨机验收边界见[更新重启规格](docs/public/UPDATE-UI.md#重启请求验收2026-10-21)。

- 今日图表左对齐、柱顶留白与即时 hover/键盘提示见[首页图表规格](docs/public/HOME-HISTORY.md#今日图表对齐留白与即时提示2026-10-21)。

- 首页缺失权限入口使用盾牌图标与“权限不足”，点击及本地化见[首页权限提示](docs/public/HOME-HISTORY.md#首页权限提示2026-10-21)。

- 组件画廊、独立交互 V2 与更新入口预览见 [Mock 导航](docs/public/MOCK-NAVIGATION.md)；Mock 方案及历史浏览器结果不得代替当前原生行为或本轮验收。

- 官方翻译的任务语言、回执与服务先行部署见[翻译规格](docs/public/TRANSLATION.md#翻译独立发布范围2026-10-23)；启动前错误关闭见[胶囊规格](docs/public/CAPSULE.md#启动前错误关闭2026-10-22)。

- 首页立即听写与启动交接保护见[首页规格](docs/public/HOME-HISTORY.md#首页听写立即开始2026-10-23)。

- 词典手动替换、原子编辑、被动建议过滤与隔离原生验收见[词典规格](docs/public/DICTIONARY.md#手动配置编辑与被动建议过滤2026-10-23)。

- 词典去说明、重复词与旧片段误过滤回归、前后对比的实际改善和未解决边界见[词典复验](docs/public/DICTIONARY.md#去说明与前后对比复验2026-10-24)。
