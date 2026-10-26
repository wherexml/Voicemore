# 系统音频采集 + 中文实时识别对比原型

独立开发原型，不属于正式应用。目标：在 macOS 14.2+ 用 Core Audio process tap 采集系统输出（不是麦克风），得到同一份 WAV 后，按真实时间节奏分别回放给 (A) 现有 Qwen 托管 worker 协议和 (B) sherpa-onnx 流式中文/双语 Zipformer，对比字幕可用性。

内容：采集助手及其独立构建脚本（`capture/`、`build-capture.sh`、`run-capture.sh`），成对回放与报告（`asr/`、`run-pair.sh`）。

## 隔离边界

- 身份：开发签名使用 `internal/distribution/policy.json` 的 `debugIdentity`（Apple Development 证书）；Bundle ID `dev.steve.voice-input.debug.sysaudio-proto`、名称 `Voicemore SysAudio Proto`，与正式 `dev.steve.voice-input` 和 `Voicemore Dev`（`dev.steve.voice-input.debug`）都不同；构建脚本遇到冲突直接拒绝。无更新源、无应用数据目录、不覆盖 `Voicemore.app` / `Voicemore Dev.app`。
- 输出：构建产物在 `build/system-audio-prototype/`，采集证据默认在 `artifacts/system-audio-prototype/`（均不入库）。
- 不修改正式应用、worker、设置、mock 或依赖；不安装虚拟声卡；不更改系统默认输出设备；采集音频不上传云端。
- 权限：首次启动会出现系统音频录制授权提示。不得修改 TCC 数据库或用重置权限代替身份核对（见本机签名约定）。

## 失败场景

说明：以下清单与第一版采集代码在同一轮编写，并非在实现前单独冻结；后续修改以它为验收依据。

采集
1. 授权被拒或未弹出：tap 能创建，但只收到全零缓冲；必须明确报告，不能输出看似正常的静音 WAV。
2. 授权归属错误：直接在终端运行可执行文件时，授权可能记到 Terminal；必须经 LaunchServices（`open`）以原型 bundle 运行，并在日志中记录实际 bundle ID。
3. 系统版本低于 14.2 或 `AudioHardwareCreateProcessTap` 失败：给出 OSStatus（含四字符码）并以非零码退出，且清理已创建对象。
4. 聚合设备不运行（3 秒无回调）：报告 `no_callbacks`，退出码 4。
5. 采集改变了用户听到的声音：tap 必须 `unmuted`，播放保持可闻；不切换输出设备。
6. 采到麦克风：聚合设备默认只含 tap；把输出设备列为子设备时，AirPods 等的麦克风输入流也会被打开并可能切到通话音质——该模式仅作回退选项，并在日志中记录缓冲布局。
7. 采到助手自身声音：全局模式排除本进程。
8. 指定 PID 不存在或尚未打开音频：明确报错，而不是退回全局采集。
9. 采样率/声道转换错误：源格式写入日志，统一转为 16 kHz 单声道 PCM16；时长按目标帧数精确截断，不因回调粒度多写或少写。
10. 时钟漂移或设备时钟停滞：记录 host time 与源/目标帧数；超出时长 5 秒仍未结束则强制停止并标记 `wall_timeout`。
11. 中断（Ctrl-C / SIGTERM）后 WAV 头未回填：信号处理须回填 RIFF/data 长度，标记 `partial`。
12. 中途切换输出设备（如插拔耳机）：原型不处理热切换，只要求日志可看出回调中断或静音段；验收时单列。

回放与识别
13. 回放速度不公平：必须按音频时长节奏发送；两引擎串行运行，不并行争用 GPU/CPU。
14. 把库名中的“streaming”当作真实流式：以实际收到 partial 的时间和音频位置为准。
15. 基线改动：Qwen 基线保持现有 30/32 秒定段；字幕短段变体必须单独命名，不修改已安装 worker。
16. 结果缺失：停止后的 flush、最终段、错误必须记录；空结果不得算作“零错误”。
17. 指标误读：CER 只表示该段人工参考、该归一化规则下的字符错误率；1−CER 不是通用准确率；无时间戳参考时句末延迟标记为不可用。

## 端到端验收计划

1. 在 Mac 上执行构建（下方命令），确认 `codesign -dv` 输出的 Identifier 为原型 ID、Authority 为 debugIdentity。
2. 播放一段可公开使用、含中文讲话的音频或视频（浏览器或播放器），音量正常可闻。
3. 运行 `run-capture.sh`，首次运行在系统提示中允许原型应用录制系统音频；采集期间确认仍能正常听到播放。
4. 检查：`capture.jsonl` 末行 `summary.status == "ok"`、`exit_code == 0`、`frames == duration × 16000`、`nonzero_samples > 0`；`afinfo` 显示 16000 Hz、1 声道、16 位；人工试听 WAV 无明显断续或变速。
5. 负面用例：(a) 不播放任何声音采集 10 秒，应出现 `zero_audio_warning` 且 `status == "failed_zero_audio"`、退出码 3；(b) 在系统设置中关闭该应用的系统音频录制后重复 (a) 的播放场景，应同样报告全零；(c) `--pid` 指定一个未发声的进程，应在启动时报错；(d) 采集中 Ctrl-C/`kill -TERM`，WAV 仍可播放且标记 `partial`。
6. AirPods 场景：默认模式采集时 AirPods 不应切到通话音质；记录是否发生。
7. 保存同一份 WAV 的 SHA-256，后续两引擎回放都引用该哈希。
8. 证据（JSONL、WAV 哈希、afinfo 输出、主观试听结论、失败用例结果）由协调者保存到 `artifacts/system-audio-prototype/`。本 README 不记录任何未执行的通过结论。

## 构建与采集命令（Mac 上执行）

```sh
# 构建并签名（需要钥匙串中的 debugIdentity 证书）
tools/system-audio-prototype/build-capture.sh

# 采集 60 秒系统音频（全局，排除助手自身）
tools/system-audio-prototype/run-capture.sh "$PWD/artifacts/system-audio-prototype/capture-01" 60

# 只采某个进程（例如浏览器渲染进程或播放器 PID，可重复 --pid）
tools/system-audio-prototype/run-capture.sh "$PWD/artifacts/system-audio-prototype/capture-pid" 60 --pid 12345

# 默认聚合设备无回调时的回退（会打开输出设备的输入流，见失败场景 6）
tools/system-audio-prototype/run-capture.sh "$PWD/artifacts/system-audio-prototype/capture-fallback" 30 --with-output-subdevice
```

助手参数：`--out /abs.wav`（必填）、`--log /abs.jsonl`、`--duration 1…600`（默认 60）、`--pid N`（可重复）、`--zero-warn 秒`（默认 5）、`--with-output-subdevice`。路径必须是绝对路径。退出码：0 成功（含 `partial`）、2 参数/初始化/写文件错误、3 全零音频、4 无回调。`open -W` 不转发退出码，以 JSONL `summary` 为准；`helper.stderr.log` 同时镜像关键事件。

## JSONL 时间线

每行一个对象，均含 `event`、`t_mono_ns`（`CLOCK_UPTIME_RAW`，与 `AudioTimeStamp.mHostTime` 同一时基，换算为纳秒）和 `t_wall`。主要事件：`start`（bundle ID、系统版本、参数、是否含用途说明）、`tap_scope`、`process_object`、`tap_created`、`tap_format`（源采样率/声道/格式）、`aggregate_created`、`io_started`、`first_buffer`（缓冲布局、host time）、`first_nonzero_audio`、每秒 `progress`（目标帧数、源帧数、回调数、host time、1 秒峰值 dBFS、非零比例）、`zero_audio_warning`、`no_callbacks`、`error`、`stopping`、`summary`（帧数、时长、状态、峰值、WAV SHA-256、退出码）。

## 已知限制

- 仅在 macOS 14.2+ 有效；Fedora Claude 编写代码，远端未运行构建或测试。2026-10-24 协调者已在本机 macOS 27.0 / M3 Ultra 完成开发签名构建、70 秒系统音频采集和六次真实模型回放；其他系统与设备组合未验证。本地证据位于 `artifacts/system-audio-prototype-20261024/assessment.md`。
- 系统没有公开的授权预检接口；拒绝授权只能通过全零缓冲推断，与“确实没有声音”无法区分，因此报告为同一失败并附提示。
- 默认 tap-only 聚合设备依赖 tap 自身时钟与漂移补偿；若某些系统上无回调，使用 `--with-output-subdevice` 回退并留意失败场景 6。
- 格式转换在调度队列中分配缓冲，非严格实时安全；60 秒原型足够，长时间采集需另行评估。
- 不处理输出设备热切换、多输出设备、受 DRM 保护而静音的内容。
- 开发签名、无强化运行时、未公证，仅限本机原型。

## 成对回放与报告

文件：`asr/common.py`（WAV、哈希、JSONL、节奏）、`asr/replay_qwen.py`（引擎 A）、`asr/replay_sherpa.py`（引擎 B）、`asr/report.py`、`run-pair.sh`。使用协调者已建的独立 venv（`artifacts/system-audio-prototype-20261024/venv`：sherpa-onnx 2.0.0、numpy 2.4.6、websockets 17.3）和已下载的官方 `sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20`（encoder/joiner INT8、decoder FP32、tokens.txt）。本仓库不安装、不下载。

- 公平性：同一 WAV、100 ms 块、按绝对时间表实时发送；引擎串行，间隔冷却（默认 10 秒）；不并行占用 GPU/CPU。
- 引擎 A（Qwen）：以 `~/Library/Application Support/Voice Input/local-asr` 的 `runtime/bin/python3.11 -I worker/run.py` 启动独立子进程，端口 48138（拒绝 48132 或已占用端口），`VOICE_INPUT_PARENT` 为回放进程，离线 HF 环境；模型与 worker 源只读，记录 server.py/run.py/speech_gate.py 与模型文件 SHA-256。协议：`ready` → PCM16 二进制块 → `{"type":"stop"}` → `partial`/`final`/`done`/`error`。`baseline` 保持已安装 worker 的 30/32 秒定段。`--variant subtitle-short` 把已安装 worker 目录复制到输出目录旁，仅替换 `MAX_SEGMENT=30`、`MAX_SEGMENT_EXTENSION=2`（默认 8/1，匹配不到即拒绝），不修改已安装文件。
- 引擎 B（sherpa-onnx）：CPU、贪心解码、端点规则 2.4/1.2/20 秒；每块后只在 `is_ready()` 时解码，文字变化记 partial，端点记 final；结束补 0.66 秒静音后 `input_finished()` 并冲刷。是否“流式”以实际 partial 时间判断；解码跟不上时发送端时间表滞后（`schedule_lag_s`）即实时积压。
- 运行日志（JSONL）：首行 `run`（引擎、参数、WAV 哈希、模型文件哈希、版本、主机）；每块 `sent`（已提交音频位置、时间表滞后）；`partial`/`final`（段 ID、文字、接收时刻 `t_s`、当时已提交音频 `audio_sent_s`、Qwen 的 `decode_s` 与段音频长度）；`stop`、`done`、`error`。`audio_sent_s` 是提交位置，不是词语实际发声时刻。
- 报告：`report.md` + `report.json`。CER 使用透明归一化（NFKC、小写、非 ASCII 字母数字逐字、ASCII 词整体、去标点空白，不做数字/繁简转换），给出 S/D/I/命中数；只对该参考与该规则有效，不报告 1−CER。延迟分三类分别给出：音频相对端到端滞后（首结果相对句首提交、定稿相对句末提交）、推理时间/推理 RTF、块时长（100 ms）。参考无时间轴（纯文本）时句级延迟标为 unavailable。另给首个输出时刻及当时已提交音频、stop→done 冲刷、音频结束后剩余时间、发送端最大滞后。参考 SRT 须对齐到采集 WAV 时间轴，可用 `--ref-offset` 平移；参考的人工核对由操作者负责。

```sh
# 一次完成：Qwen 基线 → sherpa → 报告（加 --with-short 插入 Qwen 字幕短段变体）
tools/system-audio-prototype/run-pair.sh "$PWD/artifacts/system-audio-prototype/capture-01/capture.wav" \
  "$PWD/artifacts/system-audio-prototype/capture-01/reference.srt" \
  "$PWD/artifacts/system-audio-prototype/pair-01" --with-short

# 单独运行
V=artifacts/system-audio-prototype-20261024/venv/bin/python
$V tools/system-audio-prototype/asr/replay_qwen.py --wav /abs/capture.wav --out /abs/pair/qwen-baseline.jsonl --variant baseline --port 48138
$V tools/system-audio-prototype/asr/replay_qwen.py --wav /abs/capture.wav --out /abs/pair/qwen-subtitle-short.jsonl --variant subtitle-short --short-target-s 8 --short-extension-s 1
$V tools/system-audio-prototype/asr/replay_sherpa.py --wav /abs/capture.wav --out /abs/pair/sherpa-zipformer.jsonl \
  --model-dir "$PWD/artifacts/system-audio-prototype-20261024/models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
$V tools/system-audio-prototype/asr/report.py --reference /abs/reference.srt --out-dir /abs/pair /abs/pair/*.jsonl
```

回放限制：Qwen 推理时间为 worker 报告的每次解码 `decode_s` 之和（含重复的整句重解码），sherpa 为运行进程内 `decode_stream()` 耗时之和，口径不同须分开解读；首结果延迟假设 partial 前缀稳定；单次采集、单份参考、单机串行，不代表统计显著性；sherpa 双语模型输出英文大写且无标点，标点不计入 CER。
