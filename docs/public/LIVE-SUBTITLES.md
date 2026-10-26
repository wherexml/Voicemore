# 实时双语字幕（原生，2026-10-24）

本页描述当前源码；按职责查找实现见[字幕模块入口](../../internal/subtitles/README.md)。历史验收按下文日期记录，不能视为本轮复验或正式版本已发布。

依据：`mock/live-subtitles/{styles.css,app.js,README.md}`、`mock/components/live-subtitles-settings.css`、`spec/DESIGN.md`。验收计划：`artifacts/native-live-subtitles-20261024/acceptance-plan.md`（Git 忽略）。

代码：`subtitles.go`（浮窗、记录、显示卡片、开发夹具）、`subtitles_speakers.go`/`subtitles_store.go`（说话人与保存，见下文）、`subtitles_live.go`（路径、翻译、事件日志）、`subtitles_correct.go`（后台校正的应用与重译通道）、`prefs_subtitles.go`（设置分类）、`internal/subtitles/`（采集助手、Qwen/sherpa 引擎、会话）、`internal/config/subtitles.go`（独立偏好）、`scripts/subtitle_capture.py`（助手构建与签名）。

## 故障场景（先于实现列出）

| # | 场景 | 期望 |
|---|------|------|
| F1 | 拖动字幕区、选择文字或拖滚动条 | 窗口不移动；仅标题栏（含抓手）拖动窗口 |
| F2 | 右下角缩放到极小 | 不小于 320×96；标题栏控件仍可点；尺寸写入独立偏好，新字幕到达不重置 |
| F3 | 跟随底部时新字幕/缩放/字号变化 | 仍停在最新行，不出现「最新」 |
| F4 | 阅读历史时新字幕/译文到达、缩放、字号或原文显隐变化 | 仍停在同一条记录的同一相对位置；「最新」可见，点击后回到底部并恢复跟随 |
| F5 | 原文与译文字号到达端点 | 对应 −/+ 禁用；原文 12–24、译文 16–36 独立保存 |
| F6 | 关闭「显示原文」或片段已是目标语言 | 只显示一行；原文字号行禁用；简体→繁体不算同语言 |
| F7 | 关闭后重新打开 | 记录、位置、尺寸、字号保留；不影响听写设置 |
| F8 | 字幕偏好写入失败或旧设置无字段 | 回退默认值（原文 15、译文 22、不透明 72%、显示原文），听写设置不变 |
| F9 | 字幕设置改识别模型/目标语言 | 听写 `ASRAssign`、`TranslationLanguage` 不变，反之亦然 |
| F10 | 开发夹具 | 仅 Dev 构建且显式环境变量启用；界面带「示例」标签；不得作为实时识别的实现 |
| F11 | 翻译失败、超时或排队过长 | 原文保留，该行显示「翻译失败 · 重试」；队列 8 条有界；旧代翻译结果被丢弃 |
| F12 | 停止、暂停、关闭、切换引擎/识别语言/目标语言 | 采集助手、引擎进程组与在途翻译被取消；旧会话事件按会话号丢弃 |
| F13 | 采集权限缺失、无回调或全零 | 「无法采集系统音频」/「系统音频未启动」/「未收到系统声音」；不回退麦克风 |
| F14 | 模型/运行时缺失或 worker 版本不符 | 明确错误与「字幕设置」入口；不改听写模型；不修改已安装 worker |
| F15 | 显示卡片在 320×96 浮窗中 | 卡片是浮窗外的独立 flyout，完整可见；点外部、Esc、再次点按钮关闭 |
| F16 | 听写使用托管语音（Sub2API） | 字幕翻译仍按文字模型设置解析；只改内存副本，听写路由不变 |
| F17 | 连续讲话始终不到定稿 | 稳定子句照样起草翻译 |
| F18 | 部分结果被撤回或改正 | 迟到的旧草稿结果不覆盖最新修订 |
| F19 | 定稿与草稿不同 | 同一条目原位校正，不追加重复记录 |
| F20 | 修订很快或文字服务很慢 | 草稿合并，当前片段优先，并发有上限，过时请求取消 |
| F21 | 叹词、单词或无标点中文 | 不无限等待，也不逐字闪烁重译 |
| F22 | 引擎设置为空或无效 | 默认 sherpa；Qwen 仍可显式选择 |
| F23 | 后台 Qwen 校正：加载、缺失、失败或卡死 | 首条字幕、采集启动与首次翻译从不等待 Qwen；首个 partial 后才加载；缺失/失败只记 `correction_unavailable`，sherpa 照常 |
| F24 | 校正音频范围错位或长时间连续讲话 | 范围取识别器解码时钟（reset 前后的 `start_time`），不取已接收样本数（后者含前瞻，曾把下一句首词「时」「闹了」并入上一句）；60 s 环形缓冲，不累积；超 24 s 或已被覆盖则跳过 |
| F24a | 首/末 token 紧贴边界（词可能被端点切开） | 首 token <0.24 s：有限前滚（≤0.4 s，不早于上一句末 token 后 0.5 s）；Qwen 仍缺 sherpa 首字（或末 token 紧贴结尾时缺末字）则保留 sherpa，不补写文字 |
| F25 | Qwen 跟不上 | 等待 ≤3 条，满则丢最旧；出队时距定稿 >25 s 丢弃；单次 12 s 超时即重启助手一次，再失败本会话停用校正 |
| F26 | 迟到、重复或错配的校正 | 只作用于同会话、已定稿且修订号等于该定稿的条目，每条至多一次；暂停/新会话/关闭取消在途校正与重译 |
| F27 | 校正重译与实时翻译争抢 | 独立后台通道（1 个），不占也不抢占实时槽位；成功前保留原有译文 |
| F28 | 误校正 | 空结果、边界首/末字丢失或折叠后长度比超出 0.4–2.5 拒绝并保留 sherpa 原文（这些只拦明显失败，不证明校正正确，须逐条复核）；仅标点/大小写不同则替换原文不重译 |
| F29 | 字幕运行时按下听写 | 麦克风听写的触发、转写与写入不受影响；字幕不碰麦克风、默认设备、全局触发键、正式 ASR 服务或共享模型管理 |
| F30 | 听写进行中（准备/录音/定稿/整理/写入） | Qwen 校正不加载、不推理，在途推理中止并保留任务，空闲后恢复有界队列；sherpa 与实时翻译继续，实时翻译只用 1 个槽位，历史与校正重译暂停 |

完整失败计划：`artifacts/native-live-subtitles-hybrid-20261025/failure-plan.md`（Git 忽略）。

## 窗口与显示

- 独立 `mygo.Window`：无标题栏、透明背景、置顶、所有桌面可见，最小 320×96。面板为深色圆角 16，背景 `rgba(16,16,20,opacity)`，1px `#ffffff14` 描边；主题为深色并使用应用紫色 Accent（`#6366c4`），根背景保持透明。
- 首次打开：当前显示器工作区底部居中（离底 max(64, 10%)）。保存过的位置只在与某显示器工作区相交时恢复，并夹回工作区内。标题栏拖动（`OnMove`）、缩放（`OnResize`）在 400 ms 静止后保存。
- 标题栏 40 DIP 是唯一的拖动区：抓手（聚焦后方向键移动 10 DIP，Shift 40）、电平、`系统音频`、`ZH → EN` 标签、状态、暂停/继续、新会话、显示设置、关闭。
- 右下角缩放柄：`CursorResizeNWSE`。按住时按 `mygo.Screen.CursorScreenPoint()` 相对按下点计算尺寸（不用 `Dragged`，因为柄随着窗口角移动，窗口内的增量会被抵消）；聚焦后方向键缩放 10 DIP，Shift 40。
- 记录：所有会话的条目按时间顺序；历史原文 `#a3a3a3`、历史译文 `#d4d4d4`、当前译文 `#fafafa` 500。等待译文时原文可见并显示三点；失败显示「翻译失败」与「重试」。
- 跟随：`ScrollState.Y = math.MaxFloat32`。读者上滚后记录锚点（首个可见条目 + 条目内比例）。`Element.Bounds()` 返回构建前那次布局，所以宽度、字号或原文显隐变化后，从下一帧起每帧按锚点恢复 `Y`，直到内容高度连续两帧不变（最多 10 帧）；恢复期间的再次变化（拖动缩放）沿用起始锚点。验收须用大幅变化：译文 16→36、原文开关、宽度 720→320。
- 设置侧栏顺序与 mock 一致：通用、模型、听写、实时字幕、输出、权限、隐私；「打开字幕」为纯黑按钮、白字。
- 显示卡片：`mygo.NewFlyout`（Parent 为浮窗，Focusable，top-end，间距 6，272×228），空间不足时由 mygo 翻到另一侧并滑入工作区。

## 实时链路

```
VoicemoreAudioTap（Core Audio process tap，排除自身）
  → stdout 16 kHz 单声道 PCM16，100 ms 块，有界队列 5 s（满时丢最旧并记数）
  → Qwen 短段 worker（WebSocket）或 sherpa-onnx 流式（stdin/stdout JSONL）
  → partial（按会话+段+修订号更新同一条）→ final → 翻译队列 → 译文
```

- **采集助手** `internal/subtitles/capture/main.swift`：由已验证原型改写，连续输出、不写文件；stderr 为 JSON 状态（progress 含帧数与非零比例、zero_audio_warning、no_callbacks、error、summary）。SIGTERM、stdin EOF（应用退出）或 stdout 关闭即拆除 tap 与聚合设备。由应用作为子进程启动，权限归属应用；`mygo.json` 含 `NSAudioCaptureUsageDescription`。
- **Qwen 短段**：只读使用已安装的 `local-asr`（`runtime/bin/python3.11`、`worker/`、`model/`）。把 worker 复制到 `<数据目录>/subtitles/runtime/qwen-short-worker/`，只把 `MAX_SEGMENT=30`、`MAX_SEGMENT_EXTENSION=2` 改为 8、1（目标 8 s，硬上限 9 s）；行不符即报 `qwen_worker`，不猜测。独立随机端口（拒绝 48132）、独立实例令牌、`VOICE_INPUT_PARENT` 为本应用 PID，等待 `/health` 就绪最长 180 s。
- **Qwen 根目录**：本应用管理的 `local-asr`（存在 `model/` 时），否则 `~/Library/Application Support/Voice Input/local-asr`（已安装正式版，只读）。
- **sherpa 流式**：根目录 `<数据目录>/subtitles/sherpa/`，含 `venv/bin/python3`（已装 `sherpa_onnx`、`numpy`）与 `model/`（`tokens.txt`、`encoder-*.int8.onnx`、`decoder-*.onnx`、`joiner-*.int8.onnx`）。脚本 `internal/subtitles/sherpa_stream.py` 内嵌于二进制，运行时写入 `runtime/`；端点规则同原型（2.4/1.2/20 s）。
- **进程**：助手、worker、sherpa 各自一个进程组（`internal/subtitles/proc.go`）。关闭是幂等的：已退出则不发信号；否则 SIGTERM+SIGCONT（被挂起的子进程也能收到），宽限期（助手 2 s、sherpa 3 s、Qwen 5 s）后 SIGKILL，再等最多 2 s，返回时子进程已回收。每个引擎和助手都有独立的 ctx 监视：会话取消时即使 `Run` 卡在写管道或 WebSocket 上，也会关闭 stdin/连接并结束进程组。`Run` 在返回前完成这些关闭。验收：`kill -STOP` 挂起 sherpa 或 worker 后暂停字幕，`pgrep` 中不再有该进程组。
- **会话**：暂停、关闭、新会话、切换引擎或识别语言都会结束会话；会话结束时未定稿的片段转为定稿；翻译器已停止时显示「重试」。
- **引擎默认**：未设置或无效时为 sherpa（流式）；Qwen 短段（准确优先，约 8–9 s 一次定稿）需显式选择。sherpa 会话默认附带后台 Qwen 校正（下节）；Qwen 短段模式不变。
- **翻译服务**：`cleanup.Translate`，每个任务重新解析文字模型（在设置中修复后，「重试」即用新配置）。由 `standaloneTextOptions` 解析：听写为托管语音时，在设置副本上把 `ASRAssign` 视为本地后调用 `providers.CleanupOptions`，不写回、不改听写路由。每个请求 30 s 超时。
- **切换目标语言**：重启翻译器（旧代结果丢弃），历史定稿按新目标重新判定同语言或重新翻译，当前片段按现有文字起草；每条记录保存自己的目标，旧目标译文不会显示在新标签下。暂停时切换，历史显示「重试」。

## 听写优先（`internal/subtitles/priority.go`）

字幕与麦克风听写走两条独立采集路径：字幕只用系统音频 process tap，从不打开、停止、重置或重新配置麦克风与默认设备，不注册全局触发键，不连接正式 ASR 服务端口，不经过共享模型管理。字幕不排在听写之后：采集、sherpa 与实时翻译在听写期间照常运行，只有可选工作让路。

- **状态来源**：`main.go` 的 `ctrl.OnChange` 每次调用 `noteDictationPhase`（只读控制器阶段，不改听写行为）；准备、录音、定稿、整理、写入为忙，其余（空闲、完成、失败）为闲。`subtitles.Gate` 保存该状态，带变化通道，供校正器与浮窗读取。
- **Qwen 校正**：忙时不开始加载或推理；正在加载或推理时结束助手进程（MLX 无法中途停止生成），任务放回队首，不计入重启失败次数。闲时恢复：队列仍 ≤3；过期判断不计让路时间（25 s），另有含让路时间的 90 s 绝对上限。
- **翻译**：忙时校正重译通道不启动新请求，进行中的请求取消并在空闲后重排；实时翻译继续但只占 1 个槽位，历史、重试、换目标的请求等待；进行中的实时请求不取消。不修改任何设置。
- **开发验收快捷键**：`VOICE_INPUT_SUBTITLES_LIVE`/`_FIXTURE` 运行仍不绑定听写触发键（`bindTrigger`），以免抢走正式版的 Fn；同进程验收用菜单栏开始听写。
- **跨进程边界**：Gate 只反映本进程的听写。Dev 字幕与已安装正式版听写同时运行时，正式版听写对 Dev 不可见，校正不会让路；两者在 GPU、内存与文字服务上的竞争只能测量，不能由本机制避免。同进程（正式版内字幕 + 听写）才是该优先级的真实验收场景。
- **日志**：`dictation_priority`（`busy`、`phase`、`live_active`、`corr_requeued`）、`correction_deferred`（`during`=idle/loading/inference、`job`、`depth`、`loaded`）、`correction_resumed`（`deferred_ms`、`depth`）、`correction_translation_cancel why=dictation`。

## 后台 Qwen 校正（`internal/subtitles/correct.go`、`qwen_correct.py`、`subtitles_correct.go`）

sherpa 负责首条字幕与早期翻译；每个 sherpa 定稿的原始音频再交给已安装的 Qwen3-ASR 重新识别（不是润色 sherpa 文字），成功后原位替换同一条目。

- **音频范围（解码时钟）**：端点时 `sherpa_stream.py` 先取 `get_result_all()`（`start_time` 与 token 时间，reset 会清除），再 `reset`，再取新的 `start_time`。`start_time = frames_since_start × frame_shift`，Reset 推进的是已处理帧，而不是已接收样本：已接收样本数比它多出模型前瞻（实测 24.5 vs 24.32 s、52.9 vs 52.80 s），用它切会把下一句首词并入上一句。定稿报告 `start_sample`（reset 前 `start_time`）、`end_sample`（reset 后 `start_time`），四舍五入到样本并夹在已接收范围内；每次 reset（含无文字的静音端点）都把下一段起点移到新的 `start_time`。另报 `first_token_s`/`last_token_s`（相对起点）、`prev_token_sample`（上一有字段的末 token）与 `clock`（`decoded`；识别器不提供时回退 `fed`，此时不判断边界风险）。partial/final 的文字与 `token_end_s` 不变。解码时钟与环形缓冲同以会话第一块音频为 0 点，`Run` 先写缓冲再送 sherpa。范围 <0.5 s、>24 s 或已被覆盖时跳过（`too_short`/`too_long`/`evicted`）。
- **边界风险**：首 token <0.24 s 为 `lead_risk`：起点向前补最多 0.4 s，但不早于上一段末 token 后 0.5 s（上一段末字不会被带进来）；末 token 距终点 <0.24 s 为 `tail_risk`（通常是规则 3 强制端点）。有风险时，Qwen 折叠文字的首字（或末字）须与 sherpa 相同，否则保留 sherpa（`boundary_prefix`/`boundary_suffix`）。不补写、不拼接文字；若 sherpa 自己在边界字上识别错，正确的校正也会被保守拒绝。
- **加载**：校正器在会话开始时创建但不加载；首个非空 partial 发出后才启动助手。加载在校正器自己的 goroutine 中进行，采集、sherpa、翻译从不等待。
- **助手**：`qwen_correct.py` 内嵌于二进制，运行时写入 `<数据目录>/subtitles/runtime/`；用已安装 `local-asr` 的 `runtime/bin/python3.11 -I` 只读加载 `model/`，与听写 worker 的 `transcribe` 选项相同（无热词，语言 `auto`，因为 sherpa 模型为中英双语）。只用 stdin/stdout（JSON 头 + PCM），不开端口、不改正式 worker/模型/设置。进程组与会话 ctx 绑定，`Run` 返回前已回收。
- **队列**：单线程推理；等待 ≤3 条，满则丢最旧（`saturated`）；出队时距定稿 >25 s 丢弃（`stale_age`）。单次 12 s 超时或助手退出即结束助手，下一条按需重启一次；再失败本会话停用（`correction_disabled`）。模型缺失或加载失败：`correction_unavailable`，无界面提示，sherpa 照常。
- **应用**（主线程）：校正事件带会话、段与所校正定稿的修订号；只在同会话、条目已定稿、修订号相等且尚未校正时应用，否则记 `revision`/`duplicate`/`missing`。拒绝：边界风险下丢首/末字（`boundary_prefix`/`boundary_suffix`）；空结果（`empty`）；sherpa 折叠文字 ≥4 字时长度比不在 0.4–2.5（`length`）。这些只拦明显失败，通过不代表正确；质量以逐条对照为准。折叠（去大小写、空白、标点）后相同为 `punct_only`：替换原文，不重译。替换后重新判断同语言。
- **重译**：独立的校正通道（同时 1 个，10 s 超时），不计入实时槽位，不抢占也不被抢占。成功前保留 sherpa 定稿的译文（其结果仍可应用）；成功后丢弃旧文字的在途翻译（`corrected`）。失败时保留现有译文，仅无任何译文时显示「重试」。
- **取消**：暂停、新会话、关闭、切换引擎或识别语言结束会话 ctx，校正器关闭助手并丢弃队列；主线程按会话号与翻译代号丢弃迟到结果。

## 流式翻译（`subtitles_translate.go`）

背景：Qwen 实测每 8–9 s 才定稿，而翻译请求只需 379–725 ms，瓶颈是等待定稿。现在部分结果中稳定的子句先翻译，同一条字幕随修订更新，定稿后原位校正。

- **起草策略**（无标点中文适用，不等端点）：
  - *boundary*：最近两次修订共同前缀中，最后一个子句标点（，。！？；：、,.!?;:）之前的部分；
  - *stable*：某次修订 600 ms 内没有新修订，取全文；
  - *maxwait*：修订持续不断时，距上次起草（或首次修订）满 1.8 s，取共同前缀。
  - 起草须有意义长度（≥4 个汉字或 ≥3 个词，叹词等定稿）；须比上次草稿多 ≥4 字或以子句标点结束，或是对上次草稿的改正；同一条目两次起草间隔 ≥400 ms（stable 除外）。不会逐字重译。
- **定稿**：定稿文字与已显示草稿的原文完全相同则直接标为定稿，不再请求；否则请求定稿翻译，进行中的草稿请求被取消（文字相同则直接转为定稿）。结果替换同一条目的译文，不新增记录。
- **调度**：每条目最多一个等待任务（新草稿替换旧的，合并）和一个运行任务。全局 2 个活动槽位；被取消但尚未返回的请求不占槽位，但计入硬上限 3。
  - *实时工作*：正在运行的会话中尚未定稿的片段，以及 3 s 内的定稿；从最新开始优先。其余为历史（旧定稿、重试、换目标），最多占 1 个槽位。实时工作遇到槽位全满时，抢占最旧的运行中历史任务，被抢占的任务重新排队。
  - *草稿期限*：草稿请求 3 s 超时（定稿 30 s）。运行中的草稿超过 1.2 s 后，同条目的新草稿会取消它；健康请求（约 0.4–0.7 s）不受影响。定稿到达时，进行中的同文草稿未满 1.2 s 则直接当作定稿，否则取消并发起定稿请求。
  - 没有 FIFO 积压。
- **防过期**：任务带代号、会话、段、修订号和目标。草稿结果只在「修订号新于已显示草稿、当前原文仍以该草稿原文开头、尚未有定稿译文、目标一致」时应用；定稿结果只在原文等于定稿文字时应用；其他丢弃并记录原因（superseded / stale / target / error）。
- **显示**：译文行显示最新草稿或定稿，原位更新；草稿到达前显示原文与等待点；定稿翻译失败时保留原文与草稿，显示「重试」。
- **目标**：1–3 s 的译文字幕延迟（相对译文所对应的音频），为目标而非承诺；以 Mac 端固定 75 s 已采集剧集回放的中位数、p95 与异常值为准。

### 时延诊断（`VOICE_INPUT_SUBTITLES_LOG`）

| 事件 | 字段 | 含义 |
|------|------|------|
| `pcm_first` | `t` | 第一块音频送入引擎的墙钟时间：`fed_s` 的 0 点 |
| `asr_partial` / `asr_final` | `fed_s`、`engine_audio_s`、`token_end_s` | 事件到达时已送入引擎的会话音频（上界代理，含引擎缓冲）；引擎自报的音频位置（Qwen：worker 的 `audio_s`，语义按 worker；sherpa：流已接收音频）；sherpa `get_result_all().start_time + timestamps[-1]`，即识别器估计的末个 token 时刻，不是标注的语音结束，无时间戳时 -1 |
| `draft_clause` | `reason`、`src`、`timing_source`、`timing_rev`、`text_is_timed_hypothesis`、`hypothesis_age_ms` | 稳定子句产生；计时来源见下 |
| `translation_start` | `queue_ms`、`inflight`、`active`、`live`、`deadline_ms` | 请求开始 |
| `translation` | `request_ms`、`revision_to_apply_ms`、`applied`、`why`、`timing_source` | 请求耗时、修订到应用、是否应用 |
| `translation_cancel` | `why`、`running_ms` | final_supersedes_draft / slow_draft_superseded / preempted_by_live / dropped |
| `ui_apply` | `t` | 主线程应用状态；绘制在下一帧（约 16 ms 内，未单独测量） |

**计时来源**：草稿是某次修订文字的前缀时，从最近 32 次修订中找文字与它完全相同的那次（`exact_revision`），用那次的 `token_end_s`/`fed_s`；找不到时用当前修订（`hypothesis_lower_bound`），其末个 token 晚于该前缀的结束，算出的语音延迟只是下界，不能称为子句结束延迟。定稿始终为 `exact_revision`。

**校正计时**（与首次翻译时延分开统计，事件名均以 `correction_` 开头，不计入 `translation`/`ui_apply`）：

| 事件 | 字段 | 含义 |
|------|------|------|
| `correction_range` / `correction_drop` | `job`、`start_s`、`end_s`（含前滚）、`decoded_start_s`、`preroll_s`、`clock`、`first_token_s`、`last_token_s`、`prev_token_s`、`lead_risk`、`tail_risk`、`dur_s`、`depth`、`copy_ms`、`why` | 截取范围（会话样本时钟）与入队；丢弃原因 |
| `correction_load_start` / `correction_ready` / `correction_unavailable` | `load_ms`、`code` | 助手加载 |
| `correction_start` / `correction_inference` / `correction_error` | `queue_ms`（定稿→推理开始）、`infer_ms`、`decode_s`、`text`、`sherpa_text` | 排队与推理 |
| `correction_apply` | `applied`、`why`、`kind`、`final_to_apply_ms` | 主线程替换原文 |
| `correction_translation_start` / `correction_translation` / `correction_ui_apply` | `lane=correction`、`request_ms`、`correction_to_apply_ms` | 校正重译 |

区分三种量，分开报告：模型耗时 = `request_ms`；ASR 事件到可见 = `revision_to_apply_ms`（与音频无关，不是音频延迟）；语音到可见（代理）≈ `ui_apply.t − (pcm_first.t + token_end_s 或 fed_s)`，只对 `exact_revision` 计入，`hypothesis_lower_bound` 单列为下界。`fed_s` 不小于文字所对应音频的实际结束位置，所以用它算出的语音到可见延迟是下界；采集助手内部缓冲（约一个 tap 回调）也未计入。报告时须注明为代理。
- **全局触发键**：`VOICE_INPUT_SUBTITLES_FIXTURE=1` 或 `VOICE_INPUT_SUBTITLES_LIVE=1` 的开发运行不绑定听写触发键与设备按键（`bindTrigger`），不拦截 Fn。
- **同语言**：只在原文确为目标语言时显示一次：识别语言为中文且目标为简体中文，或英语对英语；自动识别与 sherpa 按片段文字判断。繁体中文始终翻译。

## 说话人标签与会话保存（2026-10-25）

依据：`mock/live-subtitles-speakers/`（已批准，不修改）。代码：`internal/subtitles/speakers.go`、`speaker_diarize.py`（窗口推理与会话内匹配）、`subtitles_speakers.go`（对齐、标签、菜单、编辑与撤销）、`subtitles_store.go`（显式保存与恢复）。失败计划：`artifacts/native-subtitle-speakers-20261025/failure-plan.md`（Git 忽略）。

- **范围**：仅 sherpa 会话；同一字幕会话内的匿名说话人（「说话人 N」）。不做跨会话声纹：嵌入只在助手进程与应用内存中，从不写盘、写日志或保存；新会话或恢复已保存会话即清空，恢复的说话人只有名称，新音频不会自动匹配到它们。Qwen 短段引擎没有 token 时间，不显示标签。
- **模型**：`<数据目录>/subtitles/speakers/{segmentation.onnx,embedding.onnx}`（pyannote segmentation-3.0、3D-Speaker ERes2Net base zh-cn），在 sherpa 的 `venv/bin/python3` 上运行 sherpa_onnx `OfflineSpeakerDiarization` 与 `SpeakerEmbeddingExtractor`。启动会话前检查文件：缺失则不显示标签并记 `speaker_unavailable`，字幕、翻译与校正不变。
- **推理**：独立助手、单线程（`--threads 1`、`OMP_NUM_THREADS=1`）、`nice 10`、一次一个窗口。首个 sherpa 定稿后才加载，采集、partial 与首次翻译从不等待。窗口 12 s、步长 5 s、右侧保留 1.5 s：每个窗口提交 `[上次提交, 窗口末−1.5 s)` 的转换段，提交过的范围不再改动。落后超过 3 个步长时跳到最新窗口，跳过的音频为「未知」（`speaker_gap why=behind`）；环形缓冲已覆盖为 `evicted`。单窗 8 s 超时：结束助手、重启一次，再失败本次引擎会话停用（`SpeakersOff`），等待中的标签消失、字幕照常。
- **会话内身份**：每个窗口的局部簇（去掉重叠部分后最多 8 s 语音的单位嵌入）与会话质心按余弦相似度一对一贪心匹配（≥0.6；≥0.65 且语音 ≥1 s 时更新质心，权重上限 30 s）；未匹配且语音 ≥1.5 s 的簇成为新说话人（最多 16 个），否则为「未知」。阈值为暂定：取自一个开发样本上未经人工标注的模型输出（分离器归入同一簇的片段约 0.85，其余多为 0.1–0.55），未经人工说话人参考核实，不代表准确率。质心属于字幕会话而不是引擎会话：暂停/继续、切换引擎后身份延续。
- **对齐**（主线程）：sherpa 定稿额外报告每个 token 及其会话样本（`tokens`/`token_samples`，解码时钟）。条目的音频全部提交后，每个 token 取所在转换段的说话人（不在任何段内时取 0.25 s 内最近段；跳过的音频为「未知」）；同时落在两个不同声音的段内（包括未识别的声音）为「重叠」。只平滑一种情况：单个 token、≤0.3 s 的「未知」夹在同一说话人之间或位于首尾紧邻某说话人；重叠与更长的未知从不并入。只有当每段 ≥0.8 s、≥2 个 token 且 token 拼写与识别文字折叠后完全一致时，才拆为多句（键为首 token 序号，稳定；未知或重叠段可单独成句），每句标签在自己的原文之上；否则整段为一句：含重叠为「多人重叠」，否则「多人」，不按多数归属。无 token 的条目按 100 ms 采样整段判断：有重叠为重叠，两个已知声音为多人，未知超过 0.3 s 为未知。会话结束时仍未覆盖的条目为「未知」，不会一直等待。
- **Qwen 校正**：句子范围始终基于 sherpa 的 token；校正后的原文与 sherpa 文字不同时，不再按句拆分原文，标签按顺序排成一行放在整段上方（对齐不确定，显式展示）；每个标签的提示与无障碍名称附带该句的会话时间（m:ss.s–m:ss.s）与 sherpa 原文摘录（最多 24 字），以便确认「这一句」改的是哪一部分；不改变字幕样式、不发起翻译。译文不受影响。
- **编辑**（与 mock 一致）：点标签打开浮窗菜单（浮窗在字幕窗外，320×96 时也完整）：名称（回车或 ✓，空名恢复「说话人 N」，24 字上限）；范围「这一句 / 全部」；「这一句」改为其他说话人或「新说话人」（编号接续、菜单保持打开以便命名，总数 16 时禁用）；「全部」合并到目标（之后到达的被合并簇直接显示目标）。手动归属按句保存，自动结果永不覆盖；撤销（标题栏或 ⌘Z）每次只撤回最后一次编辑（最多 30 步，仅内存），期间新出现的自动说话人保留。编辑只改标签，不重译、不改文字或翻译任务。菜单打开时不自动跟随滚动。「未知 / 重叠 / 多人」只能改为某个说话人，不能重命名或合并。
- **保存**：只在点保存或 ⌘S 时写入单个槽位 `<数据目录>/subtitles/sessions/current.json`（夹具为 `fixture.json`）：主线程生成快照，后台写临时文件 → fsync → 0600 → rename → fsync 目录；失败显示「保存失败」且保持未保存。内容：文字、sherpa 原文（校正时）、定稿译文与目标、时间范围、token 文字与时间、说话人编号/名称/合并、自动标签与手动归属；没有 PCM 或嵌入字段。上限 8 MB、5000 条、单条 4000 字、64 个说话人；写入前先按读取规则自检。读取前先查文件大小（非普通文件或超过 8 MB 不读取），读取量有硬上限；拒绝未知字段、尾随数据、版本不符、超限、重复条目、无效引用、合并环，以及 token 顺序或范围、语句的 token 覆盖与音频范围不连续、越界，整份忽略（`session_restore ok=false why=…`），文件原样保留直到下次保存。
- **恢复**：启动后首次打开浮窗时恢复槽位，以「已暂停」打开、不自动采集；继续后新会话号接在恢复的之后，新说话人编号接在恢复的自动说话人之后。
- **未保存保护**：标题栏显示「未保存」（琥珀点）/「已保存」/「保存失败」。有未保存内容时点新会话弹出「开始新会话？」浮窗：取消（默认焦点，Esc/点外部同取消）、不保存、保存。「保存」先冻结当前会话（停止采集、识别、翻译与标签，迟到结果按会话号与翻译代号丢弃，未定稿片段转为定稿、未覆盖标签转为未知），再保存全部内容；只有写入成功且期间没有新的改动才新建，否则保留已暂停的旧会话及其状态。每次新建会话推进保存代号：新建之前发起、之后才完成的保存不改变新会话的保存状态，也不会触发新建。新建后未保存就退出，下次恢复上次已保存的会话。关闭浮窗不丢内容；退出应用不询问（限制）。
- **听写优先**：复用 `dictationGate`：忙时不加载、不推理，在途窗口结束助手并在空闲后重做；落后部分按上面的有界规则处理（`speaker_deferred`、`speaker_resumed`）。
- **夹具**：`VOICE_INPUT_SUBTITLES_FIXTURE=1` 的示例行按预设脚本在下一行开始时得到标签（第 6 行故意标错，供改归属/合并步骤使用）。第 3、4、5、7 行给出逐词的预设转换段，经真实对齐函数处理：第 3 行中间一段未知（说话人 1 / 未知 / 说话人 1）、第 4 行开头未知、第 5 行一个词重叠（整段「多人重叠」）、第 7 行结尾未知。走同一套编辑、撤销与保存（`fixture.json`）；仅为界面辅助验收，不能代替真实推理。

| 事件 | 字段 |
|------|------|
| `speaker_init` / `speaker_ready` / `speaker_unavailable` / `speaker_disabled` / `speaker_off` | `window_s`、`hop_s`、`load_ms`、`code` |
| `speaker_window` | `window`、`start_s`/`end_s`（窗口）、`from_s`/`to_s`（提交）、`turns`、`unknown_turns`、`local_clusters`、`speakers`、`infer_ms` |
| `speaker_gap` / `speaker_error` / `speaker_deferred` / `speaker_resumed` | `why`、`during`、`behind_s` |
| `speaker_apply` / `speaker_align` | `entries`、`event_to_apply_ms`；每条 `utterances`、`labels` |
| `speaker_edit` / `session_save` / `session_restore` | `what`、`undo_depth`；`ok`、`bytes`、`entries`、`ms`、`why` |

开发开关（仅 Dev）：`VOICE_INPUT_SUBTITLES_SPEAKERS=0`（配对关闭）、`VOICE_INPUT_SUBTITLES_SPEAKER_ROOT`（如指向空目录验证缺失模型）、`VOICE_INPUT_SUBTITLES_DATA`（隔离保存目录）。

限制：标签延迟约为步长加右侧保留再加推理（通常 7–9 s），字幕与译文不等待；短句（<1.5 s 未匹配语音）多为「未知」；拆分只在 token 边界与上述阈值内进行，词中换人或过短插话显示「多人」或「多人重叠」，分离器边缘的零星未知也会使整段显示为多人或未知（宁可显式不确定）；保存进行中再次点保存会被忽略；校正后不能按句拆分原文；译文始终对应整段，不按说话人拆分；匹配阈值来自开发样本；跨进程（正式版听写）不让路。

验收状态（2026-10-25，报告 `artifacts/native-subtitle-speakers-20261025/REPORT.md`）：

- 已通过：Mac 签名 Dev 构建。同一构建、同一 75 s 短剧冻结音频的标签关/开配对原生运行各 9 条定稿，PCM 丢块与会话错误均为 0；首次翻译估计中位数/P95：关闭 1.491/1.998 s，开启 1.091/1.654 s。仅一对运行，口径为模型 token 到主线程状态，不是物理语音到屏幕；无准确率参考，也不能把差值归因为加速。
- 已通过：原生保存后重启，恢复 9 行、5 个在用说话人、11 句，以「已暂停」打开且未采集。
- 已通过：真实模型下注入 Gate 忙状态：8 s 忙（期间 19 个 partial）、26 s 忙（43 个 partial，一处显式 gap）；缺失模型时字幕照常、无标签。这不是新构建上真实麦克风、Fn、控制器与写入的验收。
- 已通过：在保存的真实短剧会话上原生编辑（证据 `real-edits/`、`ui-complete/`：AX 快照、保存的 JSON、`snapshot-check.json`）。自动说话人 3 重命名为「主将」，全部出现处同步；只改第 8 段的归属；为该句新建 m1 并命名「传令兵」；把 m1 合并到 a4 后保存，再撤销并保存。快照对比：撤销后与合并前快照除 `savedAt` 外完全一致；所有原文、译文与 token 字段不变，只有最后一句带手动 m1；编辑期间翻译调用为 0。此前的浮窗焦点与过期几何问题，通过把临时菜单内的操作连续完成并核对刷新后的状态解决，未加任何产品侧变通。
- 已通过：原生夹具保留了较长的未知开头/中间/结尾与短暂重叠（未被并入已知说话人）。
- 已通过：损坏存档的原生恢复拒绝尾随 JSON、读取前拒绝 >8 MiB 文件、拒绝无效 token 时间，期间字幕照常产生定稿。
- 已通过：未保存新会话提示中的「取消」保留 9 行及未保存状态。
- 已通过：原生「保存并新建」确认点击，成功保存 9 条、5 个说话人（0600，16 ms），随后启动会话 2；旧场快照与事件日志保留。Steve 另行确认真人改名成功。
- 已通过（单轮）：当前签名开发构建使用真实 AirPods 麦克风、本地 Qwen、整理关闭、自动发送关闭，通过开发版按钮启停；21.62 秒非零录音，实际控制器忙 22.677 秒，期间 38 个字幕 partial、12 次 UI apply、11 次实时翻译均成功。说话人识别与校正让路后恢复，10.0–29.4 秒的标签积压区间显式保留未知。Steve 确认本轮识别内容正确且已实际写入 ChatGPT。证据见 `physical-parallel/REPORT.md`、`check.json`、`user-confirmation.json` 和 `physical-live/events.jsonl`，不借用旧构建 Fn 证据。
- 本轮边界：预备 TextEdit 仍为空，不能计入该目标通过；没有独立听写延迟对照，不证明听写延迟无回归，也不覆盖云端识别、开启整理、多轮稳定性或跨进程优先级。字幕在音频 74.6 秒暂停，最后一个未定稿片段显示翻译失败；忙碌窗口内的 11 次实时翻译请求均成功。完整短剧的翻译延迟对照仍以此前配对轮为准。

## 安装与运行（开发验收）

先构建：`python3 scripts/build-development.py`（编译并签名 `VoicemoreAudioTap`，放入 `Contents/Resources/subtitles/`，再校验）。正式打包由 `scripts/build-distribution.py` 以 Developer ID、hardened runtime 与时间戳签名同一助手。

sherpa 稳定目录（无日期，已由协调者准备）：`~/Library/Application Support/Voice Input Dev/subtitles/sherpa/`，其中 `venv/`（离线安装 `sherpa_onnx`、`numpy`）与 `model/` 均为实际目录，不是符号链接；目录内清单记录文件哈希。不要用 `ln` 覆盖它们。重新准备时先校验清单哈希，再整体替换目录。

开发开关（仅 `Voicemore Dev` 生效）：

| 变量 | 作用 |
|------|------|
| `VOICE_INPUT_SUBTITLES_LIVE=1` | 启动即打开浮窗并采集真实系统音频（优先于夹具） |
| `VOICE_INPUT_SUBTITLES_FIXTURE=1` | 合成英文夹具，标题栏「示例」，不采集、不识别、不翻译 |
| `VOICE_INPUT_SUBTITLES_LOG=/abs/events.jsonl` | 追加事件日志：会话、助手 progress/exit（PCM 帧数、丢块数）、`asr_partial`/`asr_final`（段、修订、文字）、`translation`（耗时、成败、模型主机），无音频与凭据 |
| `VOICE_INPUT_SUBTITLES_PRODUCTION_TEXT=1` | 在内存中读取已安装正式版的 `settings.json` 与已存在的加密凭据，只读，用于翻译 |
| `VOICE_INPUT_SUBTITLES_TAP` / `_QWEN_ROOT` / `_SHERPA_ROOT` | 绝对路径覆盖助手、Qwen 根目录、sherpa 根目录 |
| `VOICE_INPUT_SUBTITLES_CORRECTION=0` | 关闭后台 Qwen 校正，用于同一音频的配对回放；`session_start.correction` 记录实际状态 |

示例（引擎在 设置 → 实时字幕 → 语音识别 中选择）：

```sh
VOICE_INPUT_SUBTITLES_LIVE=1 VOICE_INPUT_SUBTITLES_PRODUCTION_TEXT=1 \
VOICE_INPUT_SUBTITLES_LOG="$PWD/artifacts/native-live-subtitles-20261024/live-events.jsonl" \
"build/development/darwin-arm64/Voicemore Dev.app/Contents/MacOS/Voicemore Dev"
```

首次采集会出现系统音频录制授权；授权给 `Voicemore Dev` 后重开会话（暂停→继续）。

## 原生验收

1. 窗口（夹具）：首次打开底部居中；拖字幕区不移动，拖标题栏移动并在重启后恢复；右下角光标为双向斜箭头，拖动到 320×96，Tab 到缩放柄用方向键缩放；显示卡片在 320×96 时完整可见，点外部/Esc 关闭。
2. 阅读锚点：上滚到中间某条，改宽度、两个字号、关显示原文，同一条仍在顶部；「最新」回到底部。
3. Qwen：播放中文视频，日志中 `capture_progress` 帧数递增且非零，`asr_partial` 修订递增，`asr_final` 每段一次、无重复条目，`translation` 成功；暂停后 `capture_exit` 与 worker 进程消失（`pgrep -f qwen-short-worker` 为空）。
4. sherpa：切换引擎后旧 worker 退出、新会话号开始，中英混合音频出现分段与译文。
5. 失败：删除 sherpa 链接 → `sherpa_missing`；撤销授权 → 采集错误或「未收到系统声音」；停掉文字服务 → 原文保留、「重试」可恢复。
6. 后台校正（固定 75 s 音频 `artifacts/native-live-subtitles-20261024/streaming-reference/capture.wav`，`replay-tap.py` 回放）：同一音频分别以默认与 `VOICE_INPUT_SUBTITLES_CORRECTION=0` 交替运行；首个 `asr_partial`、首次 `translation` 与 token→UI 时延（基线中位数 1.412 s / P95 1.954 s）不得因校正变差；`correction_load_start` 晚于首个 `asr_partial`；逐条对照 `correction_inference` 的 `text` 与 `sherpa_text` 判断改善与误校正；暂停后 `pgrep -f qwen_correct.py` 为空；`_QWEN_ROOT` 指向空目录时只有 `correction_unavailable`，字幕照常。
7. 听写优先（同进程）：Dev 字幕回放时从菜单栏开始一次听写（Dev 验收不绑定触发键），说话、定稿、写入到真实输入框；日志依次出现 `dictation_priority busy=true` → 期间无 `correction_start`/`correction_load_start`，在途推理有 `correction_deferred during=inference` 且 `pgrep -f qwen_correct.py` 随之消失 → `busy=false` 后 `correction_resumed` 并继续处理；忙期间 `asr_partial`/`translation` 持续，`translation_start.active` ≤1，无 `correction_translation_start`；听写文字、写入与时延与关闭字幕时一致。跨进程（Dev 字幕 + 正式版听写）只记录竞争，不作为优先级通过依据。
8. 隔离：正式版 `settings.json`、`local-asr/worker` 哈希不变；听写 `ASRAssign`/`TranslationLanguage` 不变；无粘贴、自动发送或词典学习。

## 限制

- 初始实现由 Linux 环境编写；后续 Mac 开发构建及有限原生验收记录见「说话人标签与会话保存」。不把早期环境限制或历史通过结论当作当前全部功能的验证状态。
- Qwen worker 的长时间单连接行为沿用原型结论；worker 的 `segment` 编号按协议直接作为条目键。
- 暂停即结束会话，继续需重新加载模型。
- `VOICE_INPUT_SUBTITLES_PRODUCTION_TEXT` 打开已存在的正式凭据库时，`secrets.Open` 会把目录/文件权限校正为 0700/0600（不写内容）。
- 后台校正：规则 3（20 s 无停顿）强制端点可能切在词中，边界词两侧的音频都不完整；末字守护只在末字丢失时保留 sherpa，Qwen 把半个词识别成别的字或末字恰好相同时仍会被采用，前一句被切走的半个词也可能出现在下一句开头（下一句首 token 紧贴边界时，前滚受上一句末 token 限制而为 0）。边界守护只比较一个折叠字符，英文按字母而非词；长度比与守护都只拦明显失败，不保证正确；每次暂停/继续需重新加载 Qwen（首个 partial 后），与正式听写同时使用会共享 GPU 与内存；校正经常改变 sherpa 无标点文字，多数定稿会再翻译一次。历史有限样本不证明普遍改善或时延无回归，新增样本仍须配对验收。
- 听写优先：只覆盖本进程听写；控制器合并的快速阶段变化（如准备后立即失败）可能不产生忙状态；中止推理需结束助手，恢复后重新加载模型；Qwen 短段模式是会话唯一识别器，不让路；实时翻译在听写期间仍可能与整理共用文字服务（已限为 1 个请求）。
- 翻译目标列表固定 8 种；源语言只有中文、英语、自动（Qwen）与中英双语（sherpa）。

## 数据与隐私范围

字幕采集 Mac 正在播放的系统音频，排除助手自身，不根据前台应用的 Bundle ID 过滤声源。[私密应用](PRIVATE-APPS.md)规则约束听写历史和纠正学习，目前不覆盖字幕采集、翻译或手动保存。字幕本身不触发跨应用输入、自动发送或词典学习。

识别和说话人推理使用本地模型；翻译请求交给设置中选择的文字服务，若选择云端则会发送字幕文字。会话仅在显式保存时写入文字和标签，不保存音频或说话人嵌入。Dev 的诊断日志可能包含识别文字、译文和目标服务主机，必须留在本地忽略目录，不能当作无内容的遥测上传。
