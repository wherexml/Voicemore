# 本地语音模型生命周期

适用范围：应用管理的 Qwen3-ASR-1.7B-8bit。云端识别和自定义外部服务不受此策略影响。

自定义能力、参数默认值、后端差异与扩展边界见 [Qwen3-ASR 自定义能力](QWEN3-ASR-CUSTOMIZATION.md)。

## 行为

- 应用打开时，已安装模型处于待机，不自动加载；首次听写或历史录音重新识别时自动加载。模型页仍支持手动启动和停止。
- 模型就绪且没有识别任务时，空闲 5 分钟后终止推理进程，释放模型内存。模型文件和运行环境留在磁盘，下次无需下载。
- 从准备识别到流关闭期间保持模型驻留，覆盖录音、等待最终结果和历史重新识别。任务结束重新开始计时；连续使用不会重复加载。
- 听写先开启麦克风并缓存 PCM，再等待模型加载；加载期间说的话和按停止前采集的音频仍会送去识别。取消听写会停止等待，无人使用的后台加载完成后仍按空闲策略释放。
- 加载等待上限 150 秒，独立于连接服务的 10 秒超时。加载失败显示错误，不无限重试；可以在模型页修复或重新启动。
- 退出应用终止子进程并禁用空闲计时器。手动停止后，下次听写仍可自动加载。
- 实时预览在说话期间约每 0.6 秒调度；检测到连续约 300 毫秒静音后，额外调度一次包含句尾及 128 毫秒尾部上下文的预览，推理完成即更新。无需按停止键即可补齐最后一次周期更新之后的音频；静音期间不反复推理。该更新仍是可修订的预览，停顿不拆句，停止或达到分段窗口才提交最终结果。

## 识别语言

设置 → 模型 → 本地语音模型的“识别语言”：中文、自动识别、英语，使用标准选择器（[mock](../../../mock/language-privacy.html)）。配置 `asrLanguage` 缺失或为空即中文，旧配置和未选择的用户保持原行为；`auto`、`en` 仅作用于应用管理的本地 worker（`providers.Local`），自定义服务与云端识别的路由和参数不变。

- 实现前失败路径：旧配置被改成自动识别；worker 未更新却静默按中文识别；非法值进入模型；自动识别输出带 `language X<asr_text>` 前缀；重新转写仍用旧语言；切换后影响云端或自定义服务。
- 协议：WebSocket 查询参数 `language=zh|auto|en`，缺省为 `zh`（`Chinese`），未知值以 1008 关闭；`ready` 回显 `language`。`/health` 返回 `languages`，客户端在非中文时检查该列表，旧 worker 返回“需重新启动”而非按中文识别。`auto` 传 `language=None`，worker 去掉开头的语言标签。
- 实时听写与历史重新转写共用同一 Dialer；重新转写的版本说明记录所用语言。设置在下一次听写或重新转写生效。

质量边界：现有中文片段与合成英文拼接的混合样本，在自动模式仍遗漏中文；固定中文、英文样本与参数路由通过不等于混说准确率通过。保留原始输出并单列失败，不用客户端补写文字掩盖模型遗漏。

验收：分别用中文、英文、中英混说、静音真实录音，在三个选项下实时听写和重新转写；检查原文无语言标签、静音无文字、云端/自定义服务请求不含 `language`。旧配置重启后仍为中文。证据放本地 `artifacts/language-privacy/`。

## 验收

1. 已部署模型的 Mac 启动应用：模型显示待机，未启动推理进程。
2. 按快捷键立即说话，加载期间停止录音：最终文字包含开头，未要求再次按键。再次快速输入复用同一 PID。
3. 连续录音或重新识别超过空闲阈值：推理进程不退出。结束后等待 5 分钟：子进程消失；再次输入自动恢复并输出文本。
4. 加载中取消、停止或退出应用：界面可响应，无遗留识别会话；退出不遗留推理进程。
5. 切换云端识别：不启动本地模型，连接超时策略保持不变。
6. 说完一句话后保持录音、不按停止键：等待推理完成，预览应包含句尾。继续说话时仍可修订同一段；长时间静音不反复触发预览，纯静音不产生文字。

句尾预览自动验收使用真实模型及 WebSocket，保存 WAV、各次预览覆盖的音频时长、文字及最终结果。先启动待验收的本地服务，然后使用内置运行环境执行（将 URL、样本及输出目录替换为实际值）：

```sh
"$HOME/Library/Application Support/Voice Input/local-asr/runtime/bin/python3.11" \
  scripts/verify-asr-preview-tail.py --url ws://127.0.0.1:48132/ws \
  --sample artifacts/smooth-onset/pipeline/sample.wav --out artifacts/asr-preview-tail/installed
```

可重复运行实际运行环境、HTTP、WebSocket 的自动验收（将空闲阈值缩短至 2 秒；使用独立目录和端口 48134）：

```sh
mkdir -p artifacts/asr-idle
go build -race -o artifacts/asr-idle/acceptance ./cmd/asr-idle-acceptance
codesign --force --sign 'Apple Development: geekmooc@gmail.com (5QLDC77G39)' artifacts/asr-idle/acceptance
artifacts/asr-idle/acceptance \
  -source "$HOME/Library/Application Support/Voice Input/local-asr" \
  -sample artifacts/smooth-onset/pipeline/sample.wav \
  -out artifacts/asr-idle
```

输入必须是 16 kHz、单声道 PCM16 WAV。报告记录冷启动、热启动、卸载后重载耗时、RSS、实际转写、取消和进程释放；报告和 worker 日志保存在本地 artifacts，不入库。此自动验收不替代原生快捷键、麦克风与界面验收。

- 分段更新：30 秒目标 / 32 秒硬上限与短尾提示防护见 [分段规格](ASR-SEGMENTATION.md)。

### 语言模式的验收边界

固定真实音频验证了中文默认、英文与自动识别英文，生产历史重新转写也分别通过。自动模式表示不强制语言，不保证每段中英混说都完整：本地双语拼接样本在自动、中文、英文模式下均曾漏掉中文部分，原始模型输出已复核。此结果是模型质量限制，不能用语言选择器已通过代替混说准确率验收。复跑音频、逐条输出与原始模型诊断保存在本地 `artifacts/language-privacy/`。
