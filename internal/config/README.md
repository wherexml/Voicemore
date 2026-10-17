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

## 识别语言与私密应用

`asrLanguage` 为可选字符串：缺失或空为中文（旧行为），`auto`、`en` 为其余选项，未知值按中文；仅本地服务使用，见[识别语言](../../docs/public/LOCAL-ASR.md#识别语言)。`privateApps[]` 为 `{bundleId, appName}`，按 Bundle ID 精确匹配，空 ID 不匹配；Store.Get/Set 复制该切片。行为见[私密应用](../../docs/public/PRIVATE-APPS.md)。

## 数字格式默认值

`textStyle.arabicNumbers` 默认 true。`Open` 先载入 Defaults 再解码旧 JSON，缺字段时启用，已有其他格式开关不变；显式 false 保存重启后仍关闭。设置 → 通用 → 基础格式提供开关。规则及音频验收见[基础格式规格](../../docs/public/TEXT-FORMATTING.md)。

## 自定义整理服务的思考参数

`providers.custom[]` 新增可选字符串 `thinkingMode`（空/server、deepseek、omlx、custom）与 `thinkingParams`（非敏感 JSON 对象文本）。旧配置缺字段默认跟随服务端，不迁移为强制关闭。字符串字段随既有 Clone、Store.Set/Get、保存重开保留；取消只丢弃编辑草稿。模式及 JSON 在编辑保存和请求边界验证，不把 API Key 写入这些字段。

协议、测试按钮与兼容边界见[整理规格](../cleanup/README.md#自定义服务的思考模式)；保存重开及 HTTP 验收：`go run ./cmd/custom-thinking-acceptance`。
