# DMG 安装窗口

## 布局与语言

采用已确认的深蓝黑声波背景与细线箭头。窗口为 768 × 512 pt，Voicemore 在左，真实 `/Applications` 链接在右，拖拽行为由 Finder 提供。背景使用 1x/2x TIFF，应用与文件夹图标不烘焙进图片。

用户已明确选择固定英文的普通拖拽安装，不再根据系统语言切换。删除旧版的本地化提示目录；磁盘可见项目只有 Voicemore.app 和 Applications。

名称与安装提示直接绘制为背景白字，无边框、无浅灰衬底。安装提示始终为单行：

> Drag Voicemore to Applications to install.

Finder 自身不支持把图片背景上的文件名改为白色，也不支持关闭文件名标签。保留真实文件名和 VoiceOver 可访问名称，原生标签使用有效的最小字号 10 pt，放在白字上方的深色阴影区域；不改名为不可见字符、不用假图标或应用别名代替真实应用。背景上的白字是视觉标签，原生标签并未从系统中删除。Finder 标题栏由系统绘制。

图标位置、字号与背景必须配套修改，并在真实挂载窗口检查。`grid_spacing` 不得超过 100、文字字号不得小于 10，否则当前 Finder 会丢弃图标视图设置。

## 构建

需要 macOS、Xcode Command Line Tools、Python 3.10+（或 uv 自动选择）与首次安装 Python 依赖时的网络。

```sh
./scripts/build-dmg.sh build/darwin-arm64/Voicemore.app /absolute/path/new-preview.dmg
./scripts/verify-dmg.sh /absolute/path/new-preview.dmg artifacts/dmg-check build/darwin-arm64/Voicemore.app
```

脚本只打包已有、签名验证通过的应用；不修改应用的 FinderInfo 或任何已封装资源，不改签名和权限。新输出路径必须不存在，临时磁盘与目录由打包工具清理。Python 依赖固定在 `scripts/dmg-requirements.txt`，隔离安装到忽略的 `build/dmg-tools/`。

本地 `build-macos.sh` 跳过 mygo 默认 DMG，调用统一布局入口并使用本机固定开发证书签名 DMG。正式 `finalize-distribution.py` 也调用此入口，之后仍必须执行 Developer ID 签名、公证、装订和验收；本地视觉预览不代表正式分发通过。

## 验收

1. 双击最终 DMG，确认自动打开窗口、背景、细箭头、两枚真实图标；无文字衬底，英文提示一行显示，无裁切和滚动条。
2. 检查白色视觉标签与原生黑色标签位置，避免重叠；保留截图作为真实效果证据。更改 Finder 布局/文字大小会偏离预置背景，验收以默认打开状态为准。
3. 弹出后重新挂载，确认背景仍可解析，Applications 符号链接仍指向 `/Applications`。
4. `verify-dmg.sh` 挂载只读镜像、复制应用到临时目录，验证签名、架构、版本、运行时校验和，输出 SHA-256 与 `layout.json`；可见项目必须只有应用和 Applications。
5. 第三个参数提供原始 app 时，对照镜像中的应用执行内容比较。正式公证包另需通过 `verify-distribution-content.py` 的票据和 Gatekeeper 检查；开发预览不放宽正式门禁。

可重复证据写入忽略的 `artifacts/`，不提交截图或开发 DMG。
