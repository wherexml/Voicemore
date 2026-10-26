# Mock 导航与维护

统一入口：http://127.0.0.1:38127/ 。本地文件：`mock/index.html`。

在项目根目录启动（已有服务时直接复用）：

```sh
python3 -m http.server 38127 --bind 127.0.0.1 --directory mock
```

| 模块 | 当前入口（相对 mock/） |
| --- | --- |
| 首页 · 中文 | `home-v3.html` |
| 首页 · English | `home-v3-en.html` |
| 历史记录 | `history-v3.html?lang=hans#history` |
| 词典 | `dictionary-rules.html`（手动规则与编辑）；完整旧页面 `auto-send.html#dictionary` |
| 通用 | `auto-send.html#settings/general` |
| 听写 · 设备按键 · 翻译 | `auto-send.html#settings/dictation` |
| 文字与粘贴 · 文字整理 · 自动发送 · 远程桌面 | `auto-send.html#settings/output` |
| 模型 | `auto-send.html#settings/models` |
| 权限 | `auto-send.html#settings/perms` |
| 识别语言 · 私密应用 | `language-privacy.html` |
| 开始使用 | `onboarding-v6.html` |
| 模型选择 | `onboarding-model-v2.html` |
| 宣传动画 | `promo-demo/index.html`（听写 → 整理 → 输入，独立网页演示） |
| 官网 · 最终版 | `website-v3/index.html`（V3 深夜声场，简繁英；`website/` 同指此版） |
| 官网旧稿 | `archive/website-2026-10-27/`（蓝色 approved、文案 V2、Q 及候选主视觉，2026-10-27 归档） |
| 连续胶囊状态 | `capsule-states-v2.html?tidy=1&autoSend=1` |
| 独立交互 V2 | `interaction-v2/index.html` |
| 组件画廊 | `component-gallery/index.html`（17 类；单组件 `?solo=1#capsule`） |
| 手动检查更新 | `auto-send.html?update=current#settings/general` |
| 发现新版本 | `auto-send.html?update=available` |

旧稿导航：`mock/archive/index.html`；本次归档：`mock/archive/2026-10-17/`。早期备份留在 `mock/archive/`，隔夜快照留在 `mock/archive/overnight-20261016/`，均由归档导航收录。独立设备按键稿已归档，当前入口为听写设置。

`mock/` 仍仅保存在本机、不入 Git。共享资源保留原位，归档页通过相对目录链接复用；移动或复制归档时须一起保留这些资源。新增版本时同步维护统一导航和本表，再归档被替代稿。导航中的“当前”表示 mock 选择，不代表原生或发布验收通过。

验收：打开统一入口，逐项点击并确认目标模块；检查历史直接进入历史、词典直接进入词典、设置各分类正确；打开归档与 V1/V2 对照，检查资源加载；返回导航。证据保存在 `artifacts/mock-navigation/`。

## 页面返回入口

全部模块与归档页面共享 `mock/components/mock-navigation.js`，左下角固定“返回导航”，直接返回统一入口，不依赖浏览器历史。组件使用独立样式，页面重绘时保留；V1/V2 对照的内嵌页面隐藏重复入口，仅外层显示。入口支持 Tab 聚焦与 Enter 跳转，打印时隐藏。新增 HTML 须使用相对路径加载该组件。

复验：从导航打开每个模块，点击“返回导航”，确认回到 `mock/index.html`；设置切换后入口仍可用；归档及深层隔夜方案返回同一入口。证据：`artifacts/mock-navigation/return-navigation/`。

胶囊入口已同步原生连续动效基线：480 ms 伸缩、状态文字错峰进出与发送控件展开。预选整理和自动发送便于完整重播；应用实际验收见[胶囊规格](./CAPSULE.md)。

## 听写翻译布局（2026-10-17）

听写设置的“翻译快捷键”和“目标语言”合并在顶部卡片，紧接“触发键”；移除用于 mock 演示的翻译示例和体验按钮。触发键与翻译快捷键点击后直接录入，松键保存，支持单独修饰键与组合键；Escape、Tab、点击其他位置或窗口失焦取消。两项快捷键相同时提示占用且保留原值；翻译快捷键可清除。首页触发键入口共用录入控件。复验组合键、左右修饰键、取消、重复冲突、清除和刷新保留；脚本、结果与截图：`artifacts/shortcut-recorder/`。这是浏览器 mock，Fn 等系统保留键能否交付及全局注册须另做原生验收。

## 润色规则 Mock（2026-10-17）

“文字与粘贴”顶部合并为一个“文字整理”分组，顶部为从“听写”移入的“默认整理文字”开关，其后为基础格式五个开关和可展开编辑的润色规则。默认规则删除与格式开关重复的口头词要求，保留语气、术语、碎句、口吃、重复与自我修正。内容和展开状态在刷新、切换分类后保留；上一版未修改的默认规则自动更新，自定义内容保持原样。仅为交互预览，尚未接入原生整理或模型调用。

验收：编辑含换行、中文及 HTML 字符的规则，收起并展开，切换分类返回，刷新后确认内容和状态保留；用 Tab、Enter、Space 检查展开按钮，复核浅色和深色显示。同时检查总开关开启时五个格式开关独立保存、总开关关闭时下方控件全部禁用、重新开启恢复原值，以及旧默认规则迁移与自定义内容保留。可重跑脚本、结果和截图：`artifacts/text-cleanup-group/`。

“默认整理文字”已从听写设置移至文字整理分组顶部，沿用同一开关状态；首页整理入口同步指向“文字与粘贴”。“默认整理文字”作为整组总开关：关闭时，基础格式五个开关、润色规则展开按钮及编辑框统一置灰禁用，保留既有值；再次开启恢复可操作。自动发送保持独立。移动复验、截图与脚本：`artifacts/text-cleanup-toggle/`。

总开关联动验收：关闭后检查所有下属控件不可点击、不可编辑且 Tab 跳过；刷新后仍禁用；开启后保留并恢复原配置。证据及可重跑脚本：`artifacts/text-cleanup-disabled/`。

## 官网模型与服务（2026-10-17）

统一导航新增官网入口，`mock/website` 软链接至本机 `website/approved`，沿用当前蓝色方案。`#models` 三张卡分别为本机识别、使用你自己的模型、Voicemore Cloud；官方模型服务展示开箱即用、订阅或按量付费，并标记即将推出。简繁英同步，复用共享返回入口。此处仅为官网 mock，官方服务尚未上线，不提供购买入口。

验收：从导航进入官网，点击卡片链接并切换三种语言，核对锚点保留；检查浅深色、窄屏与键盘，再返回统一导航。可重复脚本、截图和结果：`artifacts/website-model-choices/`。

## 官网文案 V2（2026-10-17）

`website/v2` 是蓝色官网的独立文案版本，`mock/website-v2` 软链接至该目录。原版 `website/approved` 保留，统一导航可分别打开两个版本。文案包含简繁英标题、功能、模型服务、隐私、FAQ、下载与元数据；沿用原演示与产品边界，官方云服务标记即将推出，展示订阅或按量付费。主要修改对照见 `website/v2/COPY-V2.md`。本地浏览器验收脚本与证据保存在 `artifacts/website-copy-v2/`。

官网 V2 的“更多能力”首卡改为远程桌面：为远程桌面优化，远程也能像本地一样使用。三语言同步，配套远程窗口出字预览；离屏暂停、减少动态效果展示完整文字。仅为预设 browser mock，不建立远端连接；真实远程验收仍见[远程输入规格](REMOTE-INPUT.md)。复验截图与可重跑脚本：`artifacts/website-remote-card/`。

润色规则输入框细节（2026-10-18）：固定 176 px 高度，13 px 字号、22 px 行高、10 px 圆角，长文内部滚动且禁用手工拖拽扩高；原生对应规格见[整理模块](../../internal/cleanup/README.md#润色规则编辑区细节2026-10-18)。

## 首次 Fn 提醒（2026-10-19）

`onboarding-v6.html` 首次进入 Fn 步骤时，在快捷键卡片下方显示一行弱提示：“Fn 组合键不受影响，退出后恢复原功能。”不弹窗、不遮罩、不额外确认；点击“继续”后保存已提醒状态，再返回不重复显示。预览重播入口：`onboarding-v6.html#fn-notice`（刷新可重播）；工具栏“重置”可复验完整首次流程。仅为视觉与交互 mock，不操作系统快捷键。Chrome 验收步骤、结果及截图：`artifacts/fn-notice-mock-20261019/`。

## 更新入口 Mock（2026-10-19）

共享实现 `mock/components/update-entry.js` 用于 `auto-send.html` 与 `history-v3.html`。入口显示条件、固定状态参数、手动检查与复验步骤统一见[更新入口 Mock 规格](UPDATE-UI.md#更新入口-mock2026-10-19)。这是本地预览方案，原生仍保留常驻更新入口。

## 独立交互 V2（2026-10-20）

独立入口：`interaction-v2/index.html`，保留原 `capsule-states-v2.html`。用于评审连续胶囊的状态接续、打断重来、两秒发送取消、耳机断开模拟，以及真实示例音频的波形播放和拖动定位。页面内听写、整理、插入和发送均为模拟，不调用系统录音、模型或目标应用；音频是本机合成的示例语音，波形由该 WAV 解码取峰值生成。慢速预览将整理等待和倒计时延长，正常倒计时保持两秒。使用共享返回导航，支持深浅色和减少动态效果。

浏览器验收脚本、结果与截图：`artifacts/interaction-v2-20261020/`；重跑 `ego-browser nodejs < artifacts/interaction-v2-20261020/verify.mjs`，先替换脚本中的任务空间 ID。本次只交付独立 mock，不代表原生实现或发布。

## 组件画廊（2026-10-20）

入口 `component-gallery/index.html`：独立 HTML mock，仅保留组件目录、标题与交互画布，无首页介绍、使用说明或代码区。胶囊、录音波形、标准选择器、开关、今日柱状图分开预览；`?solo=1#capsule` 仅显示单组件。共享返回导航，原有交互 V2 保留。

参考 Onevra 的 `source/examples/gallery/web/main.go` 单页预览和 `bridge_js.go` hash 切换结构：https://github.com/oneo-me/onevra/tree/main/source/examples/gallery/web 。Onevra 原站运行 MyGo WASM；本稿是 HTML 视觉与交互预览，未迁移原生 Go 组件、未调用真实听写或发送。音频复用交互 V2 的合成 WAV；设备选项与图表为固定演示数据。切换组件会中止胶囊计时和音频播放。

复验：打开入口 → 开始听写 → 结束 → 打断并重来 → 切换波形并播放 → 进度 End → 选择器选择豆包 → 自动发送开关 → 点击 14 时柱（160）→ 今日重置 → 切换深浅色 → 单组件展开。Chrome 本轮上述交互通过；结果与截图保存在 `artifacts/component-gallery-20261020/`，不入库。

2026-10-20 增量：选择器接入共享 `VMSelect`，保留分组、键盘选择、Escape 焦点恢复及切页关闭；深浅色按钮移至组件标题区，窄屏仍可访问。本轮 Ego 浏览器 13 项交互通过，脚本与原始结果位于 `artifacts/component-gallery-20261020/followup/verify.mjs`、`results.json`，另有选择器、深色和 390px 截图。重跑前将脚本首行任务空间与页面 ID 换为当前自有空间；这仍是 HTML mock 验收。

### 组件画廊扩充

现有 5 类之外新增 12 类：按钮与反馈 `#buttons`、文本输入 `#inputs`、分段选择 `#segments`、滑块 `#sliders`、快捷键录入 `#shortcuts`、日期选择 `#calendar`、词典编辑 `#dictionary`、应用筛选 `#tags`、历史卡片 `#history`、模型卡片 `#models`、更新进度 `#updates`、确认弹窗 `#dialogs`。共 17 类，复用现有画布和路由，新增示例独立维护在 `mock/component-gallery/catalog.js` 与 `catalog.css`。

示例值只存页面内存；保存、选择、删除和下载状态不修改应用配置或历史，不下载模型/更新。复制按钮通过浏览器剪贴板复制公开示例，失败有反馈。快捷键录入支持取消、失焦回退及重复检查，仅浏览器范围；日期按实际月份生成并支持翻月；切页停止下载计时与快捷键录入并关闭弹窗。确认弹窗支持 Escape、外部点击关闭和原触发器焦点恢复。沿用简洁标签，不增加解释区。

重复验收脚本：`artifacts/component-gallery-20261020/expanded/verify.mjs`（执行前替换任务空间/页面 ID）；结果与浅深色、日期及窄屏截图保存在同目录。此目录及 mock 不入 Git，不代表原生组件验收。

本轮 Ego 实测 44 项通过（`expanded/results.json`）：17 类路由、输入/筛选、快捷键冲突与取消、词典增删去重、历史编辑恢复、下载取消/失败/完成、弹窗及 390px 五类布局。后续定向复验确认桌面目录独立滚动、标题仍可见（top=54）、页面无纵向溢出，以及失败重试完成后按钮恢复“下载”；最终截图 `expanded/final.png`。

### Fedora Claude 细节优化（2026-10-20）

本轮由 Fedora Claude Opus 5.5 在共享目录修改画廊三文件，Mac 协调端复验。统一输入框、按钮、危险动作、提示色及焦点样式；窄屏模型卡纵向排列；分段选择、应用标签和模型卡使用单 Tab 停靠与方向键；日历支持方向键、Home/End、PageUp/PageDown；词典增加大小写不敏感去重、空态和移除后焦点恢复；历史编辑可取消、拒绝空正文；下载示例增加失败续传、取消归零和完成状态；弹窗默认聚焦取消，仅确认显示完成反馈。共享选择器另有无设备禁用示例。

Mac 复核统一快捷键修饰键的序列，避免与默认快捷键重复但未识别；去掉未验证版本号。远端未执行构建、测试或类型检查，未运行 Fedora 浏览器；本机浏览器证据位于 `artifacts/component-gallery-20261020/remote-polish/`。原首轮回归在弹窗 `close` 事件前读取提示而失败，单独等待实际确认反馈后通过；复跑脚本已等待该异步事件，不把这次失败隐藏为旧结果。

最终本机验收：完整回归 44/44 与新增细节 13/13 通过，分别见 `remote-polish/results.json`、`details-results.json`；窄屏模型卡、深色与确认弹窗截图同目录。没有构建、安装、发布或原生 App 验收。

## 文档核对边界（2026-10-22）

以上 2026-10-19 / 20 的浏览器结果为既有本地验收记录。本次对照 `6976d6b` 整理文档，核对入口、共享源码、结果文件与忽略规则，没有重跑浏览器或原生验收；不得将旧结果标为本次 E2E 通过。克隆仓库后不要求存在 `mock/`、脚本或截图，须在保有这些本地文件的环境中复验。

## 首页立即听写（2026-10-23）

中英文首页保持点击即开始；`auto-send.html`、`history-v3.html` 清理遗留三秒文案和切换输入位置提示。行为及本轮浏览器复验见[首页听写立即开始](HOME-HISTORY.md#首页听写立即开始2026-10-23)。

## 当前改动同步（2026-10-23）

- 中英文首页同步 108 px 柱区、8 px 上间距、柱顶留白与 0 点左对齐；hover/键盘提示即时显示，Escape 或点击关闭。`?permission=missing` 展示盾牌权限入口并链接权限设置。
- 设置和历史共用稿错误提示 3 秒自动退场，启动前错误可关闭，整理失败记录在退场前保留；润色规则分组改为 14 px 内边距并去掉顶部分隔线。连续胶囊稿同步错误自动退场。
- `auto-send.html?lang=hans&update=ready` 展示更新就绪；`update=busy` 点击后等待，再通过“结束模拟任务”进入模拟重启。均无安装行为。
- `auto-send.html?lang=hans&service=official#settings/dictation` 是 Official 1.4 本地候选场景，使用既有目标语言与预设译文；可播放翻译成功、失败、错误退场及原任务取回，取回不自动输入。工具条标记 Mock，不接服务、不收费，不代表正式服务已部署。

本轮浏览器 18 项检查通过，截图和结果在 `artifacts/mock-sync-20261023/`；可重跑 `ego-browser nodejs < artifacts/mock-sync-20261023/verify-final.mjs`。首次脚本因胶囊按钮选择器匹配两项中断，修正为带 aria-label 的按钮后续跑通过；导航首次读到浏览器旧缓存，刷新后确认新增入口。保留首次脚本与续跑脚本。此证据仅覆盖 HTML Mock，不代表原生或生产验收。原本在其他任务中更新的首页立即听写逻辑保持不变。

## 宣传动画（2026-10-23）

`mock/promo-demo/index.html` 是独立、可重播的 Voicemore 功能宣传动画，沿用官网蓝色、现有图标与连续胶囊表现。分镜为 Fn 激活、听写、文字整理、输入结果；合成示例仅改变页面状态，不录音、不请求模型、不操作系统输入或发送。保留共享返回导航，不替换原官网和胶囊稿。

来源、分镜、失败场景与可调参数见本地 `mock/promo-demo/spec.md`；本轮验收状态和重复步骤见 `artifacts/promo-demo-20261023/acceptance.md`。页面和证据均留本机，不随 Git 发布；网页效果不代表原生业务验收。

宣传动画第二轮按当前胶囊稿复用五条波形与实心整理星芒：转录、写入显示短条，星芒仅用于整理；总时长 7.1 秒，5.1 秒完成写入。状态文字按变化更新，沿用胶囊文字交叠。定向验收与本机帧间隔采样见 `artifacts/promo-demo-20261023/revision-2/`，不代表跨设备帧率保证。

英文宣传动画入口为 `promo-demo/en.html`，标题为 “Think it. Say it. Done.”，与中文版共享 7.1 秒时间轴、图标和播放控制；合成示例、状态和按钮使用英语，长标签保留完整宽度。共享返回组件按文档语言显示英文。验收记录位于 `artifacts/promo-demo-20261023/english/`。

### 官网接入（2026-10-23）

以下 `website/...` 为统一 Mock 服务中的预览路由，对应本机 `website/approved/`，不指 Git 已跟踪的根 `website/index.html`。`approved/`、`v2/`、`v3/`、`q/` 及 `assets/hero-concepts/` 均按本地预览约定忽略；提交这些规格不等于发布网站代码或部署。

当前蓝色官网的 `website/index.html#demo` 与 `website/en.html#demo` 已接入中英文宣传动画，替换原听写展示。独立宣传页仍保留。官网本机代码位于 `website/approved/`，动画副本及嵌入样式在 `demos/promo/`；通过 `scripts/sync-promo.py` 从独立稿刷新，资源不依赖 Mock 路径。新增演示保留暂停/重播与 7.1 秒节奏；语言切换保持动画锚点。浏览器结果与重复步骤：`artifacts/website-promo-20261023/acceptance.md`。本轮只做本地接入，未部署，繁体动画未新增。

2026-10-23 官网动画融合：中英文 `#demo` 使用透明嵌入层与深蓝编辑器；识别正文紧贴状态放在同一胶囊内，以分隔线隔开，随文字测量展开，长句显示末尾，整理淡化、写入收拢。共享独立稿同步胶囊结构，仍保留独立浅色主题。对照 `capsule-states-v2.html` 与用户截图；新证据见 `artifacts/website-promo-20261023/blue-inline/`。

2026-10-23 胶囊文字裁切修订：宣传组件在40px外壳以及22px状态槽、20px正文槽分别约束绘制边界；中英文过渡与窄屏全阶段复验见 `artifacts/website-promo-20261023/text-containment/`。当前蓝色官网三语言导航顺序统一为 EN、简、繁。

2026-10-23 官网自动发送案例：`website/{index,en}.html#scene-send` 的取消窗口改为1.5秒，`#auto-send-card` 复用同一 `demos/capsule.html` 场景的compact布局，统一靛蓝胶囊、Esc、圆环、发送与退场。仅官网合成示例，未改原生客户端默认2秒。验收：`artifacts/website-send-1500/`。

### 蓝色背景流动（2026-10-23）

三语言共用两层低对比度蓝色渐变，以28秒/37秒交错往返，使用 transform 移动；首尾蓝色光晕恢复24秒/31秒缓动。后台标签暂停，首尾光晕离屏暂停，减少动态效果时静止。无新增文案或布局改动，仅本机官网预览。

本轮 Chrome 英文页面截图与两次实际变换采样确认流动、无横向溢出；Ego 简繁页样式、390px宽度和减少动态效果检查通过。Ego 英文页读取旧样式且截图超时，保留失败记录，不作为通过证据。结果、截图和复验脚本：`artifacts/website-flow-20261023/`。复验打开三语言 `#more-abilities`，观察缓慢流动，切至减少动态效果应停止，再检查390px无页面横向溢出。

## 词典规则编辑（2026-10-23）

`dictionary-rules.html` 为手动词条/规则、上下文和编辑/取消的独立草稿，已接入统一导航与共享返回入口。旧完整词典仍保留在 `auto-send.html#dictionary`。原生行为见[词典规格](DICTIONARY.md#手动配置编辑与被动建议过滤2026-10-23)。

词典去说明复验（2026-10-24）：新规则稿及旧 `auto-send.html#dictionary` 均不显示「将添加为词汇提示」；旧页面同步去掉关联的无障碍说明引用。当前原生行为与成对质量结果见 [词典复验](DICTIONARY.md#去说明与前后对比复验2026-10-24)。

## 实时双语字幕（2026-10-24）

`live-subtitles/index.html` 为系统音频英 → 简中实时字幕的独立 Mock：模拟演示背景上的可拖动深色半透明浮层，英文逐词出现后显示定稿中文，含暂停/继续、重播、关闭/重新打开、字号、不透明度与原文显隐，支持键盘操作与减少动态效果。台词为预设示例，不采集音频、不调用模型；已接入统一导航与共享返回入口。手动交互复验步骤见本地 `mock/live-subtitles/README.md`，仅代表 HTML Mock，不代表原生实现。

实时字幕第二版（2026-10-24）：浮层增加右下角悬停/聚焦缩放柄（双向、键盘、视口边界，缩放不移动浮层），显示卡片改为原文/译文独立字号步进、不透明度与原文显隐。设置入口 `auto-send.html#settings/subtitles` 位于现有设置窗口，使用独立的识别模型、识别语言（本地 Qwen：中文/自动/英语；其他服务为服务默认）和翻译目标，不影响听写默认模型与翻译听写。两页偏好经 `components/live-subtitles-prefs.js` 的作用域 localStorage 同步。第一版归档于 `archive/2026-10-24/live-subtitles/`。复验步骤见本地 `mock/live-subtitles/README.md`。

实时字幕记录（2026-10-24）：浮层保留本轮已到达的全部字幕并可纵向滚动，底部自动跟随，向上阅读时保持位置并提供「最新」返回按钮；仅标题栏可拖动窗口。暂停、关闭、偏好变化不清空，切换目标重译全部已到达行，重播清空。步骤见本地 `mock/live-subtitles/README.md`。

## 实时字幕说话人（2026-10-25）

`live-subtitles-speakers/index.html` 为同一会话内说话人编辑与保存的独立 Mock，沿用实时字幕第二版深色浮层：预设三人以上英文对话，标签晚于字幕和译文独立到达；点击标签可全部改名、仅改这一句（含新建说话人拆分误判）、全部合并与撤销，手动单句归属不被后续模拟自动结果覆盖。会话需手动保存，刷新恢复已保存会话；未保存时刷新/新建有确认。存储为本 Mock 独占的版本化键 `voicemore-mock-live-subtitles-speakers.*.v1`，不读写第二版字幕偏好，不含声纹或跨会话身份。保留缩放、历史滚动与「最新」、显示设置和共享返回入口；无录音、播放、识别/翻译接口、全局快捷键或原生交互。失败场景、复验步骤与限制见本地 `mock/live-subtitles-speakers/README.md`；第二版 `live-subtitles/` 保持不变。
