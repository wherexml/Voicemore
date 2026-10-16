# Finder 安装窗口素材

- `background@2x.png`：1536 × 1024，显示为 768 × 512 pt。图片没有语言文字；声波和细箭头来自已确认的 mock。
- 使用内置 imagegen 生成；最终提示词：保留深蓝黑声波与细箭头，移除图标、标题栏和所有文字，并添加三个浅灰文字衬底以承载 Finder 的黑色原生标签。
- 图标由 Finder 从签名应用及 `/Applications` 链接读取，不把假图标画进背景。
- 图片坐标与 `scripts/build-dmg.py` 的图标位置配套；修改后必须打开实际 DMG 验证。
- 包装、语言和验收边界见 [安装窗口规格](../../docs/public/DMG-INSTALLER.md)。
