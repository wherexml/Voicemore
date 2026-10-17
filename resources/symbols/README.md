# 复制与删除系统图标

`doc.on.doc.png` 和 `trash.png` 是用户确认的 Apple SF Symbols 原版，由 macOS 的 `NSImage(systemSymbolName:)` 导出；使用 Regular 字重、Medium scale、16 pt 配置，不手工描摹或修改图形。当前资源取自 macOS 27.0（26A428）。仅用于 Apple 平台应用界面。

MyGo 0.4.8 无法直接绘制 `NSSymbolImageRep`，因此保存为 128×128 的透明白色遮罩。`action_icons.go` 在运行时按主题颜色着色；首页/历史保持 16×16 DIP 图形和 28×28 DIP 命中区域，其他删除入口沿用原有 13/14 DIP 大小。高分辨率遮罩用于 Retina 缩小显示，不作为应用图标或品牌标志。

在仓库根目录重新导出：

```sh
swift scripts/export-action-symbols.swift
```

导出需要 macOS 和 Apple 命令行开发工具。符号缺失或编码失败时脚本停止，不生成替代图形；普通构建直接使用已保存资源，不依赖导出环境。验收步骤见 [首页与历史规格](../../docs/public/HOME-HISTORY.md#复制与删除系统图标)。
