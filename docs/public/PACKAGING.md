# macOS packaging / macOS 打包

## Reproduce an Apple Silicon installer

```sh
./scripts/fetch-asr-runtime.sh
./scripts/build-macos.sh
# Prints the completed notarized candidate workspace.
python3 scripts/install-macos.py /absolute/candidate-workspace
```

Output: an isolated candidate workspace under `~/Library/Caches/voicemore-release/`, containing the final stapled app, notarized DMG and verification evidence. This command prepares only; publishing requires the explicit release entry point.
The disk image contains `Voicemore.app`, an `/Applications` shortcut, and a Finder icon layout. Drag the app onto that shortcut, eject the image, and launch from Applications.

The verification script mounts the finished DMG read-only, copies the app to a temporary installation directory, checks its signature and arm64 architecture, checks product/minimum-system metadata and the runtime payload hash (legacy bundles) or pinned on-demand manifest (new bundles), and writes repeatable evidence under `artifacts/dmg-verification/`. It then unmounts the image and removes only its temporary directories. It does not grant permissions, run the app, or access personal settings.

For native end-to-end acceptance, launch the copied app on an Apple Silicon Mac, grant the three documented permissions, deploy the local model, dictate into TextEdit, check history and replay, quit and relaunch. Test the downloaded/quarantined asset on a separate Mac before claiming Gatekeeper acceptance. Stable releases require automatic signature, notarization, ticket, Gatekeeper and exact final DMG checks. Repeat native/cross-Mac acceptance for relevant runtime, permissions, installation or updater changes; it is not mandatory for every routine release.

## Signing

Daily production builds use the same approved Developer ID identity as official updates, defined in `internal/distribution/policy.json`. Debug builds use `python3 scripts/build-development.py`, a distinct `Voicemore Dev.app` identity and data directory, with no stable update feed. The certificate and private key must already be available locally; no signing credentials are included. Missing identities stop the build. Ad-hoc signing is prohibited. A successful signature check does not mean Gatekeeper will accept a public download.

v0.3.3 remains an **unnotarized, development-signed preview**; its assets are not replaced. Starting with the v0.3.4 distribution workflow, stable releases require a usable Developer ID Application identity, hardened-runtime-compatible nested binaries, and a successful Apple notarization/stapling pass. The bundled Python runtime archive is opaque to ordinary app-bundle signing; its extracted executable/native modules must also be audited and signed for a future notarized release. Do not claim notarization based on an app-only signature.

## Runtime

The Release asset `voicemore-asr-runtime-arm64.tar.gz` is pinned by `internal/localasr/assets/runtime.sha256`. The download script verifies it before replacing the local archive. Build scripts never include model weights, personal credentials, transcripts or recordings.

To rebuild the runtime from a separately provisioned, compatible Python 3.11 environment:

```sh
./scripts/package-asr-runtime.sh /absolute/path/to/verified/venv
```

This copies the base Python installation and installed packages without changing the source environment, runs isolated import checks and updates the archive checksum. It is a packaging helper, not a bit-for-bit reproducible Python environment bootstrapper. Use `internal/localasr/assets/requirements.lock` to document the resolved packages and validate relocation/native deployment targets before publishing a new archive.

## 中文摘要

- 运行上述构建脚本得到 arm64 DMG，可拖到 Applications 安装。
- 运行验收脚本，会挂载成品并复制应用，校验签名、架构、最低系统版本和运行环境哈希，并保存报告。
- 当前版本需要 macOS 26.2+，已在 macOS 26.2 arm64 完成 v0.3.4 跨机验收，本地开发也在 macOS 27.0 arm64 验证。
- 正式发布默认要求 Developer ID 签名、Apple 公证与票据校验；开发调试包使用独立身份且不进入正式更新源。首次下载后的系统放行不等于签名校验通过。
- 不把私人验收材料、密钥、配置、录音和模型权重加入 Git。大型运行环境以 Release 附件提供。

## 实时字幕采集助手

开发与正式候选构建都通过 `scripts/subtitle_capture.py` 编译 `internal/subtitles/capture/main.swift`，仅在隔离源码副本中生成 `resources/subtitles/VoicemoreAudioTap`，随应用放入 `Contents/Resources/subtitles/`。编译目标为 arm64/macOS 14.2；这是助手 API 的最低版本，不降低主应用的系统要求。

助手使用本次应用的固定签名身份和 `<Bundle ID>.audiotap` 标识；正式候选启用 hardened runtime 与安全时间戳，开发版使用开发身份且不公证。打包后同时验证应用整体签名及助手存在和签名有效。`mygo.json` 提供 `NSAudioCaptureUsageDescription`。签名构建成功不能替代首次系统音频授权、真实采集或最终 DMG 公证校验。

助手不携带 sherpa、Qwen 或说话人模型；运行环境、缺失模型与进程回收验收见[实时字幕规格](LIVE-SUBTITLES.md#安装与运行开发验收)。构建脚本的变更本身不表示已发布或安装。

## Client updates

From 0.3.0, signed archives and a manifest support in-app updates. See [publishing and E2E acceptance](UPDATES.md). Development uses the pinned original v0.2.0 runtime asset on demand. Official candidate builds produce a separately signed/notarized runtime asset and embed its immutable URL, size and hash; the publisher uploads and verifies it alongside the app before exposing the release. New DMGs and update archives exclude runtime payloads.
