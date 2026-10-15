# 首页、历史记录与设置分类

首页不显示全局「一切就绪」状态徽标。听写入口使用左侧淡靛蓝渐变与柔和阴影；听写入口和四栏设置区不绘制外边框，仅保留四栏内部细分隔线和控件边框。四栏标题、数值和控件统一按 22/24/28 高度排列。

首页使用顶部听写入口、四等分设置区（快捷键、整理文字、语音识别、权限）和最近四条记录。快捷键、触发方式、整理开关和麦克风复用设置保存逻辑；自定义快捷键保留，不能切换为按住模式；断开的麦克风保留选择并显示断开状态。首页听写按钮延迟三秒启动，以便切回目标输入框。

首页和历史记录共用时间轴：按本地日期分组，绝对时间、竖线和节点常显。正文、复制、详情、删除互相独立。次要信息与操作仅悬浮、键盘聚焦、展开或复制反馈期间显示。三个操作均为 28×28，间距 6；复制后原位显示对勾 1.5 秒，不弹成功提示。首页最近记录不提供搜索，仅保留「显示全部」；点击后进入未筛选的历史记录。历史仍支持搜索与分页。

详情在记录下方展开，保留录音回放与跳转、修订与词典候选、重新转写、重新整理、所有文本版本、写入结果与计时。删除仍调用原有历史删除逻辑，同时删除关联录音。详情内诊断展开偏好仍保存；重新处理后可临时展开。搜索隐藏当前记录或切换页面时停止回放。

设置顺序：通用、模型、听写、文字与粘贴、权限、隐私。通用保留语言、外观、胶囊、启动、Dock、引导；音频、快捷键、默认整理和修改学习移入听写；基础格式与远程桌面移入文字与粘贴。仅迁移界面归属，不迁移或重置设置数据。设置页的相关选项以浅灰底、14px 圆角和细边框分组，组内保留细分隔线，分组内容纵向间距为 16px；模型默认配置和提供商卡片沿用相同样式。深色模式使用深灰卡片及可见分隔线，高对比度模式保留系统边框。

## 原生验收

1. 打开首页，检查四栏文字与控件对齐，最小窗口不重叠；打开各设置入口。
2. 通过首页改变触发键、模式、麦克风，核对设置页一致，恢复原设置。
3. 首页检查无搜索框，打开全部记录后在历史页搜索；验证日期与时间轴、搜索、分页。
4. 悬浮/键盘聚焦记录，核对复制、详情、删除顺序；复制后出现对勾并自动恢复。
5. 展开详情，检查播放/暂停/跳转、修订、版本与诊断，展开另一条后停止原回放。
6. 使用可丢弃的测试记录验证修订、重新转写、重新整理、删除；不要删除用户真实记录。
7. 检查六类设置的选项完整、语言切换以及重启后的持久化。分别在浅色、深色及高对比度模式检查分组圆角、底色、外框和组内分隔线；模型卡片应有相同分组边界，滚动与控件操作正常。
8. 验收记录与截图存放本地 artifacts/，未实测项明确标记，不以编译成功代替原生验收。

## 交互视觉

分段按钮选中态与开启的开关使用靛蓝色。默认主题取消装饰性边框，以底色和留白区分控件；保留时间轴、四栏内分隔及高对比度与键盘焦点提示。首页四栏设置箭头默认隐藏，指针停留整栏 500ms 后显示，离开即隐藏；键盘聚焦该栏时立即显示。验收时检查快速掠过、持续悬浮、移出与 Tab 聚焦四种状态。

模型下拉与触发框同宽，选项填满行、选中态有灰底；箭头收起向下、展开向上。服务商弹窗输入框与模型列表使用灰底和细边界，验收时检查浅深色及键盘选择。

## 应用身份与图标（HTML demo 验证）

原生 `CaptureTarget` 通过 `NSWorkspace.frontmostApplication` 读取 `localizedName` 和 `bundleIdentifier`；当前历史仅保存 `targetApp` 名称，尚未持久化 Bundle ID 或图标。名称不代表网站身份，也不保证唯一；例如本机 `com.openai.codex` 的系统名称可以是 ChatGPT。远程桌面只能据此确认本机客户端，不能识别远端输入应用。

本地 `mock/claude.html` 的首页时间轴、历史列表和详情展示应用图标与名称；旧版 `mock/index.html` 的详情也接入相同组件。图标由 `scripts/export-demo-app-icons.swift` 使用 AppKit 导出，优先采用运行中应用的 icon，其次采用 NSWorkspace 的应用文件图标。不包含 Dock 上的通知数字、进度或其他动态覆盖层。图标和应用清单只留在 Git 忽略的 mock 目录。

复现：在项目根目录执行 `swift scripts/export-demo-app-icons.swift mock/assets/apps`，再执行 `python3 -m http.server 48181 --bind 127.0.0.1 --directory mock`，打开 `/claude.html`。导出文件为应用身份快照，不读取私人听写内容；页面仍使用演示转录，不会实时监控前台应用。匹配优先 Bundle ID，其次演示别名及唯一的系统名称；未知、同名歧义或缺失图标保留文字和通用图标。原生产品接入时应在捕获目标时保存 Bundle ID，并按身份缓存系统图标；本次没有修改原生历史格式。

浏览器验收：跳过引导进入首页，核对应用名称、图标；打开全部历史、展开详情，确认身份一致；切换浅色和深色，检查图标透明边缘；图片缺失应保留通用图标与名称。实际证据见本地 `artifacts/app-icons/`。

## 历史统计（仅本地 HTML demo）

`mock/claude.html?lang=hans#history` 可直达历史页。顶部提供今日、近 7 天、近 30 天和全部范围，默认近 7 天；展示成功写入字符、录音次数、录音时长、成功写入次数与写入率。时段按本机日历日计算至当前时刻，字符按 Unicode 码点计数并去掉空白；失败、取消、未写入不计入成功字符。写入文本在记录创建时保存，修订与界面翻译不改变历史统计。

趋势图与应用分布使用完整时段数据；日期范围同步筛选下方记录。应用行可点击筛选，搜索和应用筛选不改变顶部统计，分页也不改变总量。删除演示记录后重新计算。未知应用保留名称；无数据时显示零值、空图与应用提示，写入率显示横线。历史页使用单一纵向滚动区，窄窗口统计改为两列，趋势和应用分布上下排列。

统计脚本和样式分别位于本地 `mock/assets/history-stats.js` 与 `history-stats.css`。新增确定性近月演示记录，不读取私人历史文件，刷新恢复；`?history=empty&lang=hans#history` 和 `?history=failed&lang=en#history` 分别复现空数据与全失败。该功能尚未接入原生产品；mock 与截图继续不入库。

验收步骤：切换日期范围，核对图表各柱与应用字符合计等于顶部字符数；点击应用、搜索无匹配文字、清除筛选、删除一条演示记录；近 30 天翻页应保持总量；展开详情并切换浅深色与窄窗口。截图、实测结果和修改前备份保存在本地 `artifacts/history-stats/`。

HTML demo 的历史与词典页面隐藏整个顶部标题栏（含标题文案、分隔线及 52px 占位），内容直接从主区域顶部开始；首页仍沿用原布局。浏览器验收两页标题栏实际高度均为 0，内容起点与主区域起点一致。截图见 `artifacts/history-stats/history-no-title.png` 与 `dictionary-no-title.png`。

## V2 对比方案（仅本地 HTML demo）

`mock/compare-v1-v2.html` 提供历史与词典的 V1/V2 并排、单版、语言和外观切换；V2 独立入口为 `mock/claude-v2.html?lang=hans#history` 和 `#dictionary`。V1 文件保持原样，V2 使用独立存储命名空间。两页继续隐藏顶部标题栏。

V2 历史突出写入字符，保留次数、时长与写入率核心数值，移除辅助统计文案。输入趋势为日期 × 小时方格热力图，日期倒序，悬浮查看字符、点击筛选。趋势与应用面板等高，超长内容内部滚动。应用显示系统图标、名称、字符与百分比，每行有独立细横条。字符主卡缩窄，次数标签为「语音输入次数」，时长精确到分钟。写入率横条按结果分段，悬浮或聚焦显示数量与占比，点击筛选记录；支持与应用、小时、搜索组合并清除，顶部汇总保持不变。词典保持上一版。全部统计仍来自演示记录，尚未接入原生产品。

复现：按上一节启动 48181 服务，打开对比入口，切换 History/Dictionary、Light/Dark；在 V2 切换日期范围、选择小时方格再清除筛选，顶部总量应不变；在词典添加临时词、搜索后删除。验收记录及截图见本地 `artifacts/history-stats/v2-acceptance.md`，设计说明见 `mock/v2-design-notes.md`。

V2 dense heatmap: responsive 24-column grid fills available width, 2px horizontal gaps and 16px cell height. Both panels are 300px tall with inner scrolling. Dates use M/D (10/16), newest first; hours descend 23 to 0 left-to-right. No year/month headings. Arrow keys follow visual order; original data indices preserve tooltip and filter mapping. Browser evidence: artifacts/history-stats/v2-dense-final.png.

V2 typography: panel headings use 15px, matching the record heading; app names in both distribution and record rows share 13px text and 20px icons. Distribution numbers use 13px, percentages 12px, axes 11px. Browser measured both panels at 300px and both identity labels at 13px. Evidence: artifacts/history-stats/v2-readable.png.
