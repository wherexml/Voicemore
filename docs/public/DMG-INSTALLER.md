# DMG 安装窗口

## 布局与语言

采用已确认的深蓝黑声波背景与细线箭头。窗口为 768 × 512 pt，Voicemore 在左，真实 `/Applications` 链接在右，拖拽行为由 Finder 提供。背景使用 1x/2x TIFF，图标不烘焙进图片。

图片背景下 Finder 将文件名绘制为黑色，不能依赖系统深色模式改变颜色。因此名称与提示使用浅灰衬底，这是相对 mock 白字的原生适配。标题栏由系统绘制，不伪造深色标题栏。

底部安装提示不是固定图片文字，也不是中英双语常显。它使用 Apple 的 `.localized` 目录名称机制、透明自定义图标，由当前 Finder 会话的语言偏好选择：

| 语言 | 提示 |
| --- | --- |
| 简体中文 | 将 Voicemore 拖入应用程序即可安装 |
| 繁体中文 | 將 Voicemore 拖入應用程式即可安裝 |
| 英文 / 未覆盖语言 | Drag Voicemore to Applications to install |

该提示在磁盘上是仅含隐藏本地化资源的目录；它不是按钮，也不执行安装程序。双击提示会打开该目录，安装仍须拖动 Voicemore。Finder 最多使用两行标签，因此不承诺与 mock 中单行文字完全一致。应用名保持 Voicemore，Applications 链接名称遵循 Finder 行为。

系统语言切换后遵循 Finder 会话刷新/重新登录行为，不承诺实时刷新已打开的窗口。Apple 文档还注明本地化显示可能受“显示所有文件扩展名”设置影响；不修改用户设置以规避此边界。原生 FileManager 解析验证不能代替不同系统语言下的 Finder 截图验收。

参考：[Apple 目录名称本地化](https://developer.apple.com/library/archive/documentation/FileManagement/Conceptual/FileSystemAdvancedPT/LocalizingtheNameofaDirectory/LocalizingtheNameofaDirectory.html)、[dmgbuild 布局参数](https://dmgbuild.readthedocs.io/en/latest/settings.html)。`grid_spacing` 不得超过 100，否则 Finder 会丢弃整组图标视图设置。

## 构建

需要 macOS、Xcode Command Line Tools（Swift / SetFile）、Python 3.10+（或 uv 自动选择）与首次安装 Python 依赖时的网络。

```sh
./scripts/build-dmg.sh build/darwin-arm64/Voicemore.app /absolute/path/new-preview.dmg
./scripts/verify-dmg.sh /absolute/path/new-preview.dmg artifacts/dmg-check build/darwin-arm64/Voicemore.app
```

脚本只打包已有、签名验证通过的应用；不修改应用的 FinderInfo 或任何已封装资源，不改签名和权限。新输出路径必须不存在，临时磁盘与目录由打包工具清理。Python 依赖固定在 `scripts/dmg-requirements.txt`，隔离安装到忽略的 `build/dmg-tools/`。

本地 `build-macos.sh` 跳过 mygo 默认 DMG，调用统一布局入口并使用本机固定开发证书签名 DMG。正式 `finalize-distribution.py` 也调用此入口，之后仍必须执行 Developer ID 签名、公证、装订和验收；本地视觉预览不代表正式分发通过。

## 验收

1. 双击最终 DMG；确认自动打开窗口、声波/箭头、两枚真实图标、文字衬底和完整提示，无滚动条或工具栏。
2. 弹出后重新挂载，确认背景仍可解析；打开 Applications 链接，确认指向真实应用程序目录。
3. `verify-dmg.sh` 挂载只读镜像、复制应用到临时目录，验证签名、架构、版本、运行时校验和，输出 SHA-256、布局数据和六种语言配置的原生名称解析结果。
4. 第三个参数提供原始 app 时，`verify-dmg.sh` 对照镜像中的应用执行内容比较。正式公证包还需通过 `verify-distribution-content.py` 的票据和 Gatekeeper 检查；开发预览不通过该正式门禁属于预期，不能放宽门禁。
5. 在不同语言的 Finder 会话中补充简体/繁体/英文视觉验收，尤其检查换行和系统扩展名设置。禁止把 API 解析报告称为已完成三语言 Finder UI 验收。

可重复证据写入忽略的 `artifacts/`，不提交截图或开发 DMG。
