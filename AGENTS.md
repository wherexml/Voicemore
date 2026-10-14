# 项目约定

## macOS 本地开发签名

- 本地开发、构建、重签名、打包及运行 Voicemore 时，始终使用同一张既有 Apple Development 证书：`Apple Development: geekmooc@gmail.com (5QLDC77G39)`。
- 构建命令：`SIGNING_IDENTITY='Apple Development: geekmooc@gmail.com (5QLDC77G39)' ./scripts/build-macos.sh`。
- 禁止使用 `SIGNING_IDENTITY=-`、ad-hoc 临时签名或无签名构建替代该证书。证书缺失、过期或不可用时，停止签名并说明原因，不得自动切换证书或降级签名。
- 保持 Bundle ID `dev.steve.voice-input` 不变；更换证书或应用身份须经用户明确授权。开发签名规则不代表已授权发布或公证。
- 启动新构建前，核对代码签名及 designated requirement，确认与既有授权身份一致。macOS 辅助功能、输入监控等权限依赖应用签名身份；系统设置开关开启不保证换签后的程序仍获授权。
- 若遇到“系统权限已开启，但客户端显示未授权”，先检查当前运行路径、签名身份与 TCC 日志，不要先重置权限或要求用户反复授权。
