# AGENTS.md

项目级协作约定与上下文，供 AI 代理（及人类协作者）快速理解本项目。

## OVERVIEW

**ipa-manager** — macOS CLI 工具，管理多个 Apple 账号下 iOS 应用（`.ipa`）的全生命周期：登录/切换账号、按账号隔离下载与本地管理、推送到 iOS 设备安装/卸载。

纯 Go 单二进制，CLI 交互（cobra + lipgloss + huh，精简 CLI；全屏 TUI 为未来升级路径）。两个核心底层库均**代码级 import**（非 subprocess）：
- `github.com/yeuleh/ipatool/v2`（MIT fork，`fix-auth` tag）— Apple 账号登录/搜索/下载
- `github.com/danielpaulus/go-ios`（MIT）— iOS 设备安装/列举/卸载

本项目只写真正属于自己的核心价值：多账号 profile 管理、按账号隔离、流程编排、CLI 体验。

## STRUCTURE

```
cmd/ipa-manager/      极薄入口，仅调 cli.Execute()
internal/
  cli/                cobra 命令树 + Deps 依赖注入 + 编排（详见该目录 AGENTS.md）
  account/            多账号 profile + ProfileKeychain namespace 隔离（ADR 0002）
  appstore/           ipatool adapter（ipatool 类型止步于此）+ 下载编排
  device/             go-ios adapter（go-ios 类型止步于此）；list/apps/install/uninstall
  library/            per-profile 本地 IPA 库 + 复合键索引
  config/  ui/  apperr/  doctor/   路径 / 交互提示(huh) / 共享 sentinel errors / 健康检查(stub)
docs/
  bootstrap/          初始研究/决策/技术栈（项目根基，详见 research.md / decisions/）
  features/<mission>/ 每个 mission 的 requirements/design/plan/e2e_test/validate（详见 docs/features/AGENTS.md）
```

**核心设计**：`account.ProfileKeychain` 把 ipatool 固定 key `"account"` 按 profile namespace 化（`profiles/<id>/account`），实现多账号隔离（ADR 0002）。

**技术栈**：Go ≥ 1.26；凭据存 macOS Keychain（复用 ipatool keyring）；cobra + lipgloss + huh。详见 `docs/bootstrap/tech-stack.md` 与 `docs/bootstrap/decisions/0001-tech-stack.md`。

## WHERE TO LOOK

- `docs/bootstrap/research.md` — ipatool/go-ios API 集成模式、keychain namespace、选型（**注意：其中"iOS 17+ 需 tunnel"结论已过时，见下**）
- `docs/bootstrap/decisions/0001/0002` — 技术栈 / 多账号隔离 ADR
- `docs/features/<mission>/` — 每个 mission 的完整 spec（需求→设计→计划→e2e→validate）
- `go.mod` — 依赖版本（ipatool fork replace 指向 `yeuleh/ipatool/v2@fix-auth`）

## COMMANDS

```bash
go build ./...                 # 编译（需 cgo：macOS Keychain backend）
go test ./... -count=1         # 全量测试
go vet ./...                   # 静态检查
go build -o temp/ipa-manager ./cmd/ipa-manager   # 出二进制（temp/ 已 gitignore）
./temp/ipa-manager device list  # 运行
```

**Definition of Done**：`go build && go vet && go test ./...` 全绿 + 涉及包测试全绿 + 无 go-ios/ipatool 类型泄露到 CLI 层（`grep -rn "danielpaulus/go-ios\|majd/ipatool" internal/cli` 无结果）。

## CONVENTIONS

<!-- planet-fall:preserve-start -->
### Memo — 2026-06-30

- 临时性脚本放在项目内 `./temp/` 目录，**不要**放系统 `/tmp`
- `./temp/` 必须加入 `.gitignore`
- 任务完成后清理 `./temp/`
<!-- planet-fall:preserve-end -->

- **adapter 隔离**：go-ios 类型止步 `internal/device/`，ipatool 类型止步 `internal/appstore/`；CLI 层只见我们的接口（`device.Service` / `appstore.ProfileAppStore`）。
- **依赖注入**：所有外部依赖经 `cli.Deps` 注入（Store/AppStoreFactory/UI/LibraryStore/DeviceService/ConfigRoot），测试用 mock。
- **最小成本**：个人小工具，优化目标是最小实现成本；过度设计（如基于过时文献的 tunnel 机器）应实证后删除。

## BOUNDARIES

- **Always**：改 spec 前先读已验收文档（requirements/design 是单一事实源）；CLI 不直接 import go-ios/ipatool；`go test ./...` 全绿才提交。
- **Ask First**：改 `internal/appstore` 的 ipatool fork 耦合、改 keychain 存储格式、删除既有 AGENTS.md。
- **Never**：自动 `sudo` / 提权 / 启动 tunnel；subprocess 调用 ipatool/go-ios CLI（必须代码级 import）；把 Apple ID password/token 写进日志。

## NOTES

**已知风险（贯穿全项目）**：
1. **ipatool 依赖 Apple 私有 API**：Apple 改服务端时会临时失效，需 fork 跟进。账号侧能力稳定性取决于此。
2. **Apple ID 自动化登录有理论风控风险**：个人小流量风险低但非零。只用自己可接受风险的账号。
3. ~~iOS 17+ 设备需 tunnel~~ —— **已实证证伪**（iOS 26 真机：install/apps/uninstall 经 usbmuxd 全部可用，无需 tunnel）。原 `docs/bootstrap/research.md` 的 tunnel 结论过时；tunnel 机器已从 `internal/device/` 移除（见 `docs/features/ios-device-manage/` Live Amendment）。**实证 > 文献。**

**已交付 mission**（`docs/features/`）：multi-account-login-switch、fix-ipatool-auth、download-ipa-by-account、ios-device-manage（设备侧，App Store→library→设备 完整闭环）。
