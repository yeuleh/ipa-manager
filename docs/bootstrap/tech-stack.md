# Tech Stack — ipa-manager

<!-- mindtrek-bootstrap:start -->
## 已锁定的核心栈（Stage 1 决定）

这些在前面的需求讨论中已通过研究验证并锁定，Stage 2 不再变更：

| 层级       | 选型                                                                    | 理由                                                                          |
| ---------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| 语言       | **Go** (≥ 1.26)                                                          | ipatool 与 go-ios 均为 Go 且可代码级 import；go-ios 模块要求 Go 1.26            |
| 账号/下载库 | `github.com/majd/ipatool/v2` (MIT) — import `pkg/appstore`               | Apple ID 登录/2FA/搜索/下载，9.5k⭐ 活跃维护，`AppStore` 接口干净可注入          |
| 设备操作库 | `github.com/danielpaulus/go-ios` (MIT) — import `ios/installationproxy` 等 | 设备安装/列举/卸载/配对，2.1k⭐，Sauce Labs 生产使用，纯 Go 无 GPL                |
| 凭据存储   | 复用 ipatool 的 `99designs/keyring`（macOS Keychain 后端）                | 不自造密码存储轮子；ipatool 已抽象好                                           |

**集成方式**：代码级 `import`（非 subprocess），保证单二进制、零外部进程依赖。

## Stage 2 结构选择（TUI 重量 / 交互框架）

### 提出的三个组合

#### 组合 A — 精简 CLI
- `spf13/cobra` 命令分发 + `charmbracelet/lipgloss` 彩色输出 + 交互提示库（promptui 或 huh，Stage 3 定）
- 配置：`~/.ipa-manager/config.json` + keyring
- 结构：`cmd/` + `internal/`
- 最快出活、可脚本化；UX"好用"非"惊艳"

#### 组合 B — TUI 优先
- `cobra` + `charmbracelet/bubbletea`（全屏 TUI）+ `bubbles` + `lipgloss`
- 账号/设备选择列表、进度条体验一流
- 代价：多学 Elm 架构、代码量更大

#### 组合 C — Charm 折中
- `cobra` + `lipgloss` + `charmbracelet/huh`（表单式交互）
- 介于 A、B 之间

### 最终选择：**组合 A（精简 CLI）**

**用户决策理由**：先以最小成本把工具跑起来；交互框架的"惊艳"体验（组合 B）留作未来升级路径。

**这意味着 v1 的交互范式**：
- 命令式（`ipa accounts` / `ipa use <name>` / `ipa get <bundleid>` / `ipa install`）
- 需要选择时（如切换账号、选设备）用交互式 prompt
- 输出用 lipgloss 着色 / 表格化
- 保留可脚本化能力（非交互 flag）

## 待 Stage 3 研究细化的层-2 选型

以下属"层-2 工具链"，留给 Stage 3 用外部工具查证当前最佳实践后再定：
- 交互提示库（promptui vs huh vs 其他）的当前维护状态
- Go 项目目录结构惯例（Standard Go Project Layout vs 扁平）
- 测试框架（标准 testing + testify？）
- lint / formatter（golangci-lint 配置）
- 构建分发（`go build` 起步，goreleaser 视后续需要）
<!-- mindtrek-bootstrap:end -->
