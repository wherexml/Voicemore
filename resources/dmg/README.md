# Finder 安装窗口素材

- `background@2x.png`：1536 × 1024，显示为 768 × 512 pt。深蓝黑声波、细箭头、白色名称和一行英文提示；无边框和文字衬底。
- 使用内置 imagegen 生成。提示词核心：保留已确认的声波和细箭头；背景直接显示 Voicemore、Applications 与单行 Drag Voicemore to Applications to install.，不生成图标或窗口框。原生黑色标签区域使用无边界的深色阴影，避免与白字重叠。
- 图标由 Finder 从签名应用及 `/Applications` 链接读取，不把假图标画进背景。
- 图片坐标与 `scripts/build-dmg.py` 的图标位置配套；修改后必须打开实际 DMG 验证。
- 构建与验收入口见 [BUILDING.md](../../BUILDING.md)。

- 对齐基线：2x 背景声波中线为 y=404 px，对应 Finder 两侧图标中心 y=202 pt。白色名称上移至图标下方约 18–24 pt，底部安装提示位置不变。
