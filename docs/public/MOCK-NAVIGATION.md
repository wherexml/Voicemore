# Mock 导航与维护

统一入口：http://127.0.0.1:38127/ 。本地文件：`mock/index.html`。

在项目根目录启动（已有服务时直接复用）：

```sh
python3 -m http.server 38127 --bind 127.0.0.1 --directory mock
```

| 模块 | 当前入口（相对 mock/） |
| --- | --- |
| 首页 · 中文 | `home-v3.html` |
| 首页 · English | `home-v3-en.html` |
| 历史记录 | `history-v3.html?lang=hans#history` |
| 词典 | `auto-send.html#dictionary` |
| 通用 | `auto-send.html#settings/general` |
| 听写 · 设备按键 · 翻译 | `auto-send.html#settings/dictation` |
| 文字与粘贴 · 自动发送 · 远程桌面 | `auto-send.html#settings/output` |
| 模型 | `auto-send.html#settings/models` |
| 权限 | `auto-send.html#settings/perms` |
| 识别语言 · 私密应用 | `language-privacy.html` |
| 开始使用 | `onboarding-v6.html` |
| 模型选择 | `onboarding-model-v2.html` |
| 连续胶囊状态 | `capsule-states-v2.html` |

旧稿导航：`mock/archive/index.html`；本次归档：`mock/archive/2026-10-17/`。早期备份留在 `mock/archive/`，隔夜快照留在 `mock/archive/overnight-20261016/`，均由归档导航收录。独立设备按键稿已归档，当前入口为听写设置。

`mock/` 仍仅保存在本机、不入 Git。共享资源保留原位，归档页通过相对目录链接复用；移动或复制归档时须一起保留这些资源。新增版本时同步维护统一导航和本表，再归档被替代稿。导航中的“当前”表示 mock 选择，不代表原生或发布验收通过。

验收：打开统一入口，逐项点击并确认目标模块；检查历史直接进入历史、词典直接进入词典、设置各分类正确；打开归档与 V1/V2 对照，检查资源加载；返回导航。证据保存在 `artifacts/mock-navigation/`。

## 页面返回入口

全部模块与归档页面共享 `mock/components/mock-navigation.js`，左下角固定“返回导航”，直接返回统一入口，不依赖浏览器历史。组件使用独立样式，页面重绘时保留；V1/V2 对照的内嵌页面隐藏重复入口，仅外层显示。入口支持 Tab 聚焦与 Enter 跳转，打印时隐藏。新增 HTML 须使用相对路径加载该组件。

复验：从导航打开每个模块，点击“返回导航”，确认回到 `mock/index.html`；设置切换后入口仍可用；归档及深层隔夜方案返回同一入口。证据：`artifacts/mock-navigation/return-navigation/`。
