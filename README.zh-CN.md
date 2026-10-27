# Voicemore

原生 macOS 语音输入工具。按下 **Fn** 开始说话，再按一次，文字写入当前输入框。

[English](README.md) · [下载](https://github.com/wherexml/Voicemore/releases/latest) · [更新记录](https://github.com/wherexml/Voicemore/releases) · [反馈问题](https://github.com/wherexml/Voicemore/issues)

## 安装

需要 **Apple Silicon 芯片和 macOS 26.2 或更新系统**。

1. 从最新发布页下载 DMG，将 **Voicemore** 拖入 **Applications**。
2. 启动应用，授予麦克风、辅助功能和输入监控权限。
3. 在设置中选择本地语音识别，或配置云端服务。

本地识别首次配置时下载运行环境与模型，完成后可离线使用。云端服务需要联网并配置服务商凭证。正式版本使用 Developer ID 签名和 Apple 公证，支持应用内签名更新。

## 功能

- 在当前应用输入文字，可配置快捷键和自动发送。
- 本地或云端语音识别，可选文字整理和翻译。
- 转录历史、录音回放和个人词典。
- 私密应用听写不保存本次文字、录音及纠错学习。
- 英文、简体中文和繁体中文界面。

写入效果受 macOS 权限和目标应用兼容性影响。源码可能包含尚未发布的功能，见 [CHANGELOG.md](CHANGELOG.md)。

## 隐私

本地识别在 Mac 上处理音频。云端识别将音频发送至所选服务商；云端文字整理将转录发送至所配置的服务。

设置、历史、录音、模型及加密凭证保存在 `~/Library/Application Support/Voice Input/`。默认保存录音，保留期限设置为 30 天，可在设置中调整。凭证加密使用本机密钥，不能抵御以同一 macOS 用户身份运行的软件。

## 开发

构建依赖、命令和校验入口见 [BUILDING.md](BUILDING.md)。本仓库包含 macOS 客户端及构建、验收工具；官网源码、内部设计资料、服务端运维补丁和实验原型另行维护。

依赖与素材来源见[第三方声明](resources/licenses/README.md)。反馈问题时请附上 macOS 版本、芯片和复现步骤，分享日志前移除凭证及私人转录。
