# Changelog

## 0.3.0

- Check for signed updates at startup and hourly while running.
- Show available versions on Home, in the menu-bar tooltip, and via an optional macOS notification.
- Update from Home in one click: download progress, Ed25519 verification, app replacement and automatic restart.
- Support cancellation and retry; protect active dictation/model work and retain existing data.
- Add a repeatable signed-release publishing workflow.

客户端启动时和每小时检查更新；首页一键下载、验签、安装并自动重启。支持取消和重试，保留原有数据，避免打断听写和模型部署。

Distribution remains development-signed and unnotarized; update archive signatures are separate from Apple notarization.
