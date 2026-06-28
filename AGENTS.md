# AGENTS.md

项目级协作约定与上下文，供 AI 代理（及人类协作者）快速理解本项目。

<!-- mindtrek-bootstrap:start stage="requirements-discussed" -->
## 项目：ipa-manager

**是什么**：macOS CLI 工具，管理多个 Apple 账号下 iOS 应用（`.ipa`）的全生命周期——登录/切换账号、按账号隔离下载与本地管理、推送到 iOS 设备安装/更新。

**技术定位**：纯 Go 单二进制，TUI 交互。两个核心底层库均代码级 import（非 subprocess）：
- `github.com/majd/ipatool/v2`（MIT）— Apple 账号登录/搜索/下载
- `github.com/danielpaulus/go-ios`（MIT）— iOS 设备安装/列举/卸载

本项目只写真正属于自己的核心价值：多账号 profile 管理、按账号隔离、流程编排、TUI 体验。

## Bootstrap 进度

- **Stage 1 — 需求讨论**：✅ 完成。详见 `docs/bootstrap/requirements.md`
- Stage 2 — 技术选型：⏳ 待开始
- Stage 3 — 研究：⏳ 待开始
- Stage 4 — 配置方案：⏳ 待开始
- Stage 5 — 脚手架：⏳ 待开始
- Stage 6 — 工作区初始化：⏳ 待开始

## 已知风险（贯穿全项目）

1. **ipatool 依赖 Apple 私有 API**：Apple 改服务端时会临时失效，需 ipatool 项目跟进（通常数周内修复）。账号侧能力稳定性取决于此。
2. **iOS 17+ 设备需 tunnel**：go-ios 对 iOS 17+ 设备需 `sudo ios tunnel start`；tunnel 协议随 iOS 版本演进（如 iOS 18.2 移除 QUIC）。设备侧能力稳定性取决于此。
3. **Apple ID 自动化登录有理论风控风险**：个人小流量使用风险低，但非零。建议只用自己可接受风险的账号。
<!-- mindtrek-bootstrap:end -->
