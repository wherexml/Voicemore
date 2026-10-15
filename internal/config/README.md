# 配置模块规格

`Store` 负责读取和持久化 Settings；远程桌面的完整行为见[远程输入规格](../../docs/public/REMOTE-INPUT.md)。

## RemoteInputs

`remoteInputs` 是可选数组，旧设置缺失该字段时保持空规则，不自动启用远程输入。

| JSON 字段 | 含义 |
| --- | --- |
| `bundleId` | 客户端精确匹配标识，空值不匹配 |
| `appName` | 设置界面显示名，不参与匹配 |
| `windowTitle` | 可选、大小写敏感的标题子串；省略或空值匹配所有窗口 |
| `shortcut` | `cmd-v` / `ctrl-v` 启用；空值或其他值不启用远程路径 |
| `syncDelayMs` | 剪贴板同步等待毫秒数，执行时限制在 200–10000 ms |

Store.Get 和 Store.Set 都复制 RemoteInputs 切片，避免调用方修改规则数组时直接改变存储内部切片。该结构目前只有值字段；未来增加嵌套可变字段时须重新检查复制边界。数组顺序有意义，同等具体程度的规则由后者覆盖。配置层不去重、不自动规范化窗口标题。

验收沿用远程输入 E2E：新增、修改、删除后重新打开设置及重启应用，核对规则值、顺序和旧配置兼容性。证据保存到被忽略的 `artifacts/remote-input/`。

## 数字格式默认值

`textStyle.arabicNumbers` 默认 true。`Open` 先载入 Defaults 再解码旧 JSON，缺字段时启用，已有其他格式开关不变；显式 false 保存重启后仍关闭。设置 → 通用 → 基础格式提供开关。规则及音频验收见[基础格式规格](../../docs/public/TEXT-FORMATTING.md)。
