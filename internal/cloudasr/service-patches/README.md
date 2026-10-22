# 服务补丁与版本边界

当前线上为 `voice-1.4.1`，使用本文末尾的 1.1 → 1.4.1 稳定翻译补丁；1.3/1.4 实验仍未发布。部署及验收见[翻译发布状态](../../../docs/public/TRANSLATION.md#部署与发布状态2026-10-23)。

## 组合服务 1.3 候选补丁（未发布）

`voice-quality-v1.3.patch` 用于独立 `sub2api/extensions/voice` 服务源码。这里不包含配置、凭证、录音、客户转写或数据库。`source-hashes.json` 标明逐文件的基准和目标哈希；先核对已有源码，遇到不同版本须人工合并，不能覆盖运行环境。

```sh
cd /absolute/sub2api/extensions/voice
git apply --check /absolute/voice-input/internal/cloudasr/service-patches/voice-quality-v1.3.patch
git apply /absolute/voice-input/internal/cloudasr/service-patches/voice-quality-v1.3.patch
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /absolute/candidate/sub2api-voice .
```

本地源码基准已经包含尚未部署的角色边界提示词。部署必须同时使用补丁后的完整 `cleanup-prompt.txt`，不能只把相对旧生产提示词的片段拼上去。运行配置中的提示词文件路径也须指向经校验的候选。

变化：

- `text_options.hotwords` 进入摘要和任务幂等指纹；长度、空项及换行校验在建任务前执行。
- 词表传入豆包 `corpus.context` 并启用最终二遍识别，保留 DDC/ITN/标点；清理词表仅作为数据提示，不执行用户文本中的命令。
- 整理明确说话者立场、独立信息和位置归属，短指令不代答；最终格式使用同一词表，计费正文与返回正文保持一致。
- 云端整理前执行已确认、带作用域的规则；原始 ASR 不变，候选和学习来源不上传。
- 上游发送 PCM 使用会话独立、无缓冲的弱声增益；原音/任务摘要不变。
- 明确编号每项真实换行，保持原有分项边界；无编号单段保持单段，原有段落保留，不因长度或多个要求自动分组，仍保留限定、否定和末尾要求。
- 候选服务版本为 `voice-1.3.0`。正常构建不注册任何验收接口。

隔离验收可以加 `-tags voicebenchmark` 构建并使用单独 loopback 端口 38532；配置文件和提示词路径包含 `acceptance-quality`。此构建额外注册 `/acceptance/cleanup`，仍要求正常 API 鉴权，只用于对照真实整理。不要将该构建、配置或路径部署为正式服务。

服务候选验证通过后，先部署正式服务及提示词并检查旧客户端兼容和新词表摘要，再发布新客户端。未经服务更新就发布支持热词的客户端，会在有词典时触发摘要拒绝；这属于明确的版本依赖，不能静默忽略词典。部署须保留原二进制、提示词和配置的回退入口，不回滚已发生的账本交易。

验收与结果口径见[语音质量规格](../../../docs/public/VOICE-QUALITY.md)。

## 1.4 官方翻译增量（本地候选，未发布）

`official-translation-v1.4.patch` 在已应用 1.3 的本地源码之上增加官方翻译；`translation-source-hashes.json` 记录该增量的基准/目标哈希。不要直接套到旧生产源码。保留 1.3 补丁及其独立验收，核对文件哈希后顺序应用；不同基准须合并。

新增可选 `translation_language`、独立翻译提示词、完成回执目标语言、译文跳过中文后处理；目标纳入现有摘要和任务指纹。模型、单次调用和原子结算复用原路径，普通听写保持兼容。服务版本为 `voice-1.4.0`，无数据库迁移。目标支持与端到端验收见 [官方翻译](../README.md#官方翻译14-本地候选2026-10-22)。本次仅本地构建和隔离验收，不包含线上更新。

## 1.4.1 翻译独立发布范围（2026-10-23）

本轮选择仅发布翻译增量，保留线上 1.1 普通听写行为。`official-translation-v1.4.1-stable.patch` 直接应用于 1.1，配套 `translation-stable-source-hashes.json`；不要先应用 1.3 或 1.4 候选补丁。增量仅修改四个文件：目标语言选项/摘要、独立翻译提示词、回执目标及版本号，无数据库迁移。

ASR 上游代码、PCM 发送、普通整理提示词及确定性格式维持 1.1；不启用 1.3 弱声增益、热词二遍与确认纠错。客户端不发送实验词表和纠正规则。线上普通提示词必须原样保留并核对部署前后哈希；翻译不带自由润色规则或中文后处理。历史 1.3/1.4 实验及 NOT_MET 门槛仍保留，不能将本次翻译验收当成实验获准上线。
