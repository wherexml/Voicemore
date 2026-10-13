<p align="center"><img src="resources/icon.png" width="112" alt="Voicemore 图标"></p>
<h1 align="center">Voicemore</h1>
<p align="center">自然说话，文字落在光标所在处。</p>
<p align="center"><a href="README.md">English</a> · <a href="https://github.com/wherexml/Voicemore/releases/tag/v0.3.0">下载 Apple Silicon 版</a></p>

Voicemore 是原生 macOS 语音输入工具。按下 **Fn** 开始说话，再按一次 **Fn**，识别结果就会写入当前输入框。支持本地识别、云端服务，以及按需使用语言模型整理表达。

## 下载与安装

**需要 Apple Silicon（M 系列）芯片及 macOS 26.2 或更新系统。** 此安装包不支持 Intel Mac。最低系统版本由内置 MLX 运行库决定；已在 macOS 27.0 arm64 验证，其他符合最低要求的系统尚未逐一完成端到端验证。

1. 从 [v0.3.0 发布页](https://github.com/wherexml/Voicemore/releases/tag/v0.3.0)下载 **Voicemore-0.3.0-arm64.dmg**。
2. 打开 DMG，将 **Voicemore 拖到 Applications（应用程序）**。
3. 推出磁盘映像，从“应用程序”启动 Voicemore。
4. 在“系统设置 → 隐私与安全性”授予**麦克风、辅助功能、输入监控**权限；授予键盘相关权限后重启应用。
5. 在“设置 → 模型”管理本地语音模型，选择下载部署；显示“已就绪”后即可在文本编辑器中尝试听写。

**预览版说明：** v0.3.0 使用开发签名，尚未完成 Developer ID 发布签名和 Apple 公证。其他 Mac 首次打开可能被系统拦截。确认下载来源后，可使用系统提供的“隐私与安全性 → 仍要打开”单应用放行流程；不要全局关闭 Gatekeeper。DMG 已支持拖拽安装，但不代表已实现无提示的首次启动。

DMG **包含推理运行环境，不包含模型权重**。首次本地部署会下载约 **2.47 GB** 模型文件，并解压约 **0.65 GB** 运行环境。下载时需要联网，完成后本地识别可离线使用；也可复制已有的兼容模型。云端识别需要自行配置服务商账号、凭证并保持联网。

## 客户端更新

从 **0.3.0** 起，应用会在启动约 10 秒后及运行期间每小时检查 GitHub Release。发现新版时，首页和菜单栏悬停提示会显示版本；每个版本尝试发送一次系统通知，是否展示取决于 macOS 通知权限。

点击首页的 **一键更新并重启**，即可下载更新包、验证 Ed25519 签名、替换应用并自动重启。支持取消下载和失败重试；安装期间暂停听写，避免打断正在进行的听写或模型部署。设置、模型、历史及凭证均保留。应用需要安装在可写入的 Applications 位置，不能直接从只读 DMG 内更新。

**0.2.0 用户需要手动安装一次 0.3.0。** 后续发布必须包含签名更新包及清单，单独上传 DMG 不会触发客户端更新。详见[更新发布流程](docs/public/UPDATES.md)。

## 功能

- **当前输入位置听写：** 可配置快捷键与按一次切换/按住说话模式，提供录音反馈和转录悬浮条。
- **本地语音模型：** 内置管理 Qwen3-ASR-1.7B-8bit，使用独立推理进程，不依赖预先安装 oMLX。
- **云端识别：** 支持阿里云、火山引擎，使用自己的服务商账号。
- **可选文字整理：** 连接本地或兼容的云端语言模型，默认关闭。
- **历史与回放：** 查看转录、修改结果、回听保存的录音。
- **个人词典：** 支持热词、纠错规则和从修改中生成建议；建议需确认后生效，跨应用观察修改默认关闭。
- **原生界面：** Dock 与菜单栏入口，英文、简体中文、繁体中文界面。

写入效果受目标应用和系统权限影响。已验证 TextEdit 听写；部分编辑器、受保护输入框仍可能存在兼容性差异。若 Fn 同时触发系统动作，可把系统键盘设置的“按下 🌐 键时”改成“不执行任何操作”，或换用其他快捷键。

## 隐私与数据

本地识别仅向本机回环地址 `127.0.0.1:48132` 的推理进程发送音频。选择云端识别后，音频发送到对应服务商；开启云端文字整理后，转录文字发送到所配置的接口。

设置、历史、录音、词典、模型和加密凭证保存在：

```text
~/Library/Application Support/Voice Input/
```

保留旧目录名称是为了兼容改名前的安装。API 凭证使用本机 AES-GCM 加密存储；加密密钥也存于本机，并使用严格文件权限。这能避免普通明文暴露，但不能抵御以同一 macOS 用户身份运行的软件。默认保存录音，保留期限设置为 30 天，可在“设置 → 历史与隐私”调整。

## 从源码构建

需要运行 macOS 26.2+ 的 Apple Silicon Mac、Xcode Command Line Tools，以及 **Go 1.27.1+**。Go 依赖锁定在 `go.mod` / `go.sum`。

```sh
git clone https://github.com/wherexml/Voicemore.git
cd Voicemore
# 下载指定版本运行环境并验证 SHA-256。
./scripts/fetch-asr-runtime.sh
# 仅供本机使用的临时签名构建：
SIGNING_IDENTITY=- ./scripts/build-macos.sh
```

使用自己的签名证书时，将 `SIGNING_IDENTITY` 设置为完整证书名称。正式对外分发还需 Developer ID 签名和 Apple 公证；仓库不包含签名凭证。详见[打包与验收说明](docs/public/PACKAGING.md)。

运行环境以 Release 附件分发，不存入 Git。Python 依赖、模型版本和校验值位于 `internal/localasr/assets/`。运行环境压缩包保留第三方许可证；源码依赖与图标说明见[第三方声明](resources/licenses/README.md)。

## 当前状态

这是早期预览版。已在本机验证 DMG 结构、复制安装后的签名、arm64 架构、运行环境校验值和原生应用启动。跨 Mac 的 Gatekeeper／公证、Intel 支持及更广泛的应用兼容性尚未完成。

欢迎[提交问题](https://github.com/wherexml/Voicemore/issues)，附上 macOS 版本、芯片和复现步骤。分享日志前请移除录音、转录文字与 API 密钥。
