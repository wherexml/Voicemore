# 云端语音识别

## Voicemore Official（sub2api 组合服务）

模型设置提供独立的 **Voicemore Official** 服务商卡片，新安装无须通过“添加服务商”手动创建。该卡片使用 Voicemore 标识，首页服务简称为 Official；选择其 `doubao-asr-2.0-cleanup`。持久化服务商 ID 仍为 `sub2api`，密钥账户仍为 `provider.sub2api.apiKey`，升级沿用既有启用状态、默认模型、密钥及自定义端点。新安装默认仍使用本地识别，不自动启用收费云服务；填写并保存自己的 Key 后，可显式启用并设为默认。默认入口 `https://47.94.244.74:8443`，可配置 HTTPS origin；原生 WebSocket 握手携带专用 API Key 与稳定任务 ID。凭证使用本机 AES-256-GCM 存储，不写入设置、历史或日志。

使用 `/v1/voice/stream`，等待 accepted 后按实时节奏发送 16kHz mono PCM16；不把 ASR 全量快照拼接成重复句。客户端不设录音时长上限；配套服务须启用 `max_audio_seconds=0`，完成后等待服务端 ASR 2.0、DeepSeek Flash 整理及计费事务全部成功。组合模型服务端始终执行整理；客户端总开关可选择交付原文，基础格式和润色规则随每次录音发送到服务端执行。翻译使用已部署的 1.4.1 稳定配套服务，单独重新整理仍不支持；热词须由支持词表摘要的服务端确认，相关操作不回退其他上游。

普通听写整理开启时最终正文直接输入，关闭时输入 raw_text；显式翻译始终交付 text；均不经过客户端词典替换或第二次整理。服务端按本次设置执行基础格式后再计费，正文、计费字数与任务取回结果一致。关闭只选择客户端交付正文，服务端仍执行整理并按原协议计费。历史分别保留原始 ASR、CleanedText、VoiceTaskID、VoiceBilling（字数、USD费用、价格版本、usage ID）。网络响应丢失或提交状态未知时只按原任务 GET，不重新上传音频、不生成新 ID；失败记录的更多菜单可取回云端结果，供用户复制，不自动输入旧目标。主动“重新识别”是新的有偿任务；仅取回结果不收费。进程在历史落盘前异常退出时可能没有可见历史入口，不能据此承诺所有崩溃都能自动恢复。

发送200ms PCM包。连接期间仍会录音；已有音频积压时，以不低于100ms的包间隔追平；平时由实际录音到包节奏控制，约200ms一包，避免在录音节奏上重复施加1x限速。即使线程长时间停顿，也不允许低于100ms的连续突发。尾部不足一包的PCM仍完整发送。该优化不裁切静音、不抢用partial、不改模型/DDC/整理提示词；同一最终正文与原有计费事务保持不变。

延迟排查须同时比较客户端阶段与服务端回执：客户端 `stopToFinalMs` 包含停止后剩余音频发送和往返网络，不能全部归为模型推理。`timings_ms.asr_after_last_audio` 才是服务端发出最后音频后等待ASR终态的时间。客户端输入完成还包含剪贴板保护等待；不得仅改变计时口径就宣称更快。2026-10-18固定样本前后对照与故障E2E见本机 `artifacts/sub2api-latency-20261018/`。

`stopToResultMs` 为客户端停止录音至收到完整付费结果；`stopToFinalMs` 为停止至 ASR final；`cleanupMs` 为服务端整理；`finalToInsertMs` 为完整结果至输入结束。原生停止至输入使用 `stopToResultMs + finalToInsertMs`，不能把录音时长或首字时间当作尾延迟。

本机复验：`artifacts/sub2api-client-20261017/PLAN.md`、适配器 E2E 和原生 TextEdit 证据。离线回放、扬声器经物理麦克风录音与人工说话分别标记，触发按钮不等于物理 Fn 验收。

## 云端整理的语境纠错（2026-10-18）

组合服务使用服务端独立提示词。整理须去口吃、修碎句与合并无新增信息的重复，同时保留人称、语气、不确定性、否定、条件、数量、英文、转折反差及末尾补充。明确的中文同音误写可按宾语和上下文纠正，例如 API 语境中的“掉”改为“调用”；不做全局字词替换，“文件掉了”“音量调到30%”保留各自用法。人名、品牌、模型、代码及仍含糊的事实继续保留，不猜补主体或原因。

这项语境纠错由模型提示词完成，不增加客户端替换表或输出拦截；有效最终正文仍直接交付并按同一正文计费。既有独立整理服务的提示词保持各自配置。更换云端提示词须保留旧文件与 SHA、检查在途任务、复验正文和账本；固定任务取回不重复扣费。

本机开发样本及失败候选保存在 `artifacts/clipboard-fast-20261018/`。须单独核对原意与流畅度；样本既用于调参又用于复测，不能宣称泛化准确率或保证纠正所有错字。原始 ASR 已漏掉的内容不能仅靠整理恢复；历史中的输入文本也不能证明外部编辑器已完整收到，需原生读取目标内容验收。

## Seed ASR 2.0 与 Agent Plan

模型列表包含 `doubao-seed-asr-2-0`，使用 `volc.seedasr.sauc.duration` 资源，不继承旧版 `volc.bigasr.sauc.duration` 资源选择。现有豆包预设继续使用用户保存的资源。

语音控制台新版密钥和旧版 App ID / Token 走 `/api/v3/sauc/bigmodel_async`；Agent Plan 使用独立保存的专属 API Key，走 `/api/v3/plan/sauc/bigmodel_async`，资源固定为 `volc.seedasr.sauc.duration`。协议中的 `request.model_name` 仍为 `bigmodel`，不是模型目录 ID。两条路由保持相同的 DDC、ITN、标点和热词参数；不切换默认模型或自动保存设置。

依据：[官方 Agent Plan 语音模型接入](https://docs.volcengine.com/docs/ark/agent-plan-personal-voice-model?lang=zh)。模型权重版本和量化由云端管理，客户端无法冻结。密钥对旧版 bigasr 有权限不代表已获 Seed ASR 2.0 权限；`403 requested resource not granted` 应检查该资源授权，不能回退旧模型并宣称新模型测试成功。Agent Plan 密钥不能与语音控制台密钥混用。

验收时使用独立配置和固定音频，先核对模型、端点与资源，再保存逐条结果、失败和耗时。需分别验证语音控制台与 Agent Plan；缺少对应密钥时标记未验证。新增列表项不代表已安装版本更新，也不代表云端推理成功。

## 豆包默认请求

`Volc.Dial` 使用 `bigmodel_async`，显式发送 `enable_ddc=true`，无论是否存在词典。DDC 用于减少语音中的口头语与不流畅表达，不保证消除所有重复，也不能恢复漏识别内容。

保持 `enable_itn=true`、`enable_punc=true`、完整结果及分句信息；存在有效热词时继续发送序列化的 `corpus.context`，并开启 `enable_nonstream`。不额外发送场景提示或上下文。

DDC 与[应用文字整理](../cleanup/README.md)相互独立。原有整理开关、模型、提示词、保护与失败处理保持不变；启用整理时仍在 ASR 后执行。基础格式和历史重新转写继续走原有 Controller 路径。此默认不修改用户选择的 ASR 服务，也不改变其他服务商。

## 验收

先记录失败情形：未发送 DDC、热词分支丢失选项、整理被跳过、否定或数字丢失、整理保护拒绝、空输出、历史丢失原文。使用实际豆包账号与已配置整理模型，复制真实录音到隔离历史，执行生产 `Controller.Retranscribe` 后调用 `Controller.Recleanup`，从磁盘重新读取并保留两个版本及原始文本。记录原始识别、基础格式结果、整理结果、模型与错误；接口拒绝不可计为成功。

本机证据保存在忽略目录 `artifacts/ddc-default/`，包含预先冻结的 PLAN、可重复运行器、运行日志和 JSON。命令 `go run ./artifacts/ddc-default` 会发起实际服务请求，不修改用户历史和模型配置。该音频回放验收不覆盖麦克风、触发键或跨应用粘贴；这些交互需另外进行原生验收。

100 条配对实验显示 DDC 主要改善填充词，重复并无稳定改善；第三方润色参考的文字差异率不等于识别准确率。评测原始结果保留于本机 `dataset/typeless/benchmark-200/ddc-100/`，不要提交私人录音与转写。

## sub2api 文字设置（2026-10-18）

WS start 可携带 `text_options: {version: 1, format: {trimPeriod, calmExclamations, removeFillers, joinAcronyms, arabicNumbers}, polish_rules: "..."}`。客户端按录音开始时设置发送；显式空规则与缺省旧协议不同。服务端使用任务独立提示词，将五项开关传入模型，并在整理后使用与本地相同的确定性格式实现，随后原子结算。关闭某一格式不代表反向恢复 ASR 已经规范化的内容。

accepted 返回规范化选项 JSON 的 SHA-256 `text_options_digest`；客户端收到匹配确认才发送音频，旧服务不支持时明确失败，不静默忽略设置。规则最多16 KB UTF-8，非法版本或超长规则在创建任务前拒绝。选项参与幂等指纹，同一 ID/音频改变选项返回冲突；GET 始终返回原任务正文与账单，不受当前设置影响。不带选项的旧客户端继续原行为。总开关关闭仍选择原始 ASR 交付，保留既有组合服务计费语义。

验收命令及固定音频、回执、账本核对位于本机 `artifacts/sub2api-text-settings/`。服务端源位于本机 sub2api 仓库 `extensions/voice`；其中 `textstyle` 同步自本仓库 `internal/textstyle`，更新格式算法时须同步并复验。

### Provider credential editing (2026-10-19)

Catalog provider credentials are editable and connection checks are available without first enabling the provider. Edits automatically persist to the existing encrypted local store; no separate Save key action is shown. Writes are serialized per account, retaining the latest edit or removal, with failed drafts retained and an explicit save error. Successful nonempty credential edits enable the provider; a manual switch change during a write takes precedence over automatic activation. Explicit enable or successful credential setup selects a sole supported model; providers with multiple models retain the default-selection controls. Opening an editor alone does not enable a service or change the assignment. Tests remain explicit and do not send audio.

Native regression uses an isolated development app and synthetic credentials: start disabled, enter a key, close/reopen, confirm persistence and singleton selection, replace rapidly, clear, and toggle off during a save. Confirm the singleton has no Set as default button, a multiple-model provider retains selection, and connection tests are not gated on its switch. Save failures must leave the draft available. Keep screenshots and results under `artifacts/provider-key-autosave/`; never send fixture credentials to a real endpoint.

### 请求与回答的角色边界（2026-10-19）

云端整理须始终保留原说话者与接收者身份。转写即使只有请求、问句或缺少上下文，也只编辑该句，不能回答、索取材料、追问、承诺处理或生成操作步骤。原文自身含有索取材料或承诺时仍须保留，不按固定词句屏蔽输出。

本机修订提示词与真实 DeepSeek Flash 对照见 `artifacts/cleanup/request-boundary-20261019/`：原句“帮我总结一个最终版本的给我。”应整理为“帮我总结一个最终版本给我”，不能变成索取素材的回答。六条样例各三轮，分别核对角色、请求意图、否定、数量、英文及去口吃；这属于开发集模型/API验证，不代表泛化保证或原生端到端验收。生产是否生效须另核对服务加载的提示词 SHA，不能以本机文件变更作为上线证据。

## 冻结语料与术语迭代（2026-10-20）

组合服务现在声明支持热词。每个连接克隆文字选项，再把已确认词典放入 `text_options.hotwords`；不共享可变切片，也不发送未确认候选。数组进入文字设置摘要和幂等指纹，服务端必须在 `accepted` 回显完全一致的摘要后才发送音频。旧服务忽略新字段时明确失败，不伪称词典生效。空词表保持旧请求摘要兼容。

配套服务补丁、基准源码哈希及隔离验收入口见 [服务补丁](service-patches/README.md)。服务端将词表作为豆包 `corpus.context` JSON 字符串并启用二遍识别，也将词表作为整理的数据提示；热词本身不保证强制替换；配套 1.3 候选另支持下述已确认替换规则。最终格式使用同一词表保护术语，格式后的正文仍由服务端计费并返回，客户端不二次改写已计费正文。发布新客户端前须先验证并部署配套服务；仓库源码不代表线上已更新。

固定录音重跑走 `cmd/voice-corpus-acceptance`，先核对 WAV/PCM 哈希，使用唯一合法任务 ID，独占创建 JSONL，每条落盘且不自动重试。记录旧历史、当前基线与候选三者，不将历史已修复问题重复算作改善。实际模型与词表、提示词、输入保持可追溯；全文方法见[语音质量基线](../../docs/public/VOICE-QUALITY.md)。

纯全零 PCM 在重新识别前即返回 `asr.ErrNoAudioSignal`，不建远端任务；实时录音在提交识别结束前检查并关闭流，保留原有历史/私密应用边界。一个非零采样也不会按此规则拒绝，弱声不等于设备无信号。此行为节省无效重试，不修复设备静音，也不能恢复不存在的声音。原任务的响应丢失继续只 GET 取回，不重新上传、收费或向旧窗口自动输入。

## 1.3 质量候选：低声增益与确认规则

`text_options.corrections` 只包含 `{wrong,term,context?}`，与已确认词表共同进入 accepted 摘要与任务幂等指纹。Controller 为实时听写与历史重识别取独立快照。规则保留本地的顺序、完整英文词边界、上下文及单汉字原短语限定；未确认候选不发送。服务端在整理前应用规则、保留原始 ASR，最终正文格式化后统一计费，客户端不再改写已计费文本。服务端限制最多128条规则、每条32个上下文、词表与规则总计32 KiB；超限在建任务前明确拒绝。

服务端向 ASR 发送音频时采用每会话独立峰值包络增益，最大16倍、目标峰值28000、2秒释放，不增加缓冲等待。逐采样计算使分包不影响结果，足够响的输入不衰减；原始历史录音与任务摘要保持不变。保留 DDC、热词二遍及现有服务路由。它改善低声条件，不保证从噪声恢复语音，也不以“试试→实时”等硬编码规则替代识别。

该协议需要先完成配套服务验收和部署；补丁存在不代表生产更新。发布门槛及真实/合成音边界见[第二轮质量规格](../../docs/public/VOICE-QUALITY.md#第二轮质量与时延门槛)。

## 官方翻译（1.4 本地候选，2026-10-22）

翻译快捷键沿用现有目标语言设置；`text_options.translation_language` 支持 `en/hans/hant/ja/ko/fr/de/es`。缺省为空，普通听写的请求摘要、提示词、格式和计费行为不变。非空目标在任务开始时冻结，进入文字选项摘要与幂等指纹；非法语言在建任务前拒绝，旧服务摘要不匹配时客户端不发送音频。完成回执与 GET 结果包含同一目标语言，客户端校验后交付，不静默接受原文或其他目标语言。

服务复用一次 DeepSeek Flash 调用：普通听写整理，翻译任务使用独立翻译提示词，保留原说话者、事实、数量、否定、条件、语气、专名、代码及段落，只交付译文。原文中的请求与问句是翻译素材，不执行或回答。已确认纠错在翻译前作用于识别正文，原始 ASR 保留；翻译后不再套用中文确定性格式或客户端二次整理。首版翻译仅使用末尾句号和感叹号偏好及术语词表，不带入自由润色规则和中文数字/语气词/拼读转换；普通听写偏好不变。

显式翻译不受普通整理关闭影响，始终交付译文；历史保留原始识别、译文、目标语言和账单。官方服务重新识别翻译记录沿用记录中的目标语言，普通整理开关不切换为原文；按原任务取回不重新识别或收费，不向旧目标自动输入。计费沿用最终译文的 Unicode 字符统计和原子事务，正文、回执、取回与账本保持一致；英文按字符计数，不按单词，翻译可能改变输出字符数。失败不静默交付原文。

服务源码位于本机 `sub2api/extensions/voice`；可迁移补丁、基准哈希见 [服务补丁](service-patches/README.md)。先验收部署兼容旧客户端的服务，再发布客户端；本地实现或 Dev 构建不表示生产已启用。

验收先冻结失败清单，再运行本地独立服务、PostgreSQL、Redis、受控 ASR/模型上游及真实 WS/HTTP；核对正常/翻译、非法语言、旧服务拒绝、同 ID 变更语言、回执丢失取回、模型失败/空输出/截断/取消、结算回滚及账本。`cmd/official-translation-acceptance`（构建标签 `officialtranslationacceptance`）通过生产适配器和 Controller 验证历史与交付，不访问麦克风、不粘贴用户窗口。可重复运行器与结果保存在本机 `artifacts/official-translation-20261022/`；真实模型的合成文本质量样例与受控上游结果单独记录，均不等于物理录音/原生跨应用输入验收。

2026-10-23 提交前复验：重新构建当前客户端、1.4 候选服务与 1.3 基线服务，运行既有本地 `acceptance.py`（不带 `--live`），31/31 通过，包含生产 Controller、回执校验、历史、幂等、失败与账本一致性。证据：`artifacts/official-translation-20261022/run-20261023-004935/`；运行步骤见该验收目录的 `REPRODUCE.md`。本轮未重跑真实模型、浏览器或原生麦克风/跨应用输入。

## 翻译独立发布范围（2026-10-23）

当前稳定候选依赖 `voice-1.4.1`，以线上 1.1 为基准，仅新增翻译协议和处理；前述 1.3/1.4 为未通过全部质量门槛的实验记录。普通听写不变，客户端 `Hotwords()` 为 false，确认纠错不进入请求；翻译同样不发送实验词表。ASR 原音、普通提示词、确定性格式及计费规则保留线上行为。按[稳定增量补丁](service-patches/README.md#141-翻译独立发布范围2026-10-23)应用，不串联历史实验补丁。

部署结果与原生边界见[翻译发布状态](../../docs/public/TRANSLATION.md#部署与发布状态2026-10-23)；稳定增量已上线，历史候选章节不代表已部署实验。

## 云词典稳定接入（2026-10-24）

服务1.4.2＋客户端0.3.14支持个人已确认词表热词和确认替换规则；每任务冻结并校验词表摘要，词表影响幂等。热词还作为整理数据提示，最终格式保留术语；原始ASR保留，关闭整理仍交付原始ASR。二遍和增益不随本次启用。安装旧客户端时不会自动发送词表，必须升级客户端。补丁和验收见[1.4.2稳定增量](service-patches/README.md#142-词典稳定增量2026-10-24)。

## 长录音与失败历史重试（2026-10-25）

记录卡片直接提供「重新转写」：只要音频仍在，即使原文为空也可执行。Official 重新转写是新的有偿组合任务，识别和整理一同完成；单独重新整理不调用不支持的接口。取回云端结果仍只查询原任务、不重新上传。未获得成功回执的云端 partial 不保存为成功版本。

重新转写追加版本，不覆盖原记录、音频或失败证据。卡片展示最新转写/整理/手动修订，复制原文取最新转写的 BasedOn；再次失败不删除旧版本。恢复云端成功正文时按原整理开关或翻译语言展示正文。

服务配套补丁见 `service-patches/unlimited-recording.patch`。配置 `max_audio_seconds=0` 关闭录音总时长限制；流式计算音频哈希避免服务累计完整 PCM，保留每包校验、15 秒空闲超时、60 秒结束处理超时及并发限制。任务每30秒续租90秒，崩溃仍可回收。HTTP文件上传另保留64 MiB传输大小保护；流式录音不设总字节上限。上游服务故障仍可失败，不保证无限时长的网络连接。

复验：先冻结 `artifacts/history-long-recording-20261025/PLAN.md`，运行同目录 `acceptance.py`，隔离数据库和合成上游执行240秒音频的 Controller 重试、回执、磁盘音频与账本验证；不将合成上游当作真实语音质量验收。
