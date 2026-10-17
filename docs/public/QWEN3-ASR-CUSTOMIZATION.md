# Qwen3-ASR-1.7B 本地版自定义能力

核对日期：2026-10-15。本文整理模型、推理后端和 Voicemore 应用三个层次的能力，并区分已经接入的功能与可继续开发的能力。描述当前源码，不代表所有发布版本都已包含这些功能。

生命周期和句尾预览见 [LOCAL-ASR.md](LOCAL-ASR.md)，词典行为见 [DICTIONARY.md](DICTIONARY.md)，文字整理见 [独立整理模块](../../internal/cleanup/README.md)。本文不是云端 DashScope 参数手册，也不是“给语音模型写提示词就能任意改稿”的承诺。

## 1. 版本与核对依据

| 项目 | 本次核对值 |
| --- | --- |
| 基础模型 | `Qwen/Qwen3-ASR-1.7B` |
| 本地量化模型 | `mlx-community/Qwen3-ASR-1.7B-8bit` |
| 本项目模型 revision | `a8379a2e2f9e313c9292cdf1af4055ab56d50d55`，以 [模型清单](../../internal/localasr/assets/model-manifest.json) 为准 |
| 本地推理实现 | `mlx-audio==0.5.8`，`mlx==0.32.3`，见 [运行环境锁定文件](../../internal/localasr/assets/requirements.lock) |
| 官方代码核对版本 | `QwenLM/Qwen3-ASR@7c6daf77a2421100f5fb066495372c00129d39ff` |
| MLX 上游核对版本 | `Blaizzy/mlx-audio@dad2087f790dfe96b07d5aac1405db4b578b411f` |
| 本次本地 MLX 模型实现 SHA256 | `661e6e930842dcd0cd509a75964f4b0fcf4d935a52011062d2978f050bc6b38d`，与上述 MLX commit 对应文件一致 |
| 项目核对基线 | `9f48669c27bf079ca44d795fc1b99806e8e0d3df` 的相关源码；其他模块未提交修改不在本文范围内 |

参数、默认值与支持范围必须以所用版本的代码为准。尤其不要把官方 Python 包、原生 Transformers、vLLM、MLX 和云端 API 的同名参数混用。参考资料使用固定 commit 链接，便于后续复核。

## 2. 能力总览

“已接入”表示项目源码已经使用或提供相应入口；“底层支持”表示需要开发接入及验收，不表示当前设置页面可直接使用。

| 能力 | 所属层 | 当前状态与边界 |
| --- | --- | --- |
| 专有名词、人名、产品名、缩写提示 | 模型上下文 + MLX `hotwords` | 已接入个人词典；提示倾向，不保证命中 |
| 会话场景、领域背景、动态词表 | 官方 `context` / MLX `system_prompt` | 底层支持；应用尚无场景配置或自动读取前台内容的实现 |
| 指定语言或自动识别语言 | 模型 / 后端 | 本地 worker 按会话接受中文（默认）、自动、英语，见[识别语言](LOCAL-ASR.md#识别语言) |
| 中文方言、英文口音、中英混合 | 模型识别能力 | 模型宣称覆盖多种语言与方言；不是逐个方言都能传入的独立开关，也不是本项目已经逐一验收 |
| 标点、口头语、缩写格式 | 应用确定性规则 | 已接入五个独立选项，不依赖整理模型 |
| 按完整语义重写、去掉重复表达、润色 | 文字整理模型或专门训练 | 不属于已验证的 ASR 自定义指令能力 |
| 预览频率、句尾补全、思考停顿、音频窗口 | 应用识别服务 | 已实现；当前大部分为代码常量，并非用户可调选项 |
| 随机采样、重复惩罚、输出预算 | MLX 解码器 | 底层支持；worker 只显式设置部分参数 |
| 长音频、批处理、逐 token 输出 | 后端 | 有对应接口；应用采用单会话、有界音频窗口重解码 |
| 带前缀回退的音频流式识别 | 官方 vLLM wrapper | 官方支持，未接入本地 MLX worker |
| 字词时间戳、音文对齐 | 独立 ForcedAligner 模型 | 需额外加载和接入；不是当前识别服务自带的精确时间戳 |
| 用录音与标注文本微调 | 官方训练工具 | 可进行监督训练；当前应用没有训练入口或个人在线学习流程 |
| 替换模型精度、量化版本、后端 | 部署层 | 需要修改模型/运行环境清单并重新验证，不能只改显示名称 |
| 说话人分离、声纹识别、翻译、语音指令执行 | 额外系统能力 | 未在当前接入中实现，不应从“支持 ASR”推导这些能力 |

来源：[官方概览 Q1](#参考资料)、[官方推理 Q2](#参考资料)、[MLX 实现 M1](#参考资料)及本项目源码。

## 3. 词典、热词与场景上下文

### 3.1 参数如何对应

- 官方 `transcribe(audio, context=..., language=...)` 接受上下文文本；批处理可以提供逐音频的上下文列表。[Q2]
- MLX `generate(..., system_prompt=..., hotwords=[...])` 接受背景文本及词列表。`merge_hotwords` 会清理空词，用英文逗号连接，再追加到已有 `system_prompt` 后；没有单独的强制词表解码器。[M1][M2]
- 当前 worker 只传 `hotwords`，不额外传场景提示。词表来自本机词典，每次会话发送；不会因为上次识别过某个名字，就永久更新模型权重。

可以试验的背景是“正在讨论某产品，涉及以下术语”。应把它当识别线索，并用录音验证：提示过多或不相关时可能诱导错词。修改场景上下文不需要第二个整理模型，但“有参数入口”不等于对指令可靠服从。

### 3.2 本项目的限制

见 [词典实现](../../internal/dict/dict.go)、[worker](../../internal/localasr/assets/server.py)：

| 项目 | 当前规则 |
| --- | --- |
| 数量 | 最多 128 项，超限报错，不静默截断 |
| 总长度 | Go 客户端检查连接后字符串的 UTF-8 **字节数**不超过 4096；Python worker 检查字符数不超过 4096。中文场景以客户端更严格限制为准 |
| 分隔 | WebSocket query 的 `hotwords` 是逗号分隔；客户端拒绝词项内英文逗号；worker 还将中文逗号当分隔符，因此词项不宜含两种逗号 |
| 去重 | 词典按顺序提供唯一词项 |
| 强制纠正 | 已确认的词典纠正规则在转写之后执行，与模型热词提示是两条不同路径 |
| 权重、负向热词、词频 | 当前接口没有逐词权重、负向词表或发音词典参数 |
| 隐私 | 当前本地模式在本机使用词表；改用云端服务时另按该服务的词典发送规则处理 |

按应用或项目选择词表、临时词表、同音词消歧均可进一步开发；当前没有这些自动选择逻辑。不能未经用户授权把前台文档内容自动当作上下文上传。

## 4. 语言与识别范围

`language=None` 用于自动语言识别；指定语言会影响输出前缀，**不等于把音频翻译成该语言**。应用界面语言与 ASR 语言是独立概念，切换界面英文不会改变识别语言；识别语言在模型设置中单独选择，缺省仍为 `Chinese`。[Q2][M1]

官方语言枚举为：[Q3]

```text
Chinese, English, Cantonese, Arabic, German, French, Spanish,
Portuguese, Indonesian, Italian, Korean, Russian, Thai, Vietnamese,
Japanese, Turkish, Hindi, Malay, Dutch, Swedish, Danish, Finnish,
Polish, Czech, Filipino, Persian, Greek, Romanian, Hungarian, Macedonian
```

官方另列出 22 种中文方言/地区口音覆盖，包括吴语、闽南语及多个地区口音；详细列表见 [模型卡](https://huggingface.co/Qwen/Qwen3-ASR-1.7B)。这些识别范围不意味着可以把 `Fujian` 等任意地区名作为 `language` 参数。应使用支持的语言枚举；方言和中英混说需用真实音频单独验证。[Q1][Q3]

本地输入契约为单声道 16 kHz 音频；模型推理接收归一化浮点数组。应用 WebSocket 输入是 PCM16 little-endian。官方 wrapper 可以处理路径、URL、数据 URL 或 `(数组, 采样率)` 并归一化音频；本机 worker 未开放这些远程输入接口。[Q2][Q3]

## 5. MLX 推理自定义参数完整表

以下是已安装 MLX `Qwen3ASRModel.generate` 的显式参数。库默认值不等于 Voicemore 实际值。模型目录、量化和硬件配置属于加载阶段，见第 9 节。[M1]

| 参数 | MLX 默认 | 当前 worker | 用途及注意点 |
| --- | --- | --- | --- |
| `audio` | 必填 | 当前音频窗口的 NumPy 数组 | 路径、数组、MLX 数组或输入列表；应用只传单个窗口 |
| `language` | `None` | 会话设置，缺省 `Chinese` | 自动（`None`）、`Chinese` 或 `English`；非翻译开关 |
| `system_prompt` | `None` | 未显式传入 | 场景上下文，不保证服从格式指令 |
| `hotwords` | `None` | 会话个人词典 | 最终合并到上下文，不强制替换 |
| `max_tokens` | `8192` | `512` | 生成 token 上限，不是字数；太小会截断。一次 `generate` 内部多个分块共享预算 |
| `temperature` | `0.0` | `0` | 贪心解码；提高后更随机，不能当作准确率增强开关 |
| `top_p` | `1.0` | 使用默认 | 概率累计采样阈值；贪心路径下不应期待靠它改善文字 |
| `top_k` | `0` | 使用默认 | 候选采样截断，0 为不启用此限制 |
| `min_p` | `0.0` | 使用默认 | 相对概率筛选 |
| `min_tokens_to_keep` | `1` | 使用默认 | 采样过滤时保留的最少候选数 |
| `repetition_penalty` | `None` | 未启用 | 解码重复惩罚；可能损害真实重复、口吃或强调，不能用来无条件去口头语 |
| `repetition_context_size` | `100` | 使用默认 | 重复惩罚查看的 token 窗口；不等于保留多少秒音频 |
| `batch_size` | `1` | 使用默认 | 内部音频分块批量推理；输入音频列表本身按输入顺序独立处理，不代表当前 WebSocket 支持并发 |
| `prefill_step_size` | `2048` | 使用默认 | 提示预填充分块，主要影响内存与性能 |
| `chunk_duration` | `1200.0` 秒 | 使用默认，但应用先切到约 30 秒 | 库内长音频分块目标；不是当前实时预览间隔，也不是任意长度质量保证 |
| `min_chunk_duration` | `1.0` 秒 | 使用默认 | 长音频切分时的最小尾块策略；不是语音活动检测阈值 |
| `stream` | `False` | `False` | `True` 返回逐 token 的生成器，详见第 6 节 |
| `verbose` | `False` | `False` | 推理进度日志 |
| `**kwargs` | 额外参数 | 未传 | 当前实现会丢弃未使用参数；能传入而不报错不代表已经生效 |

更底层的 `stream_generate` 还接受自定义 `sampler` 和 `logits_processors`，可供解码研究使用；应用不暴露该入口。库级 `split_audio_into_chunks` 还有 `search_expand_sec=5.0`、`min_window_ms=100.0` 的低能量切点搜索配置，它们不是 `generate` 的显式配置项，不能靠任意同名 kwargs 假定传透。[M1]

**没有已确认的本地 `enable_punc`、`remove_filler`、`itn`、`speaker_diarization` 或“输出 JSON”开关。** 不要照搬其他 ASR 或云端 API 的参数；当前的格式自定义来自第 8 节的应用规则。

## 6. 三种“流式”与时间戳

### 6.1 应用当前的实时预览

音频持续进入本地 WebSocket；VAD 判断语音，worker 对目前累计的窗口反复调用普通 `generate`，发出可替换的 `partial`。停止或窗口到限时产生 `final`。这是增量重解码，不是因果音频流，也没有把之前的 decoder KV cache 当成持续音频记忆。[worker](../../internal/localasr/assets/server.py)

### 6.2 MLX 的 token 流

`generate(stream=True)` 转到 `stream_transcribe`，对已经给定的音频逐块、逐 token 输出；不接受音频输入列表。它不是“不断给这个调用追加麦克风 PCM”的状态接口。返回的 `start_time/end_time` 主要按 token 位置估计，**不能当作精确字词对齐**；应用目前也没有使用它。[M1]

### 6.3 官方 vLLM 音频流

官方 `init_streaming_state`、`streaming_transcribe`、`finish_streaming_transcribe` 提供另一路状态式接口。核对版本限制为 vLLM，不能直接用于当前 MLX worker；不支持批量流式或强制对齐时间戳。[Q2]

| 初始化参数 | 官方默认 | 意义 |
| --- | --- | --- |
| `context` | 空字符串 | 会话上下文 |
| `language` | `None` | 自动或指定语言 |
| `unfixed_chunk_num` | `2` | 最初若干块不把旧输出作为前缀 |
| `unfixed_token_num` | `5` | 使用旧输出前回退的 token 数 |
| `chunk_size_sec` | `2.0` | 触发下一次流式解码的音频块长度 |

这些回退参数不是当前应用 `MAX_SEGMENT`、VAD 静音长度或 MLX 采样参数的别名。

### 6.4 精确时间戳

需要另一个 `Qwen3-ForcedAligner-0.6B` 模型。官方 wrapper 初始化 `forced_aligner` 后，可在离线 `transcribe` 设置 `return_time_stamps=True`；仅提供已有文字与音频时也可做强制对齐。应用未部署此模型，也未暴露对齐接口。[Q2]

对齐语言范围比 ASR 小：中文、英文、粤语、法语、德语、意大利语、日语、韩语、葡萄牙语、俄语、西班牙语。官方介绍提到最长 5 分钟，但本次核对的 wrapper 工具常量 `MAX_FORCE_ALIGN_INPUT_SECONDS` 为 **180 秒**，调用路径会按实现分块。不能把宣传上限当作单次接口的实际切块配置。[Q1][Q3]

## 7. 应用层音频与性能策略

当前 [server.py](../../internal/localasr/assets/server.py) 的值如下。除词典外，这些并未作为 HTTP query 的自定义参数开放；调整需改代码、重新部署并进行真实音频回归。

| 项目 | 当前值 | 作用与风险 |
| --- | --- | --- |
| 采样率 `SR` | 16000 Hz | 模型及 VAD 输入契约 |
| 周期预览 `STEP` | 0.6 秒 | 调度频率，不是保证 0.6 秒内完成推理 |
| 句尾补充预览 | 4800 samples / 300 ms 静音 | 每次停顿额外更新一次尾部，不定稿 |
| 最长窗口 `MAX_SEGMENT` | 30 秒 | 控制上下文与计算量；边界仍可能切开句子，512-sample 帧有离散粒度 |
| 定稿条件 | Stop 或窗口到限 | 不再因普通静音马上切句；`/health` 中 `silence_s=null` |
| 最少有效语音 | 1536 samples / 96 ms | 过滤纯静音及短噪声，不是声纹识别 |
| VAD 语音阈值 | 0.5 | 越低可能误收噪声，越高可能漏掉轻声 |
| 尾部保留 | 2048 samples / 128 ms | 避免裁掉尾音；裁剪本身也可能改变识别 |
| 前置保留 | 2 秒 | 缓解语音活动检测较晚才触发造成的开头裁剪 |
| 推理并发 | 1 个 executor worker / 单会话 | 忙时拒绝第二个 WebSocket 会话 |
| 排队上限 | 最多 8 个待定稿任务 | 超限报告积压错误；预览只保留最新候选 |
| 空闲接收超时 | 120 秒未收到消息 | 网络/会话保护，不等于 120 秒静音自动断句 |
| 会话长度保护 | 约 1 小时 | 音频接收路径中的时长保护，非模型硬上限 |
| 运行时加载 | 按需加载，任务结束后空闲 5 分钟释放 | 由 manager 管理，详见生命周期文档 |

30 秒是应用选择，官方/MLX 工具的长音频切块可达 1200 秒，两者并不冲突。更大窗口可能减少人为切句，但增加重解码开销；更短窗口可能降低延迟却损失语义上下文。调整时应一起衡量首字时间、停止到定稿、内存、漏字、重复和标点，不只看一条样例。

## 8. 已接入的格式自定义及其边界

实现：[textstyle](../../internal/textstyle/style.go)、[配置](../../internal/config/config.go)、[会话控制器](../../internal/session/controller.go)。五个选项默认开启，可在“设置 → 通用 → 基础格式”分别关闭。

| 配置字段 | 行为 | 不做什么 |
| --- | --- | --- |
| `textStyle.arabicNumbers` | 明确数字优先使用阿拉伯数字，如二零二六年 → 2026年 | 保留歧义简称、普通词语及受保护片段；详见[基础格式规格](TEXT-FORMATTING.md) |
| `textStyle.trimPeriod` | 省略整段末尾普通句号 | 不移除问号、省略号，不修复句中全部碎句 |
| `textStyle.calmExclamations` | 普通叙述中的感叹号改成句号，与尾句号选项协同 | 不尝试推断情绪，不修改受保护片段 |
| `textStyle.removeFillers` | 删除有边界的嗯／呃／啊及部分句尾啊 | 不全局删除这些汉字，不清理所有无边界填充词或任意重复表达 |
| `textStyle.joinAcronyms` | 合并白名单和词典中确认的分读大写字母，保留规范拼写 | 不把未知字母序列或跨行文本一律拼接 |

保护范围按实现为代码片段、指定引号形式和 HTTP(S) URL，不能扩大解释成支持所有标记语言或所有引号样式。格式规则不会理解句子含义，不能替代语义整理。

实际顺序：ASR 原文 → 已确认词典纠正 → 基础格式 → 可选文字整理 → 再应用基础格式。整理关闭时基础格式仍可生效。实时定稿与历史重新转写使用共享格式路径；原始转写保留，空格式结果有显式标记，不退回粘贴原始填充词。

“修改历史文字”“接受纠正规则”“热词提示”“微调模型”是不同操作。前两项不意味着模型权重发生变化。

## 9. 模型加载、量化、后端与训练

### 9.1 部署可选项

官方提供 Transformers 和 vLLM 路径；Transformers 加载时可设置设备、dtype、attention 实现等，vLLM 有其自身服务配置。它们需要对应运行环境，不能当作当前 MLX `generate` 的参数。[Q2]

当前应用使用 Apple Silicon MLX、固定 8-bit 模型清单和独立 Python 运行环境。改用其他量化精度、非量化权重或 0.6B 型号，需验证模型格式、tokenizer、依赖、内存、识别准确率、下载完整性和打包流程。`asrModel` 显示名称不是自动换权重的入口。应用使用离线加载约束和模型文件校验；详见 [模型管理器](../../internal/localasr/manager.go)。

### 9.2 监督微调

官方提供音频—文本配对的 SFT 脚本；数据使用 JSONL 的 `audio` 与 `text` 字段，可包含语言前缀。训练、验证数据应分开，用独立录音衡量泛化；不能只验证被训练过的句子。[Q4]

核对脚本提供的配置组如下：[Q5]

| 配置组 | 参数 |
| --- | --- |
| 输入输出 | `model_path`、`train_file`、`eval_file`、`output_dir`、`sr`（默认 16000） |
| 优化 | `batch_size`（32）、`grad_acc`（4）、`lr`（2e-5）、`epochs`（1）、`lr_scheduler_type`（linear）、`warmup_ratio`（0.02） |
| 日志与保存 | `log_steps`（10）、`save_strategy`（steps）、`save_steps`（200）、`save_total_limit`（5） |
| 数据加载 | `num_workers`（4）、`pin_memory`（1）、`persistent_workers`（1）、`prefetch_factor`（2） |
| 续训 | `resume_from`、`resume` |

这些是脚本默认值，不是针对个人录音的推荐配方。官方示例采用 PyTorch/Transformers，提供单 GPU 与 `torchrun` 多 GPU 路径；本次核对脚本没有公开的 LoRA rank/alpha、冻结层或“只训练标点”CLI 选项。不能宣称当前 8-bit MLX 安装包支持直接微调，也没有“录一次自动学会”的能力。训练后若回到本地量化部署，还需转换、量化及独立回归。

个人化训练可能研究专有词、口音和转写风格，但收益、所需数据量、硬件成本尚未针对本项目验证。原始录音、校正文本及训练集属于用户数据，不应随公开文档或 Release 分发。

## 10. 接入和验收建议

| 改动 | 必须核查的失败模式 | 建议证据 |
| --- | --- | --- |
| 热词 / 上下文 | 同音替换错误、不相关词被强塞、超限、中文长度、旧会话词表残留 | 相同录音有/无上下文的原始输出、词表版本、错误率 |
| 自动语言 / 指定语言 | 中文、英文、混说、方言、静音；语言标签解析与正文丢失 | 每类录音、参数、原文及人工参考 |
| 采样 / 重复惩罚 | 重复真实词被误删、随机性增加、token 截断 | 同一录音多次输出、长度与耗时 |
| 预览 / 窗口 / VAD | 思考停顿、轻声、起始尾音、纯静音、30 秒跨界、长录音重复/漏字 | WAV 哈希、partial/final 事件、首字和定稿延迟 |
| 基础格式 | 引号/代码/URL、讨论语气词本身、未知缩写、空结果、全部关闭、重复执行 | 原文与结果并排、命中规则、保存重读结果 |
| 时间戳 / 对齐 | 多语言范围、音文不一致、长音频分块、把估算当精确值 | 人工时间基准与偏差，额外模型版本 |
| 换模型 / 微调 | 训练集过拟合、量化后退化、依赖变化、OOM、冷启动与长期运行 | 独立留出集、版本清单、准确率和资源报告 |

现有验收入口：

- [句尾预览脚本](../../scripts/verify-asr-preview-tail.py)：真实 WebSocket 的静音后预览补全。
- [停顿脚本](../../scripts/verify-asr-pauses.py)：短样本插入不同静音，注意人工切入音节会改变输入性质。
- [格式与历史验收命令](../../cmd/style-acceptance/main.go)：基础格式和历史录音管线。该命令当前依赖本机历史及 `artifacts/transcript-style/` 的私有夹具，不能直接作为全新克隆的通用 CI 测试。
- [生命周期验收](LOCAL-ASR.md#验收)：按需加载、取消、卸载、重载。

优先真实音频端到端回放，保存可复验的本地证据。测试设计先列失败模式；不要为文档补写事后单元测试。公开文档只写方法和范围，私人音频、历史文本、截图与报告留在 `artifacts/`。本次能力搜集只核对文档和代码，**没有重新运行模型、训练或语言覆盖基准**。

## 参考资料

以下来源均于 2026-10-15 核对。正文 `[Qn]` / `[Mn]` 指向这里的固定版本来源。

- **Q1**：[Qwen 官方概览与能力范围](https://github.com/QwenLM/Qwen3-ASR/blob/7c6daf77a2421100f5fb066495372c00129d39ff/README.md)，[1.7B 模型卡](https://huggingface.co/Qwen/Qwen3-ASR-1.7B)。
- **Q2**：[官方推理与音频流式接口](https://github.com/QwenLM/Qwen3-ASR/blob/7c6daf77a2421100f5fb066495372c00129d39ff/qwen_asr/inference/qwen3_asr.py)。
- **Q3**：[语言枚举、采样率、长音频与对齐分块常量](https://github.com/QwenLM/Qwen3-ASR/blob/7c6daf77a2421100f5fb066495372c00129d39ff/qwen_asr/inference/utils.py)。
- **Q4**：[官方微调说明](https://github.com/QwenLM/Qwen3-ASR/blob/7c6daf77a2421100f5fb066495372c00129d39ff/finetuning/README.md)。
- **Q5**：[官方微调脚本参数及训练实现](https://github.com/QwenLM/Qwen3-ASR/blob/7c6daf77a2421100f5fb066495372c00129d39ff/finetuning/qwen3_asr_sft.py)。
- **M1**：[MLX Qwen3-ASR 实现](https://github.com/Blaizzy/mlx-audio/blob/dad2087f790dfe96b07d5aac1405db4b578b411f/mlx_audio/stt/models/qwen3_asr/qwen3_asr.py)。
- **M2**：[MLX 热词与上下文合并](https://github.com/Blaizzy/mlx-audio/blob/dad2087f790dfe96b07d5aac1405db4b578b411f/mlx_audio/stt/utils.py)。
- **L1**：[应用 worker](../../internal/localasr/assets/server.py)、[语音活动检测](../../internal/localasr/assets/speech_gate.py)、[模型清单](../../internal/localasr/assets/model-manifest.json)、[运行环境版本](../../internal/localasr/assets/requirements.lock)。
- **L2**：[个人词典](../../internal/dict/dict.go)、[基础格式](../../internal/textstyle/style.go)、[配置](../../internal/config/config.go)、[会话控制器](../../internal/session/controller.go)。
