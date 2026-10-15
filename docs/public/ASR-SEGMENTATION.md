# 本地 ASR 分段与短尾提示防护

Qwen3-ASR 会将很短的残留音频误解成提示词续写。不能靠删除所有短结果修复，否则会丢失“有”等真实短回答。

- 30 秒为目标窗口：达到后，遇到至少 300 毫秒静音再提交；连续说话最多延至 32 秒，按 32 毫秒 VAD 帧对齐。Stop 仍提交剩余有效语音。
- 模型输入不足 1 秒时，保留识别，但禁用 hotwords 与离线实验 system_prompt。此门槛按裁剪后的模型输入时长计算，并非 VAD 净语音时长；不保证消除所有幻觉。
- 同样规则用于 partial 和 final；事件 `hints_suppressed` 记录是否抑制提示。`/health` 区分 `target_segment_s=30` 和 `max_segment_s=32`。
- 长段仍可能在硬上限切开；这不是无限等待句末。场景背景仅用于离线实验，未新增 WebSocket 接口参数。

## 可重复验收

先准备真实 PCM16 / 单声道 / 16 kHz WAV：一个跨越 30 秒的 #091 样本，以及短回答“有”的 #011 样本。词典文件为 JSON 字符串数组。对隔离的本地 worker 执行：

```sh
"$HOME/Library/Application Support/Voice Input/local-asr/runtime/bin/python3" scripts/verify-asr-short-tail.py \
  --url ws://127.0.0.1:48136/ws \
  --sample /absolute/path/091.wav --short /absolute/path/011.wav \
  --hotwords /absolute/path/hotwords.json --out artifacts/asr-short-tail/websocket
```

产物 `report.json` 保存输入 PCM 哈希、完整协议事件和检查结果；覆盖长段边界、短回答、静音、取消及取消后重录。取消场景验证连接中断后的服务恢复，不声称覆盖推理中途取消。该脚本不覆盖原生麦克风和粘贴。

本机 100 条 × A/B/C 回归保存在忽略的 `dataset/typeless/benchmark-100/local-context-ab/short-tail-fix/`，冻结词表、场景、原始音频与模型版本；`run.py` 运行真实模型，`metrics.py` 计算汇总，`verify.py` 校验哈希及完整性，`build_report.py` 更新上层并排报告。原始报告保存在 `original-report.html`。数据不上传 GitHub。

本轮 300 组完成，297 组有文本；#076 三组仍判无语音。#091 从 30.016 秒正文加 224 毫秒残段变为 30.240 秒单段，三组均不再回显词典或背景。与 Typeless 润色参考的字符差异率 A/B/C 从 28.22% / 21.03% / 21.86% 变为 20.74% / 20.88% / 20.81%；排除 #091 后为 20.28% / 20.42% / 20.35%。这些不是人工金标错误率，不能证明所有样本都改善，仍不支持默认开启通用场景提示。
