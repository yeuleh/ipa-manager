# ADR 0001 — 技术栈选型

- 状态：Accepted
- 日期：2026-06-28
- 决策者：Leon

## 背景 (Context)

ipa-manager 需要管理多 Apple 账号下的 iOS 应用全生命周期：登录/切换账号、按账号隔离下载 `.ipa`、推送到 iOS 设备安装/更新。这是个人小工具，优化目标是最小实现成本，同时希望最终是一个交互体验良好的单一工具。

核心能力（Apple 账号登录/下载、iOS 设备通信）都已有成熟开源实现。关键问题是如何集成：
- `ipatool`（Apple 登录/下载）是 Go + MIT，`pkg/appstore` 暴露干净的依赖注入式 `AppStore` 接口，可代码级 import。
- 设备操作侧 `ideviceinstaller`/`libimobiledevice` 是 C + GPL-2.0，无法 import。但存在纯 Go + MIT 的替代品 `go-ios`，把设备通信能力用 Go 重实现，可代码级 import，且活跃度/生产背书更强。

## 决策 (Decision)

采用**纯 Go 单二进制**架构：

1. **语言**：Go（≥ 1.26，因 go-ios 模块要求）。
2. **底层库（代码级 import）**：
   - `github.com/majd/ipatool/v2` — Apple 账号登录/搜索/下载。
   - `github.com/danielpaulus/go-ios` — 设备安装/列举/卸载/配对。
3. **凭据存储**：复用 ipatool 的 `99designs/keyring`（macOS Keychain 后端），不自造。
4. **交互框架（v1）**：组合 A —— `cobra`（命令分发）+ `lipgloss`（彩色输出）+ 交互提示库。命令式 + 可脚本化。全屏 TUI（bubbletea）作为未来升级路径，不在 v1 范围。
5. **本项目只写**：多账号 profile 管理、按账号隔离、流程编排、CLI 体验。

## 结果 (Consequences)

- **正向**：单二进制分发、零外部进程依赖、全 MIT 无协议风险；两个最重的活由活跃维护的库承担，库升级即获得 Apple/iOS 跟进修复（`go get -u`）；本项目代码量聚焦在核心价值。
- **负向 / 风险**：
  - ipatool 依赖 Apple 私有 API，服务端变更时账号侧能力会临时失效，需等项目跟进。
  - iOS 17+ 设备需 `sudo ios tunnel start`，tunnel 协议随 iOS 演进（如 iOS 18.2 移除 QUIC），设备侧能力受 Apple 节奏影响。
  - Go 1.26 要求意味着开发机需较新 Go 工具链。
  - 组合 A 的 UX 为"好用"级别；若日后需全屏交互体验，需迁移到 bubbletea（架构已为此预留：cobra 在底层，TUI 是替换层）。
- **缓和**：账号风控风险通过"只用自己可接受风险的账号"控制；tunnel 复杂度是设备管理工具的普遍宿命，go-ios 跟进最勤。

## 备选方案 (Alternatives Considered)

1. **纯 bash/Python 包装**（subprocess 调用 ipatool + ideviceinstaller）：被否。无法代码级合并、丢单二进制优势、UX 受限，且不满足用户"合并成一个体验良好的工具"的诉求。
2. **用 Go 重写 ideviceinstaller 以避开 GPL**：被否。前提不成立（subprocess 调用本就不触发 GPL 传染），且无必要——go-ios 已是纯 Go + MIT 的现成替代。
3. **Swift 原生**：被否。无法 import ipatool（Go 库），会退回 subprocess 包装。
4. **组合 B（bubbletea TUI 优先）**：暂缓。v1 用组合 A 快速出活，B 作为体验升级路径保留。
<!-- mindtrek-bootstrap:end -->
