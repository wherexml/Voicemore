# macOS packaging / macOS 打包

## Reproduce an Apple Silicon installer

```sh
./scripts/fetch-asr-runtime.sh
SIGNING_IDENTITY='Your full signing identity' ./scripts/build-macos.sh
./scripts/verify-dmg.sh 'build/darwin-arm64/Voicemore 0.2.0 arm64.dmg'
```

Output: `build/darwin-arm64/Voicemore 0.2.0 arm64.dmg` and `SHA256SUMS.txt`.
The disk image contains `Voicemore.app`, an `/Applications` shortcut, and a Finder icon layout. Drag the app onto that shortcut, eject the image, and launch from Applications.

The verification script mounts the finished DMG read-only, copies the app to a temporary installation directory, checks its signature and arm64 architecture, checks product/minimum-system metadata and the bundled runtime hash, and writes repeatable evidence under `artifacts/dmg-verification/`. It then unmounts the image and removes only its temporary directories. It does not grant permissions, run the app, or access personal settings.

For native end-to-end acceptance, launch the copied app on an Apple Silicon Mac, grant the three documented permissions, deploy the local model, dictate into TextEdit, check history and replay, quit and relaunch. Test the downloaded/quarantined asset on a separate Mac before claiming Gatekeeper acceptance. v0.2.0 has only local native launch validation, not cross-Mac notarized distribution validation.

## Signing

The repository deliberately contains no personal signing identity or notarization credentials. `SIGNING_IDENTITY=-` is an explicit local-only ad-hoc build; a successful signature check does not mean Gatekeeper will accept a public download.

v0.2.0 is an **unnotarized, development-signed preview**. Production distribution needs a usable Developer ID Application identity, hardened-runtime-compatible nested binaries, and a successful Apple notarization/stapling pass. The bundled Python runtime archive is opaque to ordinary app-bundle signing; its extracted executable/native modules must also be audited and signed for a future notarized release. Do not claim notarization based on an app-only signature.

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
- 当前版本需要 macOS 26.2+，仅在 macOS 27.0 arm64 完成当前本机验收。
- 当前为开发签名预览版，未完成 Developer ID 签名与 Apple 公证；首次下载后的系统放行不等于签名校验通过。
- 不把私人验收材料、密钥、配置、录音和模型权重加入 Git。大型运行环境以 Release 附件提供。
