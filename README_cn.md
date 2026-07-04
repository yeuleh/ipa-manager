# ipa-manager

[English](README.md) | **简体中文**

一个 macOS 命令行工具，用于跨**多个 Apple 账号**管理 iOS 应用（`.ipa`）的完整生命周期：登录 / 切换账号、按账号隔离下载与管理、推送到 iOS 设备进行安装 / 更新。

## 为什么需要

有些应用只在某些 App Store 区域上架，从不进入全球目录。当你的 iOS 设备已经登录了一个 Apple ID 时，想安装其他区的应用，通常得在设备的 App Store 设置里切换账号——既打断使用、又慢，还容易搞不清哪个购买归属于哪个账号。`ipa-manager` 把这一切都省掉了：它在你的 Mac 上管理多个 Apple 账号，从对应区域的目录下载每个应用，再通过 USB 推送到设备上安装——设备本身登录的那个账号始终不用动。底层它把 `ipatool`（登录 / 搜索 / 下载）和 `go-ios`（设备安装）作为库编排在一起，并加上了多账号隔离这一核心价值层。

## 状态

核心命令已实现并可用——账号登录 / 切换、App Store 搜索 / 下载、按账号隔离的本地 IPA 库、以及经 usbmuxd 的设备安装 / 卸载。少数子命令仍是占位 stub（`app versions`、`doctor`）。完整设计思路见 `docs/bootstrap/`。

## 工作原理

- **账号侧** —— [`ipatool`](https://github.com/majd/ipatool)（MIT，Go）负责 Apple ID 登录 / 2FA / App Store 搜索 / `.ipa` 下载，作为库直接 import。本项目使用一个持续维护的 fork（`yeuleh/ipatool/v2`，`fix-auth` 分支）以修复认证问题。
- **设备侧** —— [`go-ios`](https://github.com/danielpaulus/go-ios)（MIT，Go）负责设备安装 / 列举 / 卸载，走 `usbmuxd`（macOS 自带的 USB 多路复用守护进程——无需额外安装、无需 tunnel），作为库直接 import。
- **多账号隔离** —— 每个账号 profile 把自己的 Apple ID 凭据存在独立的 Keychain 命名空间中，多个账号互不干扰。（实现细节：`account.ProfileKeychain` 复用 ipatool 固定的 `"account"` key 并按 profile 命名空间化；见 `docs/bootstrap/decisions/0002-multi-account-isolation.md`。）

> ⚠️ 已知风险：ipatool 依赖 Apple 私有 API（Apple 改服务端时会临时失效，需等待上游修复）。设备操作（安装 / 列举应用 / 卸载）经 usbmuxd 完成，无需任何 tunnel 或 `sudo`。

## 快速上手

### 前置条件

- macOS
- Go **1.26+**（`brew install go`）
- （可选，用于设备操作）一台已配对的 iOS 设备，通过 USB 连接

### 构建与运行

```bash
make build              # → bin/ipa-manager
./bin/ipa-manager --help
./bin/ipa-manager --version
```

下文示例直接使用 `ipa-manager`——你可以设置别名，或把 `./bin/` 加入 `PATH`：

```bash
alias ipa-manager=./bin/ipa-manager
```

或者直接从源码运行：

```bash
go run ./cmd/ipa-manager --help
```

### 快速开始

```bash
# 1. 登录一个 Apple ID（会创建 profile，并处理 2FA）
./bin/ipa-manager auth login

# 2. 在 App Store 搜索
./bin/ipa-manager app search "应用名"

# 3. 把 IPA 下载到该账号的隔离库中
./bin/ipa-manager app download <bundle-id>

# 4. 用 USB 连上 iOS 设备，装上去
./bin/ipa-manager device install <bundle-id>

# 5. 再加一个账号，随时在多个账号间切换
./bin/ipa-manager auth login          # 另一个 Apple ID
./bin/ipa-manager accounts list       # 查看所有 profile
./bin/ipa-manager accounts use <id>   # 切换当前账号
```

### 常用命令

```bash
# 账号
ipa-manager auth login                  # 登录 Apple ID（创建/刷新 profile，处理 2FA）
ipa-manager auth logout [profile-id]    # 撤销凭据（默认针对当前 profile）
ipa-manager accounts list               # 列出已配置的 profile 及状态
ipa-manager accounts use <profile-id>   # 切换当前账号（严格：必须已登录）
ipa-manager accounts remove <id>        # 删除 profile 并撤销凭据（需确认）

# App Store（当前 profile）
ipa-manager app search <term>           # 搜索 App Store
ipa-manager app download <bundle-id>    # 下载应用 IPA 到 profile 库

# 本地库（按 profile 隔离）
ipa-manager library list                # 列出当前 profile 已下载的 IPA
ipa-manager library clean [bundle-id]   # 删除已下载的 IPA

# 设备（经 usbmuxd，无需 tunnel / sudo）
ipa-manager device list                 # 列出已连接的 iOS 设备
ipa-manager device apps                 # 列出设备上用户安装的应用
ipa-manager device install <bundle-id>  # 安装应用到设备（缺失时自动下载；需要账号登录）
ipa-manager device uninstall <bundle-id># 从设备卸载应用
```

> 运行 `ipa-manager <命令> --help` 查看所有可用 flag。
> `app versions` 和 `doctor` 尚未实现。

### 已知限制

- **不支持并发访问**：在**同一 profile** 上同时运行多个 ipa-manager 命令（例如在不同终端窗口同时跑 `auth login` 和 `accounts remove`）可能损坏状态。行为**未定义**，且不被测试覆盖。请按顺序运行命令。
- **没有 `accounts add` 命令**：使用 `auth login` 来添加新账号。
- **仅支持 macOS**：凭据存储依赖 macOS Keychain。

### 开发

```bash
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -s -w .
make tidy      # go mod tidy
make lint      # golangci-lint run（若已安装）
```

> 构建需要开启 CGO（macOS Keychain 后端）。`make build` / `go build` 默认已正确处理。

## 项目结构

```
cmd/ipa-manager/      入口
internal/
  cli/                cobra 命令树
  account/            profile + ProfileKeychain（多账号隔离）
  appstore/           ipatool adapter
  device/             go-ios adapter
  library/            按账号隔离的本地 .ipa 库
  config/             路径 + 全局配置
  doctor/             健康检查
  ui/                 huh 交互提示 + lipgloss 表格
docs/                 设计文档（bootstrap、decisions）
```

## 许可证

个人工具。底层库均为 MIT（`ipatool`、`go-ios`）。
