# Design — ios-device-manage

> 配套：[`requirements.md`](./requirements.md)（已验收，单一事实源）｜[`e2e_test.md`](./e2e_test.md)

## 1. Goals & Non-Goals

### Goals（本设计满足的 user stories）

- **US-01 device list**：列举已连接设备（UDID/Name/iOS版本/连接类型），iOS 17+ 经 usbmuxd 仍可见（AC-07-1）。
- **US-02 install（library 推送）**：把 profile library 中的 IPA 推到设备。
- **US-03 install 自动下载**：library 缺 IPA 时复用 download mission 流程自动下载再推。
- **US-04 uninstall**：卸载设备 app（二次确认）。
- **US-05 device apps**：列举设备 user app。
- **US-06 多设备选择**：`--udid` / 单台自动 / 多台交互 / 0台报错 / 非TTY多台报错。
- **US-07 iOS 17+ tunnel**：服务操作缺 tunnel → `ErrTunnelRequired` 可操作错误，绝不自动 sudo。
- **US-08 `--latest`**：强制刷新 App Store 最新版（update 用例，合并进 install）。
- **US-09 `--profile`**：仅 install 接受（涉及 library+凭据）。
- **US-10 `--version`**：library 多版本时指定装哪个。

### Non-Goals（约束设计的边界，来自 requirements §3）

- 不做批量多设备（`--all`）、不做任意文件路径安装、不做 pairing/trust、不做 `device update` 独立命令、不增强 doctor、不做设备端完整性校验、不自动 sudo。

## 2. Architecture Overview

### 2.1 组件与职责

```
┌─────────────────────────────────────────────────────────────────┐
│  cmd/ipa-manager/main.go  →  cli.Execute()                       │
├─────────────────────────────────────────────────────────────────┤
│  internal/cli/   (cobra 命令树 + 编排)                            │
│    device.go         device 命令组 (list/apps/install/uninstall)  │
│    device_install.go install 编排 (resolve→source→push)           │
│    device_helpers.go resolveDevice / resolveProfile (复用)         │
│    download_core.go  共享下载核心 (downloadToLibrary)  ← 重构提取  │
│    app_download.go   app download (委托 download_core)            │
├─────────────────────────────────────────────────────────────────┤
│  internal/device/   (go-ios adapter — go-ios 类型止步于此)        │
│    service.go        Service 接口 + DeviceInfo 类型               │
│    backend.go        Backend 接口 (go-ios 调用契约)               │
│    backend_impl.go   生产实现 (调用真实 go-ios)                    │
│    apps.go           InstalledApp 类型 (已存在，保留)              │
│    errors.go         ErrTunnelRequired (已存在，保留)              │
│    tunnel.go         tunnel 检测 (错误翻译)                        │
├─────────────────────────────────────────────────────────────────┤
│  既有包（复用，不改契约）                                          │
│    internal/appstore/  ProfileAppStore + AppStoreFactory          │
│    internal/library/   Store (per-profile IPA + 多版本)            │
│    internal/account/   Store / Profile                             │
│    internal/ui/        Prompter (+ SelectDevice 新增)              │
│    internal/apperr/    sentinel errors (+ device 新增)             │
└─────────────────────────────────────────────────────────────────┘
```

**分层不变量**：go-ios 类型（`ios.DeviceEntry` / `installationproxy.AppInfo` / `zipconduit.Connection`）**仅出现在 `internal/device/backend_impl.go`**。CLI 层只见 `device.Service` 接口与 `device.DeviceInfo` / `device.InstalledApp` 我们自己的类型（NFR-06）。这与 appstore adapter 模式对称（ipatool 类型止步于 `internal/appstore/`）。

### 2.2 关键设计决策（DD）

#### DD-01 — device.Service 接口 + Backend 注入（可测试性）

复用项目既有 DI 模式（appstore ProfileAppStore+Factory、library Store、account Store 均经 `cli.Deps` 注入）。device 包暴露：

```go
// internal/device/service.go
package device

// DeviceInfo 是我们的设备摘要（无 go-ios 类型泄露）。
type DeviceInfo struct {
    UDID           string // ios.DeviceEntry.Properties.SerialNumber
    Name           string // lockdown DeviceName；空串表示未取到（untrusted / tunnel 缺）
    IOSVersion     string // lockdown ProductVersion；空串同上
    ConnectionType string // USB / Network（ConnectionTypeLabel）
    NeedsTunnel    bool   // device.SupportsRsd()（iOS 17+ 服务操作需 tunnel）
}

// Service 是 CLI 层依赖的设备操作接口（编译时由 backendService 实现）。
type Service interface {
    ListConnected() ([]DeviceInfo, error)
    ListInstalledApps(udid string) ([]InstalledApp, error)
    Install(udid, ipaPath string) error
    Uninstall(udid, bundleID string) error
}
```

`Backend` 接口（device 包内部，封装 go-ios 调用，方法签名含 go-ios 类型）：

```go
// internal/device/backend.go（包内，不导出给 CLI）
type Backend interface {
    ListDeviceEntries() ([]ios.DeviceEntry, error)
    GetDeviceEntry(udid string) (ios.DeviceEntry, error)
    GetValues(entry ios.DeviceEntry) (ios.GetAllValuesResponse, error) // lockdown enrichment
    SendFile(entry ios.DeviceEntry, ipaPath string) error              // zipconduit
    BrowseUserApps(entry ios.DeviceEntry) ([]installationproxy.AppInfo, error)
    UninstallApp(entry ios.DeviceEntry, bundleID string) error
    SupportsRsd(entry ios.DeviceEntry) bool
}
```

生产实现 `defaultBackend{}` 直接调用 go-ios；`device.NewService(backend Service)` 允许测试注入 mock Backend。**CLI 测试**则通过 `cli.Deps.DeviceService` 注入 mock `device.Service`（更外层，不触及 go-ios 类型）。

> 理由：双层 mock 点——CLI 测试 mock `device.Service`（行为级），device 包自身测试 mock `Backend`（go-ios 调用级）。与 appstore 包结构对称（appstore 有 ProfileAppStore 接口给 CLI、内部 adapter 调 ipatool）。

#### DD-02 — iOS 17+ tunnel 检测（错误翻译，非主动探测）

不主动探测 tunnel 进程状态（脆弱、平台相关）。改用**错误翻译**：服务操作（Install/ListInstalledApps/Uninstall）调用 go-ios 的 `zipconduit.New` / `installationproxy.New` 时，对 RSD 设备会走 `ConnectToShimService`，缺 tunnel 时 go-ios 返回（connect.go:116 实证）：

```
ConnectToShimService: Cannot connect to ..., missing tunnel address and RSD port.
To start the tunnel, run `ios tunnel start`
```

`internal/device/tunnel.go` 翻译逻辑：

```go
// isTunnelMissing 检测 go-ios 服务错误是否表明缺 tunnel。
func isTunnelMissing(err error, entry ios.DeviceEntry) bool {
    if err == nil { return false }
    if entry.SupportsRsd() { return true }                 // RSD 设备服务失败 → 默认 tunnel 问题
    return strings.Contains(err.Error(), "missing tunnel address") // 兜底字符串匹配
}

// wrapServiceError 把 go-ios 服务错误翻译为我们的语义。
func wrapServiceError(err error, entry ios.DeviceEntry) error {
    if isTunnelMissing(err, entry) { return ErrTunnelRequired }
    return err // 非 tunnel 错误原样上浮（trust/not-installed/网络等）
}
```

`device.Service` 的 Install/ListInstalledApps/Uninstall 实现均 `defer` 此翻译。CLI 层 `errors.Is(err, device.ErrTunnelRequired)` → 打印 `"iOS 17+ tunnel required; run: sudo ios tunnel start"`（AC-07-2/3）。

> 替代方案（否决）：主动调用 `tunnel` 包探测。否决理由：go-ios tunnel API 面向"启动 tunnel"（需 sudo），无干净的"只读探测"入口；错误翻译更稳、零提权。

#### DD-03 — 设备选择在 CLI 层（resolveDevice helper）

设备选择逻辑（AC-06-1~5）放 CLI 层 `resolveDevice`，因为它涉及交互提示（ui.Prompter）与非 TTY 判断——这些都是 CLI 关注点，不应下沉到 device 包。

```go
// internal/cli/device_helpers.go
// resolveDevice 解析目标设备 UDID（AC-06-1~5）。
//   0 台 → "no connected device..." 错误
//   --udid 未连 → "device '<id>' not connected" 错误
//   单台 → 自动选中
//   多台 + TTY → deps.UI.SelectDevice 交互
//   多台 + 非TTY → "multiple devices connected; specify --udid (non-interactive mode)" 错误
func resolveDevice(deps Deps, udidFlag string) (string, error)
```

`ui.Prompter` 新增方法（避免 ui→device 依赖环）：

```go
// internal/ui/prompter.go
type DeviceOption struct {
    UDID  string
    Label string // "iPhone 15 Pro (iOS 17.5) — a1b2…"
}
SelectDevice(options []DeviceOption) (string, error)
```

CLI 把 `[]device.DeviceInfo` 映射为 `[]ui.DeviceOption`（Label 含 Name/版本/UDID 缩写）。

> AC-06-3 "选取消 → cancelled exit 0"：SelectDevice 返回特殊错误或空串+sentinel；CLI 翻译为 cancelled。具体：SelectDevice 在用户取消时返回 `("", errCancelled)`，CLI 检测后打印 cancelled exit 0。

#### DD-04 — install 编排：共享 downloadToLibrary（复用 download mission）

`app download` 的 `runDownload`（app_download.go）封装了完整下载编排（profile→AppStore→AccountInfo→Lookup→skip→Download→handleDownloadError→ReplicateSinf→register index）。`device install` 的自动下载（US-03）与 `--latest`（US-08）需要同一编排。

**重构提取** `internal/cli/download_core.go`：

```go
// downloadToLibrary 下载（或确认已有）IPA 到 profile library 并返回其 Entry。
//   latest=true  : 查 AppStore 最新版；library 已有同版本 → 返回现有（AC-08-2）；
//                  否则下载新版（作为新版本条目，保留旧版）→ 返回新 Entry（AC-08-1）。
//   latest=false : 标准下载（AC-03 系列，含 license/token 重试）。
// profile 必须（已由调用方 resolveProfile 解析；此函数内构造 AppStore 故需凭据）。
func downloadToLibrary(deps Deps, out io.Writer, profile account.Profile,
    bundleID string, latest bool) (library.Entry, error)
```

`app download` 的 `runDownload` 重构为委托 `downloadToLibrary`（保持其 stdout/副作用不变，测试应全绿——NFR-07）。`device install` 在需要下载时直接调用 `downloadToLibrary`。

> 版本比较安全性（AC-10-2 已避免语义比较）：`latest=true` 的"library 是否已有最新版"用**精确字符串匹配** `entry.Version == appStoreApp.Version`（非语义比较），安全。

#### DD-05 — `--profile` 仅 install 注册（cobra 自动拒绝其余）

仅 `device install` cmd 注册 `--profile` flag。`device list`/`apps`/`uninstall` 不注册 → 用户传 `--profile` 时 cobra 自动报 `unknown flag: --profile` exit 1（AC-09-5），无需额外代码。与 download/library 的 `--profile` 语义对齐（仅那些触及 library/凭据的命令注册）。

#### DD-06 — 统一 device 命令组；移除 stub

- **删除** `internal/cli/devices.go`（顶层 `devices` 单命令 stub）。
- **删除** `internal/cli/install.go`（`install` 组 + push/uninstall/update stub）。
- **新建** `internal/cli/device.go`（`device` 命令组，子命令 list/apps/install/uninstall）。
- **修改** `internal/cli/root.go`：`devicesCmd()` + `installCmd()` → `deviceCmd(deps)`。
- **保留** `internal/device/apps.go` 的 `InstalledApp` 类型、`errors.go` 的 `ErrTunnelRequired`；其余 stub 函数被 `service.go` 取代。

> 零回归风险：被删的 CLI 命令全是 stub（"not yet implemented"），无测试覆盖其行为。

#### DD-07 — 非交互检测复用 checkInteractive

复用既有包级变量 `var checkInteractive = appstore.IsInteractive`（app_download.go:213，测试可覆盖）。`device uninstall` 确认（AC-04-4）与多设备选择非TTY拒绝（AC-06-4）均用它。保持单点。

#### DD-08 — Deps 扩展

```go
// internal/cli/deps.go
type Deps struct {
    // …既有字段…
    DeviceService device.Service // 新增（生产: device.NewService(defaultBackend{})）
}
```

`newProductionDeps` 增加 `DeviceService: device.NewService(device.NewDefaultBackend())`。

## 3. Data Models, State & Interfaces

### 3.1 新增/修改类型

| 位置 | 类型 | 说明 |
|------|------|------|
| `device.DeviceInfo` | struct（新） | UDID/Name/IOSVersion/ConnectionType/NeedsTunnel；无 go-ios 类型 |
| `device.InstalledApp` | struct（已存在） | BundleID/Name/Version；保留 |
| `device.Service` | interface（新） | ListConnected/ListInstalledApps/Install/Uninstall |
| `device.Backend` | interface（新，包内） | go-ios 调用契约（含 go-ios 类型） |
| `device.ErrTunnelRequired` | sentinel（已存在） | 保留；CLI errors.Is 检测 |
| `ui.DeviceOption` | struct（新） | UDID/Label；SelectDevice 入参 |
| `ui.Prompter.SelectDevice` | method（新） | 设备选择提示 |
| `cli.Deps.DeviceService` | field（新） | device.Service 注入 |
| `apperr.ErrDeviceNotConnected` / `ErrAppNotInstalled` / `ErrMultipleDevices` | sentinels（新） | 设备侧可操作错误（供 errors.Is + NFR-04 文案统一） |

### 3.2 状态与持久化

**无新增持久化状态**。本 mission 全部设备操作是只读/瞬时的（list/apps/uninstall）或基于既有 library（install 读 library.Entry.FilePath）。library 索引更新发生在 `downloadToLibrary` 内（复用既有 `LibraryStore.Add`），无新存储机制。

### 3.3 install 决策树（核心状态流转）

```
resolve profile (requireCredentials=false)  ← AC-09-2: cached 推送无需凭据
resolve device (udidFlag)                    ← AC-06-*

switch {
  case --latest && --version:  return Err 互斥 (AC-10-4)
  case --latest:
     require creds; entry = downloadToLibrary(profile, bid, latest=true)  ← AC-08-1/2
  case --version v:
     entry, err = LibraryStore.GetVersion(profile.ID, bid, v)             ← AC-10-1/3
  default:
     entries = LibraryStore.Get(profile.ID, bid)
     if len(entries)>0: entry = mostRecentByDownloadedAt(entries)         ← AC-10-2
     else:               require creds; entry = downloadToLibrary(profile, bid, latest=false) ← AC-03-1
}

err = DeviceService.Install(udid, entry.FilePath)                          ← AC-07-2 tunnel / AC-02-9 push
  → errors.Is(ErrTunnelRequired): print tunnel hint, exit 1
  → other err: surface with NFR-04 actionable text, exit 1
report success (device name + app + version)
```

`require creds`：当路径需要下载时，若 profile 未登录，`downloadToLibrary` 内构造 AppStore + AccountInfo 失败 → `"profile '<id>' has no credentials"`（AC-03-3 / AC-09-3）。

## 4. Code Structure

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/device/service.go` | `Service` 接口 + `DeviceInfo` + `NewService(backend)` + `backendService` 实现（含 tunnel 翻译） |
| `internal/device/backend.go` | `Backend` 接口（包内） |
| `internal/device/backend_impl.go` | `defaultBackend{}` 调真实 go-ios（ListDevices/GetValues/zipconduit/installationproxy） |
| `internal/device/tunnel.go` | `isTunnelMissing` / `wrapServiceError` |
| `internal/device/service_test.go` | Service 行为测试（mock Backend），覆盖 tunnel 翻译/list/apps/install/uninstall |
| `internal/cli/device.go` | `device` 命令组 + 4 子命令构造 |
| `internal/cli/device_install.go` | install 编排（决策树 §3.3 + 调 downloadToLibrary） |
| `internal/cli/device_helpers.go` | `resolveDevice`（AC-06） |
| `internal/cli/download_core.go` | `downloadToLibrary`（从 app_download.go 重构提取） |
| `internal/cli/device_test.go` | device 命令 E2E（mock DeviceService + 既有 mockStore/AppStore/Library） |
| `internal/ui/prompt.go`（改） | `SelectDevice` huh 实现 |

### 修改文件

| 文件 | 改动 |
|------|------|
| `internal/cli/root.go` | 注册 `deviceCmd(deps)` 替代 `devicesCmd()`+`installCmd()` |
| `internal/cli/deps.go` | `Deps` 加 `DeviceService device.Service`；`newProductionDeps` 构造之 |
| `internal/cli/app_download.go` | `runDownload` 委托 `downloadToLibrary`（行为不变） |
| `internal/device/apps.go` | 删除 stub `ListInstalledApps/Install/Uninstall`（被 Service 取代），保留 `InstalledApp` 类型 |
| `internal/device/client.go` | 删除 stub `ListConnectedDevices`（被 Service 取代） |
| `internal/apperr/errors.go` | 加 device sentinels（ErrDeviceNotConnected/ErrAppNotInstalled/ErrMultipleDevices） |

### 不变文件（含理由）

- `internal/appstore/*`、`internal/library/*`、`internal/account/*`：契约复用，不改。
- `internal/cli/auth.go`/`account.go`/`app.go`(search)/`library.go`：与本 mission 无交集。
- `internal/device/errors.go`：`ErrTunnelRequired` 已就位，保留。

## 5. Processing Flows

### 5.1 device list（happy）

```
[device list]
  → DeviceService.ListConnected()
     → Backend.ListDeviceEntries() (ios.ListDevices, usbmuxd)
     → for each entry: best-effort Backend.GetValues(entry) (lockdown)
         成功 → 填 Name/IOSVersion；NeedsTunnel=SupportsRsd
         失败 → Name/IOSVersion="" (untrusted 或 iOS17+缺tunnel)
  → 0 台: "no connected device" exit 0 (AC-01-2)
  → ≥1 台: 表格输出 (UDID/Name/iOS Version/Connection Type) exit 0 (AC-01-1/3)
```

**失败路径**：usbmuxd 不可用（go-ios 连接失败）→ surface 错误 exit 1。单设备 lockdown 失败不影响其他设备列举（best-effort，per-device try）。

### 5.2 device apps（happy + tunnel）

```
[device apps [--udid id]]
  → udid = resolveDevice(deps, --udid)         (AC-06-*)
  → DeviceService.ListInstalledApps(udid)
     → Backend.GetDeviceEntry(udid) → entry
     → svc, err = installationproxy.New(entry)
         err + SupportsRsd → ErrTunnelRequired (AC-07-3)
     → svc.BrowseUserApps() → []AppInfo
     → map → []InstalledApp
  → 0 app: "no user apps installed on device '<name>'" exit 0 (AC-05-2)
  → ≥1: 表格 (Bundle-ID/Name/Version) exit 0 (AC-05-1)
```

### 5.3 device install（核心，happy：library 有 / happy：自动下载 / failure）

```
[device install <bid> [--profile id] [--udid id] [--latest] [--version v]]
  ① resolve profile (requireCredentials=false)           AC-09-1/2
  ② resolve device (udid)                                AC-06-*
  ③ 决策 IPA 来源 (§3.3 决策树):
       --latest:    downloadToLibrary(latest=true)       AC-08-1/2  [需 creds]
       --version v: LibraryStore.GetVersion              AC-10-1/3
       default+有:  mostRecentByDownloadedAt             AC-10-2
       default+无:  downloadToLibrary(latest=false)      AC-03-1    [需 creds]
  ④ DeviceService.Install(udid, entry.FilePath):
       zipconduit.New(entry) → RSD+tunnel缺 → ErrTunnelRequired  AC-07-2
       SendFile(ipaPath) → 设备端接受/拒绝(降级等) 上浮       AC-02-9
  ⑤ 成功: "✓ Installed <app> <ver> → <device>" exit 0
```

**失败路径**：
- profile 无 creds 且需下载 → AC-03-3/AC-09-3 "no credentials" exit 1
- bundle-id 不在 App Store（自动下载路径）→ AC-03-4 "app not found" exit 1
- license required（自动下载）→ AC-03-5 交互授权 / AC-03-6 非TTY报错
- iOS 17+ 缺 tunnel → AC-07-2 tunnel hint exit 1
- 设备未信任 → AC-02-7 go-ios trust 错误 + 提示 exit 1
- `--latest`+`--version` 同传 → AC-10-4 互斥错误 exit 1

### 5.4 device uninstall（happy + 确认 + failure）

```
[device uninstall <bid> [--udid id]]
  ① udid = resolveDevice(deps, --udid)                   AC-06-*
  ② 非TTY → "confirmation required..." exit 1            AC-04-4
  ③ UI.Confirm "uninstall '<bid>' from device '<name>'?" 
       no → "cancelled" exit 0                           AC-04-1
  ④ DeviceService.Uninstall(udid, bid):
       installationproxy.New → RSD+tunnel缺 → ErrTunnelRequired  AC-07-3
       svc.Uninstall(bid) → 未装 → ErrAppNotInstalled     AC-04-3
  ⑤ 成功: "✓ Uninstalled <bid> from <device>" exit 0
```

### 5.5 resolveDevice（设备选择，横切）

```
[resolveDevice(deps, udidFlag)]
  devices = DeviceService.ListConnected()
  len==0 → Err "no connected device; connect a device and trust this Mac"  AC-06-1
  udidFlag!="":
     命中 → return udid                                    AC-06/--udid 选中
     未命中 → Err "device '<id>' not connected"            AC-06-2
  len==1 → return devices[0].UDID (自动)                   AC-06-5
  len>1 + TTY    → UI.SelectDevice(options)               AC-06-3
  len>1 + 非TTY  → Err "multiple devices...specify --udid" AC-06-4
```

## 6. Impact Analysis

### 6.1 兼容性风险

- **CLI 命令树变更**：移除顶层 `devices` + `install` 组（stub，无用户依赖）→ 统一 `device` 组。无破坏性（被删命令从未可用）。**风险低**。
- **go-ios API 稳定性**：依赖 go-ios v1.2.0 的 `ListDevices`/`GetValues`/`zipconduit.New`/`installationproxy.New`。go-ios 是活跃项目，API 可能演进 → Backend 接口隔离使变更限于 `backend_impl.go`（与 appstore 对 ipatool 的隔离策略一致）。**风险中**，已隔离。

### 6.2 迁移需求

**N/A**。无持久化状态变更；无既有用户数据；library 索引格式不变（install 复用既有 Add）。

### 6.3 安全/隐私

- **绝不自动 sudo / 不启动 tunnel**（NFR-03，DD-02）：tunnel 检测纯错误翻译，零提权调用。源码审计执行路径无 `exec.Command("sudo"...)` / 无 `tunnel` 包 import 于执行路径。
- **凭据不泄露**：install 自动下载复用 download mission 的凭据处理（AccountInfo 不暴露 Password/Token，NFR-04 继承）。device 操作本身不触及凭据。

### 6.4 性能/可靠性

- **device list 性能**（NFR-08）：usbmuxd 枚举 < 100ms；lockdown enrichment per-device（best-effort，串行）。多设备时延迟 = N × lockdown RTT。**风险低**（个人工具通常 1-2 台）。若需优化，future 可并发 lockdown。
- **tunnel 检测时延**（NFR-01）：服务操作缺 tunnel 时 go-ios 快速返回错误（不挂起），< 5s exit 1。
- **install 失败边界**（NFR-02）：preflight（resolve profile/device/library、tunnel 检测）在设备写入前；中途传输失败诚实上浮，不声称设备端回滚。

### 6.5 可观测性

- 错误文案统一可操作（NFR-04）：每条错误含 cause + suggestion（tunnel hint / trust hint / login hint）。复用 `apperr` sentinel + CLI 层 errors.Is 分支格式化（与 download/library 同源，NFR-10）。
- 成功消息含设备名 + app + 版本（可确认操作生效）。

### 6.6 回滚

- 单 feature 分支 `feature/ios-device-manage`；不合入 main 前可整体回退。
- CLI 命令树变更若需回滚：恢复 `devices.go`/`install.go`（git revert）。低风险（被删为 stub）。

## 7. Validation Strategy 概述

详见 [`e2e_test.md`](./e2e_test.md)。要点：

- **测试分层**：CLI E2E（mock `device.Service` + 既有 mockStore/AppStore/Library）覆盖全部 AC 的可观测行为；device 包单测（mock Backend）覆盖 go-ios 调用 + tunnel 翻译。
- **spec → cases 单向流**：E2E case 从 requirements AC 派生，不反向从实现推导。
- **live 设备验收**：validate 阶段手动（真实 iOS 设备 install/apps/uninstall + iOS 17+ tunnel 场景），不在自动化测试范围（无 CI 设备）。
- **NFR-03 审计**：grep 执行路径无 sudo/exec 提权。
- **NFR-06 隔离审计**：`grep -r "danielpaulus/go-ios" internal/cli` 无结果。
- **NFR-07 无回归**：`go test ./... -count=1` 全绿（含前三 mission 测试）。

## 8. 决策追溯

| 决策 | 选择 | 否决的替代 | 理由 |
|------|------|-----------|------|
| device 包可测试性 | Service 接口 + Backend 注入（DD-01） | 包级全局变量 | 与项目 DI 模式一致；无全局状态；双层 mock |
| tunnel 检测 | 错误翻译（DD-02） | 主动探测 tunnel 进程 | 主动探测需 sudo/平台相关；错误翻译零提权更稳 |
| 设备选择位置 | CLI 层 resolveDevice（DD-03） | 下沉 device 包 | 交互/非TTY 是 CLI 关注点；device 包保持纯设备语义 |
| download 复用 | 提取 downloadToLibrary（DD-04） | install 内联重写 | DRY + 行为一致 + 降低 install/license/token 逻辑漂移 |
| `--profile` 范围 | 仅 install（DD-05） | 全 device 命令 | 设备只读/卸载与账号无关；避免误导 |
| version 比较 | 精确字符串匹配（DD-04） | 语义版本比较 | AC-10-2 已定避免不可靠比较；exact match 安全 |
