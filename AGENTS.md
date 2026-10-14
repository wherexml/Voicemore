# 项目约定

本地开发前，如存在 [本机开发约定](docs/macos-local-signing.md)，须先读取并遵守。该文档仅保存在本机，不纳入版本控制。

正式 GitHub Release 默认且必须完成 Developer ID 签名、Apple 公证、票据校验及最终安装包验收，使用 `scripts/publish-release.sh`，遵循[正式分发流程](docs/public/DISTRIBUTION.md)。不得将开发签名或公证未通过的包发布为正式版／latest；开发预览仅允许显式 `--development-preview`。

## 模块规格

- 修改相关行为时同步维护[文档索引](docs/public/README.md)引用的模块规格与验收步骤。
- Sprint 草稿与本地验收证据不入库，目录约定见[文档索引](docs/public/README.md#维护约定)。
