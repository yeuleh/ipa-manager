# Config Plan — ipa-manager（Stage 4）

> 本方案经 Spock 严格评审（GO-WITH-FIXES），已纳入全部修正。Stage 5 脚手架将按此执行。

## 1. 确认的配置

### Module path
`github.com/yeuleh/ipa-manager`

### 目录结构（最终版，含评审新增）

```
ipa-manager/
  cmd/ipa-manager/
    main.go                         # 极薄入口，仅调 internal/cli.Execute()

  internal/
    cli/
      root.go                       # cobra root + 全局 flag + 版本
      auth.go                       # login（含 2FA 重试）/ logout
      account.go                    # accounts: list / use / add / remove
      apps.go                       # search / list-versions
      devices.go                    # devices list
      install.go                    # download / install / uninstall / update
      doctor.go                     # doctor: 环境健康检查（评审新增）

    account/
      profile.go                    # Profile 类型定义
      store.go                      # profiles 配置读写
      keychain.go                   # ProfileKeychain wrapper（核心：多账号隔离）
      cookiejar.go                  # 每 profile 的 persistent-cookiejar 构造（评审新增）

    appstore/
      client.go                     # ipatool AppStore 工厂（按 profile 实例化，注入 keychain+cookiejar）
      apps.go                       # search / download / list-versions 封装（评审建议合并薄文件）
      errors.go                     # appstore 侧 sentinel errors（如 ErrAuthCodeRequired 透传）

    device/
      client.go                     # 设备获取 + tunnel 前置检测
      apps.go                       # list / install / uninstall 封装（go-ios 编排）
      errors.go                     # device 侧 sentinel errors（如 ErrTunnelRequired）

    library/
      ipa_store.go                  # 按账号隔离本地 .ipa 文件管理 + 索引（不放 config）

    config/
      paths.go                      # ~/.ipa-manager 路径常量
      config.go                     # 全局配置读写（不含 IPA 清单）

    doctor/                         # 评审新增
      checks.go                     # 健康检查：Go/macOS/keychain/go-ios/tunnel

    ui/
      prompt.go                     # huh 交互（账号/设备选择、确认、2FA 输入）
      table.go                      # lipgloss 彩色表格输出

  docs/                             # 已存在
  go.mod  go.sum
  .golangci.yaml  Makefile
```

**包边界原则**（评审 m4/m5 采纳）：
- `config` 只存路径与全局配置，**不存** IPA 清单（清单归 `library`）。
- `appstore`/`device` 各自一个 `client.go` + 一个能力文件（`apps.go`），薄文件不强行多拆，逻辑膨胀再分。
- 第三方类型不泄漏到 CLI 层。

### go.mod（评审修正后）

```
module github.com/yeuleh/ipa-manager

go 1.26.0

toolchain go1.26.4

require (
    github.com/spf13/cobra v1.10.2
    charm.land/lipgloss/v2 v2.0.4
    charm.land/huh/v2 v2.0.3
    github.com/majd/ipatool/v2 v2.3.0
    github.com/danielpaulus/go-ios v1.2.0
    github.com/stretchr/testify v1.11.1
)
```
> indirect 依赖交由 `go mod tidy` 生成，不手写。

### 工具链版本（评审核验真实 latest）

| 工具/依赖       | 版本     | 核验来源（2026-06-28）         |
| --------------- | -------- | ------------------------------ |
| Go              | 1.26.0 (toolchain go1.26.4) | go-ios v1.2.0 go.mod 硬要求 |
| cobra           | v1.10.2  | GitHub Releases（2025-12-04）  |
| lipgloss        | v2.0.4   | GitHub Releases（2026-06-12）  |
| huh             | v2.0.3   | GitHub Releases（2026-03-10）  |
| ipatool/v2      | v2.3.0   | GitHub Releases（2026-02-16）  |
| go-ios          | v1.2.0   | GitHub Releases（2026-06-08）  |
| testify         | v1.11.1  | GitHub Releases（2025-08-27）  |
| golangci-lint   | v2.12.2  | GitHub Releases                |

### 导入路径（评审核验，v2 域名迁移注意）

| 库             | 正确导入路径                       |
| -------------- | ---------------------------------- |
| cobra          | `github.com/spf13/cobra`             |
| lipgloss       | `charm.land/lipgloss/v2`（+ `/table`）|
| huh            | `charm.land/huh/v2`（**非** `github.com/charmbracelet/huh`）|
| ipatool        | `github.com/majd/ipatool/v2/...`     |
| go-ios         | `github.com/danielpaulus/go-ios/...` |

### .golangci.yaml（要点）
```yaml
version: "2"
linters:
  default: standard
  enable: [govet, staticcheck, errcheck, ineffassign, unused, misspell, gosec, revive]
```
不一开始 `default: all`；Apple ID/password/token 日志靠 code review 把关。

### Makefile
`build` / `test` / `lint` / `run` / `tidy` 目标。

---

## 2. 设计要点（评审 Major/Minor 采纳）

### 2.1 多账号隔离（B2 / M1）
- **keychain**：`ProfileKeychain` wrapper 把 ipatool 固定 key `"account"` 映射为 `profiles/<id>/account`。
- **cookie jar**：每 profile 独立 `persistent-cookiejar` 文件，路径 `~/.ipa-manager/profiles/<id>/cookies`。
- **单一构造入口**：`appstore/client.go` 的 `NewProfileAppStore(profileID)` 同时注入两者。
- **一致性**：删除 profile 时同时 revoke keychain namespace + 删 cookie jar。
- **并发**：v1 用文件锁或简单拒绝同 profile 并发登录。

### 2.2 2FA 登录重试流（M2）
1. `auth login` 收集 email/password（password 不入配置文件）。
2. 调 `AppStore.Login(AuthCode:"")`。
3. 若 `errors.Is(err, appstore.ErrAuthCodeRequired)` → huh Input 提示 2FA → 同 email/password + AuthCode 重试。
4. 成功后 account JSON 经 wrapper 存入 Keychain。

### 2.3 iOS 17+ tunnel（M5）
- v1 **不自动 `sudo` 提权**。
- 设备操作前检测 `SupportsRsd()` / tunnel 状态；连接失败时输出明确提示：
  > This device requires iOS 17+ tunnel. Run: `sudo ios tunnel start`, then retry.
- 提供 `ipa-manager doctor` 只检测不提权。

### 2.4 错误分类（m2）
- `appstore/errors.go`、`device/errors.go` 定义 CLI 需识别的 sentinel errors（未登录 / 2FA required / keychain 不可用 / tunnel 未启动 / 设备未信任 / Apple API 失败）。
- 不过度设计层级。

### 2.5 doctor 命令（m3）
`ipa-manager doctor`：检查 Go/runtime、macOS、keychain backend 可打开、`ios.ListDevices`、iOS 17+ tunnel 提示。

---

## 3. Stage 5 实现深度

脚手架只搭"能编译 + 结构清晰"的骨架：
- 每个文件有清晰的包声明 + 关键类型/函数签名 + `// TODO` 占位。
- `main.go` 能跑出 cobra help（`./ipa-manager --help`、`./ipa-manager doctor`）。
- **依赖编译验证**（评审 M4）：脚手架第一步即 `go mod tidy && go build ./...`，确认 ipatool + go-ios 同时 import 无硬冲突；若 `go mod tidy` 暴露传递依赖冲突，按 Failure 处理流程上报。
- **实际功能实现**留给后续 `/mission engage`。

---

## 4. 调整历史（Spock Stage 4 评审修正）

| # | 评审编号 | 原方案                          | 修正                                       |
| - | -------- | ------------------------------- | ------------------------------------------ |
| 1 | B1       | `github.com/charmbracelet/huh latest` | `charm.land/huh/v2 v2.0.3`（huh v2 已迁域名）|
| 2 | B2       | research.md 记 ipatool 用 byteness/keyring | 更正：v2.3.0 用 `99designs/keyring v1.2.1`（已同步修 research.md）|
| 3 | M1       | 未明确 cookie jar 落点          | 新增 `internal/account/cookiejar.go`        |
| 4 | M2       | 未设计 2FA 重试                 | auth.go 明确 ErrAuthCodeRequired 重试流     |
| 5 | M3       | go ≥1.26（可选语气）            | 硬约束：`go 1.26.0` + `toolchain go1.26.4`  |
| 6 | M4       | 隐含"无依赖冲突"               | 改为"需脚手架 go mod tidy 验证"             |
| 7 | M5       | tunnel 仅错误提示               | 增加 SupportsRsd 检测 + 友好提示 + doctor   |
| 8 | m2       | 无错误分类                      | 新增 appstore/device errors.go              |
| 9 | m3       | 无诊断命令                      | 新增 cli/doctor.go + internal/doctor/       |
| 10| m4       | appstore/device 文件过细        | 合并为 client.go + apps.go                  |
| 11| m5       | config 可能存 IPA 清单          | 明确清单归 library                          |
