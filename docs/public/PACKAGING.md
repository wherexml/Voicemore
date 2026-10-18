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

## Client updates

From 0.3.0, signed archives and a manifest support in-app updates. See [publishing and E2E acceptance](UPDATES.md). Development uses the pinned original v0.2.0 runtime asset on demand. Official candidate builds produce a separately signed/notarized runtime asset and embed its immutable URL, size and hash; the publisher uploads and verifies it alongside the app before exposing the release. New DMGs and update archives exclude runtime payloads.
