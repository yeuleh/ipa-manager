# Scaffold Result — ipa-manager（Stage 5）

> 脚手架已完成并通过完整编译/测试验证。Spock 评审的 M4（ipatool + go-ios + cobra + lipgloss + huh 同时 import）已**实证通过**。

## 验证结果（2026-06-28，Go 1.26.4）

| 检查                     | 结果                                            |
| ------------------------ | ----------------------------------------------- |
| `go mod tidy`            | ✅ 成功（58 条依赖，无硬冲突）                        |
| `go build ./...`         | ✅ exit 0（所有外部库类型引用真实有效）              |
| `go test ./...`          | ✅ account 包 ProfileKeychain 隔离测试通过          |
| `go vet ./...`           | ✅ exit 0                                        |
| 二进制 `--help` / `--version` | ✅ 命令树完整，版本注入正确（`0.1.0-dev`）          |

## go.mod 关键依赖（已锁定）

```
module github.com/yeuleh/ipa-manager
go 1.26.4
require (
    charm.land/huh/v2 v2.0.3
    charm.land/lipgloss/v2 v2.0.4
    github.com/danielpaulus/go-ios v1.2.0
    github.com/majd/ipatool/v2 v2.3.0
    github.com/spf13/cobra v1.10.2
    github.com/stretchr/testify v1.11.1
)
```
`99designs/keyring v1.2.1` 作为 ipatool 的 indirect 确认引入（坐实 research.md 的修正）。

## 实际目录结构

```
ipa-manager/
  cmd/ipa-manager/main.go              # 入口，调 cli.Execute(Version)
  internal/
    apperr/errors.go                   # 共享 sentinel（ErrNotImplemented）
    cli/
      root.go                          # cobra root + Execute + 命令注册
      auth.go                          # auth login(2FA) / logout
      account.go                       # accounts list/use/add/remove
      apps.go                          # apps search / versions
      devices.go                       # devices list
      install.go                       # install download/push/uninstall/update
      doctor.go                        # doctor 健康检查
    account/
      profile.go                       # Profile 类型
      store.go                         # profile 配置读写（TODO）
      keychain.go                      # ProfileKeychain wrapper（已实现+编译期断言）
      cookiejar.go                     # 每 profile cookie jar 路径
      keychain_test.go                 # ProfileKeychain 隔离测试（已通过）
    appstore/
      client.go                        # ipatool AppStore 工厂（TODO 接线）
      apps.go                          # search/download 封装（TODO）
      errors.go                        # ErrAuthCodeRequired（alias ipatool sentinel）
    device/
      client.go                        # ios.ListDevices 封装（TODO）
      apps.go                          # list/install/uninstall（TODO）
      errors.go                        # ErrTunnelRequired sentinel
    library/ipa_store.go               # 按账号隔离本地 .ipa（TODO）
    config/
      paths.go                         # 路径常量
      config.go                        # 全局配置（不含 IPA 清单）
    doctor/checks.go                   # 健康检查（TODO）
    ui/
      prompt.go                        # huh Select/Confirm/Input（TODO 接线）
      table.go                         # lipgloss table 渲染
  docs/                                # 已存在
  go.mod  go.sum
  .golangci.yaml  Makefile
```

## 文件用途清单（27 个项目文件）

| 文件                                    | 用途 / 状态                         |
| --------------------------------------- | ----------------------------------- |
| `cmd/ipa-manager/main.go`               | 入口；已实现                        |
| `internal/apperr/errors.go`             | 共享错误；已实现                    |
| `internal/cli/*.go` (7)                 | cobra 命令树；结构已实现，RunE 为 TODO |
| `internal/account/profile.go`           | Profile 类型；已实现                |
| `internal/account/store.go`             | profile 存储；TODO                  |
| `internal/account/keychain.go`          | **ProfileKeychain wrapper；已实现 + 编译期接口断言** |
| `internal/account/cookiejar.go`         | cookie jar 路径；已实现             |
| `internal/account/keychain_test.go`     | 隔离测试；**已通过**                  |
| `internal/appstore/client.go`           | AppStore 工厂；TODO（类型契约已锁） |
| `internal/appstore/apps.go`             | search/download；TODO               |
| `internal/appstore/errors.go`           | ErrAuthCodeRequired；已实现（alias）|
| `internal/device/client.go`             | 设备列举；TODO                      |
| `internal/device/apps.go`               | 安装/列举/卸载；TODO                |
| `internal/device/errors.go`             | ErrTunnelRequired；已实现           |
| `internal/library/ipa_store.go`         | 本地 .ipa 隔离存储；TODO            |
| `internal/config/paths.go`              | 路径常量；已实现                    |
| `internal/config/config.go`             | 全局配置；TODO                      |
| `internal/doctor/checks.go`             | 健康检查；TODO                      |
| `internal/ui/prompt.go`                 | huh 交互；TODO（API 契约已锁）      |
| `internal/ui/table.go`                  | lipgloss 表格；已实现               |
| `.golangci.yaml`                        | lint 配置；已实现                   |
| `Makefile`                              | build/test/vet/fmt/tidy/lint；已实现 |
| `go.mod` / `go.sum`                     | 依赖清单；已锁定                    |

## ADR 候选评估

| 方面             | 是否建 ADR | 理由                                                                                          |
| ---------------- | ---------- | --------------------------------------------------------------------------------------------- |
| 目录结构 (cmd/internal) | 否         | 遵循 Go 标准惯例；research.md/config-plan.md 已记录。可逆性低但非架构性决策                  |
| **多账号隔离架构**   | **是 → ADR 0002** | 难逆转（影响所有凭据处理）、拒绝备选（per-profile ServiceName / 不隔离）、是本项目核心设计 |
| 构建工具         | 否         | `go build` 起步，goreleaser 视后续需要；可逆，标准                                            |
| 核心依赖选择     | 否         | 已由 ADR 0001（技术栈）覆盖                                                                   |

→ 新增 `docs/bootstrap/decisions/0002-multi-account-isolation.md`。

## 与 config-plan 的差异

无实质性差异。唯一调整：go.mod 的 `go` 指令解析为 `1.26.4`（go mod init 按本机 toolchain 写入），而非 config-plan 写的 `go 1.26.0` + 显式 `toolchain go1.26.4`。功能等价（满足 go-ios 的 ≥1.26 要求），保留 1.26.4。
