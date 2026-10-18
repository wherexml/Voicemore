# 云端语音识别

## sub2api 组合服务

模型设置可选 sub2api 的 `doubao-asr-2.0-cleanup`。默认入口 `https://47.94.244.74:8443`，可配置 HTTPS origin；原生 WebSocket 握手携带专用 API Key 与稳定任务 ID。凭证使用本机 AES-256-GCM 存储，不写入设置、历史或日志。

使用 `/v1/voice/stream`，等待 accepted 后按实时节奏发送 16kHz mono PCM16；不把 ASR 全量快照拼接成重复句。每次录音最多180秒，完成后等待服务端 ASR 2.0、DeepSeek Flash 整理及计费事务全部成功。组合模型服务端始终执行整理；客户端总开关可选择交付原文，基础格式和润色规则随每次录音发送到服务端执行。该服务暂不支持翻译、热词和单独重新整理，相关操作不回退其他上游。

整理开启时最终正文直接输入，关闭时输入 raw_text；均不经过客户端词典替换或第二次整理。服务端按本次设置执行基础格式后再计费，正文、计费字数与任务取回结果一致。关闭只选择客户端交付正文，服务端仍执行整理并按原协议计费。历史分别保留原始 ASR、CleanedText、VoiceTaskID、VoiceBilling（字数、USD费用、价格版本、usage ID）。网络响应丢失或提交状态未知时只按原任务 GET，不重新上传音频、不生成新 ID；失败记录的更多菜单可取回云端结果，供用户复制，不自动输入旧目标。主动“重新识别”是新的有偿任务；仅取回结果不收费。进程在历史落盘前异常退出时可能没有可见历史入口，不能据此承诺所有崩溃都能自动恢复。

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
