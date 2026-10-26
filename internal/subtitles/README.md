# 实时字幕实现入口

用户行为、失败场景、窗口交互、模型准备与原生验收见[实时字幕规格](../../docs/public/LIVE-SUBTITLES.md)。本模块只负责系统音频采集、识别与可选后台推理；界面、翻译调度和显式保存由应用层负责。

## 按改动定位

| 职责 | 实现 | 继续阅读 |
| --- | --- | --- |
| 系统音频 → PCM16 → 引擎事件 | `capture/main.swift`、`capture.go`、`session.go` | [实时链路](../../docs/public/LIVE-SUBTITLES.md#实时链路) |
| sherpa 流式 / Qwen 独立短段 worker | `sherpa.go`、`sherpa_stream.py`、`qwen.go` | 同上；不修改已安装 worker，不占用听写服务端口 |
| 定稿音频后台重识别 | `correct.go`、`qwen_correct.py`；应用层 `subtitles_correct.go` | 字幕规格的「后台 Qwen 校正」 |
| 同进程听写让路、进程组取消 | `priority.go`、`proc.go` | 字幕规格的「听写优先」 |
| 会话内说话人匹配 | `speakers.go`、`speaker_diarize.py`；应用层 `subtitles_speakers.go` | [说话人与保存](../../docs/public/LIVE-SUBTITLES.md#说话人标签与会话保存2026-10-25) |
| 原生浮窗 / 流式翻译 / 存档 | 根目录 `subtitles.go`、`subtitles_translate.go`、`subtitles_store.go` | 字幕规格的窗口、翻译与保存章节 |
| 偏好 / 构建 | `../config/subtitles.go`、根目录 `prefs_subtitles.go`、`scripts/subtitle_capture.py` | [配置](../config/README.md#实时字幕设置)、[打包](../../docs/public/PACKAGING.md#实时字幕采集助手) |

## 修改时保持的约束

- 事件使用会话号、片段号和修订号；暂停、关闭、切换引擎后丢弃迟到事件，停止时回收助手进程组。
- 首条字幕与实时翻译不等待校正或说话人加载；队列、音频环形缓冲与超时有界。模型缺失只关闭相应可选能力。
- Gate 只观察同进程听写。sherpa 与实时翻译继续，校正、说话人推理及后台重译让路；不能宣称跨进程保护或听写零延迟回归。
- 匿名说话人嵌入只在内存中。保存仅写显式选定的文字会话，不保存 PCM 或声纹；恢复暂停，不自动采集。
- 字幕翻译沿用所选文字服务，可能访问云端；不能把本地语音识别表述成整个字幕链路离线。听写私密应用规则不覆盖系统音频字幕，见[隐私范围](../../docs/public/LIVE-SUBTITLES.md#数据与隐私范围)。

## 复验入口

构建使用 `python3 scripts/build-development.py`。先使用 Dev 专属合成夹具验证窗口、编辑和保存，再按字幕规格进行固定音频配对回放及同进程真实听写。记录源码版本、模型哈希、音频哈希、开关、事件日志和逐条结论到忽略的 `artifacts/`；合成夹具不能替代真实识别、翻译或物理按键验收。

可重复系统音频采集及同一 WAV 的 Qwen/sherpa 串行对照工具见[原型 README](../../tools/system-audio-prototype/README.md)。原型是独立应用，只用于采集/回放分析，不是正式字幕助手的运行入口。模型与 Python 环境需要单独准备，不随源码提交。
