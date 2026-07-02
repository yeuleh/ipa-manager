# Plan — ios-device-manage

> 事实源：[`requirements.md`](./requirements.md)（已验收）｜[`design.md`](./design.md) + [`e2e_test.md`](./e2e_test.md)（已验收）。本 plan 从 design 的文件结构与处理流程派生任务，不发明新架构/接口/任务序。

## 1. Implementation Context

| 项 | 值 |
|----|----|
| 语言/运行时 | Go ≥ 1.26（与既有 mission 一致） |
| 核心依赖 | `github.com/danielpaulus/go-ios v1.2.0`（设备侧，代码级 import）；复用既有 ipatool fork（下载侧）、account/library/appstore/ui/apperr 包 |
| 测试框架 | 标准 `testing` + `testify`；CLI E2E（mock `device.Service`）；device 包单测（mock `Backend`）；既有 mockStore/mockAppStore/mockLibraryStore 复用 |
| 平台 | macOS only（go-ios 设备后端 + Keychain） |
| 关键约束 | NFR-03 绝不 sudo（允许只读 TunnelInfoForDevice）；NFR-06 go-ios 类型止步 `internal/device/`；NFR-07 零回归（不改 app_download.go 契约） |
| 重要非目标 | 不做批量多设备、不做任意文件路径安装、不做 pairing/trust、不增强 doctor、不做设备端完整性校验 |

### Staged Interface 策略（保证每 task `go build` 绿）

`device.Backend` 接口**逐任务扩展**——每个 task 只向 Backend/Service/连接接口**添加它实际实现的方法**，不预声明未实现方法（Go 接口未实现方法会编译失败）。各 task 末态的 Backend 方法集：

| Task 末态 | Backend 新增方法 | 连接接口 |
|-----------|-----------------|----------|
| T1 | `ListDeviceEntries`、`GetDeviceEntry`、`GetLockdownInfo` | — |
| T2 | `OpenInstallationProxy` | `ProxyConn{ BrowseUserApps, Close }` |
| T3 | `OpenInstaller`、`LookupTunnelInfo` | `InstallerConn{ SendFile, Close }` |
| T5 | —（ProxyConn 扩展） | `ProxyConn` 加 `Uninstall` 方法 |

`device.InstalledApp` 类型**保留在 `internal/device/apps.go`**（仅类型，删 stub 函数）——与 design 一致。

## 2. Dependency Graph

```
T1 (device list + device 包骨架 + 命令树重构)
   │
   ▼
T2 (device apps + resolveDevice + SelectDevice)
   │  创建 resolveDevice（被 install/uninstall 复用）；本任务仅经 apps 验证
   ▼
T3 (device install 推送 + tunnel 分层 + tunnel info)   ←─ 创建 install 命令
   │
   ▼
T4 (device install 自动下载 + --latest)
   │
   ▼
T5 (device uninstall + 确认 + ErrAppNotInstalled)       ←─ 创建 uninstall 命令
```

**串行执行（推荐顺序 T1→T2→T3→T4→T5）**。T3 与 T5 共享 `internal/device/service.go`、`backend_impl.go`、`internal/cli/device.go`、`device_test.go`、`service_test.go`，**不可真正并行**（同文件合并冲突）。单执行者按序最稳。若希望尽早验证 uninstall，可 T1→T2→T3→T5→T4（T5 不依赖 T4）。

**依赖关系**：
- **T1** 无依赖；建立 device 包骨架 + `device` 命令组。阻塞 T2/T3/T4/T5。
- **T2** 依赖 T1；引入 `resolveDevice`+`SelectDevice`+`ErrCancelled`。T3/T5 复用 resolveDevice。
- **T3** 依赖 T1（骨架）+ T2（resolveDevice）；install 命令 + DD-02 tunnel 分层 + tunnel info 复用。
- **T4** 依赖 T3（install 命令已存在）；扩展 auto-download + `--latest`。
- **T5** 依赖 T1（骨架）+ T2（resolveDevice）+ T3（tunnel 分层诊断函数 `diagnoseConnectError`/`resolveEntry` 已就位可复用）；uninstall 命令。

**无独立 Foundation 任务**：T1 既是 story task（US-01 device list）又建立 device 包骨架。Foundation=0 < story=5，合规。

## 3. Task List（垂直切片）

> 每个 task 完成标准统一含：**Spock review pass + 该 task 测试全绿 + `go build ./...` + `go vet ./...` 通过 + `go test ./... -count=1` 零回归**。

### T1 — device list + device 包骨架 + 命令树重构 ✅ COMPLETE

- **US / AC / E2E**：US-01；AC-01-1/2/3、AC-07-1（list 列出 iOS 17+ 设备；NeedsTunnel 作为 **内部字段**计算供 T3 tunnel 诊断用，**非 CLI 显示列**——AC-01-1 固定 4 列）；E2E-001/002/003/004、E2E-090、E2E-074（list 拒 `--profile` 分支）
- **目标**：`device list` 可用；建立 device.Service/Backend/DeviceInfo/defaultBackend 骨架；移除旧 `devices`+`install` stub，统一 `device` 命令组。
- **新建文件**：
  - `internal/device/service.go` — `Service` 接口（仅 `ListConnected()`，余方法后续 task 加）、`DeviceInfo`（UDID/Name/IOSVersion/ConnectionType/NeedsTunnel）、`NewService(backend Backend)`、`backendService.ListConnected()`（Backend.ListDeviceEntries + best-effort GetLockdownInfo → DeviceInfo）
  - `internal/device/backend.go` — `Backend` 接口（**仅** `ListDeviceEntries`/`GetDeviceEntry`/`GetLockdownInfo`）
  - `internal/device/backend_impl.go` — `defaultBackend{}` + `NewDefaultBackend()`：`ListDeviceEntries()`（`ios.ListDevices`）、`GetDeviceEntry(udid)`（`ios.GetDevice`）、`GetLockdownInfo(entry)`（`ios.GetValues` → name+version）
  - `internal/device/tunnel.go` — `isIOS17OrLater(version)`（TrimSpace → split "." → Atoi 首段 → ≥17；空/解析失败→false）
  - `internal/cli/device.go` — `deviceCmd(deps)` 命令组 + `deviceListCmd`（表格 UDID/Name/iOS Version/Connection Type；0 台→"no connected device" exit 0）
  - `internal/device/service_test.go` — ListConnected 映射（mock Backend：lockdown 成功/失败 best-effort、NeedsTunnel=isIOS17OrLater）
  - `internal/cli/device_test.go` — `mockDeviceService`（实现 device.Service）；E2E-001/002/003/004/090、E2E-074 list 分支（cobra unknown flag）
- **修改文件**：
  - `internal/cli/root.go` — 注册 `deviceCmd(deps)` 替代 `devicesCmd()`+`installCmd()`
  - `internal/cli/deps.go` — `Deps` 加 `DeviceService device.Service`；`newProductionDeps` 构造 `device.NewService(device.NewDefaultBackend())`
- **删除文件**：`internal/cli/devices.go`、`internal/cli/install.go`（stub）；`internal/device/client.go`（`ListConnectedDevices` stub）；`internal/device/apps.go` 的 stub 函数（**保留 `InstalledApp` 类型**）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1 && go test ./... -count=1`
- **Rollback note**：删除旧 CLI stub（devices.go/install.go）是破坏性变更但被删为 stub（零用户依赖）；回滚 = git revert T1。root.go 命令树变更同上。

### T2 — device apps + resolveDevice + SelectDevice ✅ COMPLETE

- **US / AC / E2E**：US-05、US-06（**仅经 apps 命令可验证的子集**）；AC-05-1/2/3/4、AC-06-1/2/3/4/5（apps 路径）、AC-09-5（apps 分支）；E2E-010/011/012、E2E-026/028（apps 多设备交互/非TTY）、E2E-020/021（**apps 分支**——0 设备/--udid 未连，经 apps 命令验证）、E2E-074（apps 拒 `--profile` 分支）、E2E-092（**apps tunnel 分支**）。注：AC-06-3/4/5 的"多设备交互/非TTY/单台自动"经 apps 路径补 **supplementary CLI 测试**（由 AC-06 派生，不复用 e2e_test.md 中 E2E-022~025 的 install 用例 ID；那些 ID 在 e2e_test.md 中属 install 命令，归 T3）。
- **目标**：`device apps` 可用；引入 `resolveDevice`（被 install/uninstall 复用）+ `ui.SelectDevice`+`ErrCancelled`。**install/uninstall 的选择路径 E2E 延后到 T3/T5**（命令尚未存在）。
- **新建文件**：
  - `internal/cli/device_helpers.go` — `resolveDevice(deps, udidFlag) (device.DeviceInfo, error)`（AC-06-1~5：0 台/`--udid` 未连/单台自动/多台 TTY 交互/多台非TTY 报错；SelectDevice 返回 ErrCancelled→CLI "cancelled" exit 0）
- **修改文件**：
  - `internal/device/backend.go` — 加 `OpenInstallationProxy(entry) (ProxyConn, error)` + `ProxyConn` 接口（**仅** `BrowseUserApps() ([]installationproxy.AppInfo, error)` + `Close()`；`Uninstall` 方法 T5 加）
  - `internal/device/backend_impl.go` — `OpenInstallationProxy`（`installationproxy.New`）、`ProxyConn` 实现（`BrowseUserApps` → `installationproxy.AppInfo` → `InstalledApp` 映射）
  - `internal/device/service.go` — Service 加 `ListInstalledApps(udid)`（resolveEntry + OpenInstallationProxy + best-effort diagnoseConnectError + BrowseUserApps → InstalledApp）；加 `resolveEntry`（GetDeviceEntry 硬错 + GetLockdownInfo best-effort）；加 `diagnoseConnectError`（tunnel.go 已有 isIOS17OrLater；本 task 在 tunnel.go 补 diagnoseConnectError 供 apps connect 失败用）
  - `internal/cli/device.go` — `deviceAppsCmd`（resolveDevice → ListInstalledApps → 表格/空消息）
  - `internal/ui/prompter.go` — `DeviceOption` 结构、`SelectDevice(options []DeviceOption) (string, error)`
  - `internal/ui/prompt.go` — `SelectDevice` huh 实现（取消→`apperr.ErrCancelled`）
  - `internal/apperr/errors.go` — `ErrCancelled`、`ErrDeviceNotConnected`、`ErrMultipleDevices`
  - `internal/device/tunnel.go` — `diagnoseConnectError(err, version)`（iOS≥17→ErrTunnelRequired；否则原样）
  - 测试：`internal/device/service_test.go`（ListInstalledApps、resolveEntry、diagnoseConnectError 单元）、`internal/cli/device_test.go`（mockPrompter 加 SelectDevice；apps + apps 变体设备选择 E2E）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1 && go test ./... -count=1`
- **Rollback note**：纯增量（resolveDevice/SelectDevice/apps + ErrCancelled 等哨兵）；回滚 = revert。

### T3 — device install（library 推送 + tunnel 分层 + tunnel info 复用）✅ COMPLETE

- **US / AC / E2E**：US-02、US-07、US-09、US-10；AC-02-1/2/3/4/5/6/7/8/9、AC-07-2/4、AC-09-1/2/3/4、AC-10-1/2/3、AC-06-1~5（**install 路径**）；E2E-030/030b/031/032/033/034、E2E-022/023/024/025（install 多设备交互/非TTY/单台自动）、E2E-020/021（**install 分支**）、E2E-091/093/093b/093d、E2E-060/061/062、E2E-070/071/072/073。（AC-10-4 互斥与 E2E-063 归 T4，因 `--latest` 在 T4 注册。）
- **目标**：`device install <bid>`（library 有 IPA 时推送）可用；DD-02 分层 tunnel 检测（connect 失败→ErrTunnelRequired，operate→原样）+ tunnel info 只读复用（iOS 17+ 用户启 tunnel 后 install 闭环）；`--udid`/`--version`/`--profile` flags。**T3 不注册 `--latest`**（T4 注册），故 AC-10-4 互斥校验在 T4 落地。
- **新建文件**：
  - `internal/cli/device_install.go` — install 命令编排（决策树 §3.3 的 **library 有** 路径：resolveProfile(requireCreds=false) → resolveDevice → LibraryStore.Get/GetVersion/mostRecentByDownloadedAt → DeviceService.Install）；`downloadToLibrary` 函数**不在此 task**（T4 实现）
- **修改文件**：
  - `internal/device/backend.go` — 加 `OpenInstaller(entry) (InstallerConn, error)`、`InstallerConn` 接口（`SendFile(ipaPath string) error` + `Close() error`）、`LookupTunnelInfo(udid) (address string, rsdPort int, err error)`
  - `internal/device/backend_impl.go` — `OpenInstaller`（`zipconduit.New`）、`InstallerConn` 实现（`SendFile`→`zipconduit.SendFile`）、`LookupTunnelInfo`（`tunnel.TunnelInfoForDevice`，只读 HTTP）、`withRsdProvider(entry, udid, address, rsdPort)`（参考 go-ios cli_device_resolution.go；复制 entry + 设 Rsd provider，不复制 UserspaceTUN）
  - `internal/device/service.go` — Service 加 `Install(udid, ipaPath)`（resolveEntry + iOS17 时 LookupTunnelInfo→withRsdProvider + OpenInstaller + diagnoseConnectError + SendFile）
  - `internal/cli/device.go` — `deviceInstallCmd`（flags: `--profile`/`--udid`/`--version`；**不注册 `--latest`**——T4 注册）
  - 测试：`internal/device/service_test.go`（Install 分层诊断：connect 失败 iOS17→ErrTunnelRequired/iOS16→原样；operate 错误原样；**connect 失败时 InstallerConn.SendFile 未调 oracle**；LookupTunnelInfo 复用路径）、`internal/cli/device_test.go`（install push + install 变体设备选择 + tunnel + flags E2E）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1 && go test ./... -count=1`；NFR-03 审计：`! grep -Ern 'exec\.Command.*sudo|startTunnel|NewTunnelManager|ServeTunnelInfo' internal/`（无匹配则通过）；NFR-06：`! grep -rn 'danielpaulus/go-ios' internal/cli/`（无匹配则通过）
- **Rollback note**：install 命令 + Service.Install/OpenInstaller/LookupTunnelInfo/withRsdProvider 新增；纯增量。回滚 = revert。

### T4 — device install 自动下载 + `--latest`

- **US / AC / E2E**：US-03、US-08、AC-10-4；AC-03-1/2/3/4/5/6、AC-08-1/2/3、AC-10-4（`--latest`+`--version` 互斥）；E2E-040/041/042/043/044/045/046、E2E-050/051/052
- **目标**：`device install <bid>`（library 缺 IPA）自动下载再推送；`--latest` 强制刷新 App Store 最新版；注册 `--latest` flag + 互斥校验。
- **修改文件**：
  - `internal/cli/device_install.go` — 实现 `downloadToLibrary(deps, out, profile, bundleID, latest)`（install 专属：默认 library 目录 `<configRoot>/library/<profileID>/`；复用 `app_download.go` 的 `handleDownloadError`/`handleLicenseRequired`/`handleTokenExpired`；latest=true 时 `appStore.Lookup` 比较版本精确字符串匹配→library 已有同版本返回现有/否则下载新版保留旧版；用户取消→`ErrCancelled`）；决策树补 **library 缺** 路径（auto-download）+ `--latest` 路径
  - `internal/cli/device.go` — `deviceInstallCmd` 注册 `--latest` flag + `--latest`&`--version` 互斥校验（AC-10-4）
  - 测试：`internal/cli/device_test.go`（E2E-040~046 auto-download happy/no-creds/not-found/license 交互/非TTY/cancel；E2E-050~052 --latest 新版/已最新/无 creds；E2E-063 互斥）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/cli/ -count=1 -run 'InstallAutoDownload|InstallLatest|InstallFlagConflict' && go test ./... -count=1`（**app_download_test 必须仍绿**——证明未改 app_download.go，NFR-07 证据）
- **Rollback note**：仅扩展 device_install.go（downloadToLibrary + 决策树分支）+ device.go 注册 flag。**注意**：auto-download 会在 library 产生持久化副作用（新 IPA 文件 + library index entry）；代码 revert **不自动清除**已下载的 IPA/entry——用户可用 `library clean [bid]` 手动清理。

### T5 — device uninstall + 确认 + ErrAppNotInstalled

- **US / AC / E2E**：US-04、US-06（uninstall 变体）、AC-04-1/2/3/4/5/6、AC-06-1~5（**uninstall 变体**）、AC-09-5（uninstall 分支）、AC-07-3（uninstall tunnel 分支）；E2E-080/081/082/083/084/085、E2E-027（uninstall 多设备交互）、E2E-020/021（**uninstall 变体**）、E2E-074（uninstall 拒 `--profile` 分支）、E2E-092（**uninstall tunnel 分支**）
- **目标**：`device uninstall <bid>` 可用（二次确认 + 非TTY拒绝 + ErrAppNotInstalled）。
- **修改文件**：
  - `internal/device/backend.go` — `ProxyConn` 接口加 `Uninstall(bundleID string) error` 方法
  - `internal/device/backend_impl.go` — `ProxyConn.Uninstall` 实现（`installationproxy.Connection.Uninstall`）
  - `internal/device/service.go` — Service 加 `Uninstall(udid, bundleID)`（resolveEntry + OpenInstallationProxy + diagnoseConnectError + conn.Uninstall → 未装错误映射 ErrAppNotInstalled，operate 阶段不误判 tunnel）
  - `internal/cli/device.go` — `deviceUninstallCmd`（resolveDevice → 非TTY 拒绝 AC-04-4 → UI.Confirm `[y/N]` → DeviceService.Uninstall → errors.Is ErrTunnelRequired/ErrAppNotInstalled 分支文案）
  - `internal/apperr/errors.go` — `ErrAppNotInstalled`
  - 测试：`internal/device/service_test.go`（Uninstall + ErrAppNotInstalled 映射，operate 阶段原样不误判 tunnel）、`internal/cli/device_test.go`（E2E-080~085、E2E-027 uninstall 选择、E2E-020/021 uninstall 变体、E2E-074 uninstall 分支、E2E-092 uninstall tunnel）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1 && go test ./... -count=1`
- **Rollback note**：纯增量（Service.Uninstall + ProxyConn.Uninstall + uninstall 命令 + ErrAppNotInstalled）；回滚 = revert。

## 4. Traceability Matrix — US/AC → Task

> 设备选择（AC-06-1~5）与 0 设备/--udid 未连（AC-02-3/02-5/04-6 等）是**横切**行为，经各命令（apps/install/uninstall）的变体验证：apps 变体→T2，install 变体→T3，uninstall 变体→T5。

| US | AC | Task |
|----|----|------|
| US-01 | AC-01-1/2/3 | T1 |
| | AC-07-1（list 列出 iOS17+ 设备） | T1 |
| US-02 | AC-02-1/2/4/7/8/9 | T3 |
| | AC-02-3/02-5/02-6（0设备/--udid/多设备，install 变体） | T3 |
| US-03 | AC-03-1/2/3/4/5/6 | T4 |
| US-04 | AC-04-1/2/3/4/5 | T5 |
| | AC-04-6（0设备，uninstall 变体） | T5 |
| US-05 | AC-05-1/2 | T2 |
| | AC-05-3（多设备交互，apps 变体） | T2 |
| | AC-05-4（--udid，apps 变体） | T2 |
| US-06 | AC-06-1/2/3/4/5（apps 变体） | T2 |
| | AC-06-1/2/3/4/5（install 变体） | T3 |
| | AC-06-1/2/3/4/5（uninstall 变体） | T5 |
| US-07 | AC-07-1 | T1 |
| | AC-07-2（install tunnel） | T3 |
| | AC-07-3 apps 分支 / uninstall 分支 | T2 / T5（条件性，validate 定论） |
| | AC-07-4（绝不 sudo，NFR-03 审计） | T3 |
| US-08 | AC-08-1/2/3 | T4 |
| US-09 | AC-09-1/2/3/4（install --profile） | T3 |
| | AC-09-5（apps/uninstall/list 拒 --profile） | T1(list)/T2(apps)/T5(uninstall) |
| US-10 | AC-10-1/2/3 | T3 |
| | AC-10-4（--latest+--version 互斥） | T4 |

**反向覆盖**：US-01→T1，US-02→T3，US-03→T4，US-04→T5，US-05→T2，US-06→T2/T3/T5，US-07→T1/T3/T2/T5，US-08→T4，US-09→T3/T1/T2/T5，US-10→T3/T4。全部 10 US 有 task。✅

## 5. Traceability Matrix — E2E → Task

| E2E | Task | E2E | Task |
|-----|------|-----|------|
| E2E-001/002/003/004 | T1 | E2E-040~046 | T4 |
| E2E-090（list iOS17+ 可见） | T1 | E2E-050/051/052 | T4 |
| E2E-010/011/012 | T2 | E2E-060/061/062 | T3 |
| E2E-026/028（apps 多设备） | T2 | E2E-063（互斥） | T4 |
| E2E-020/021 apps 分支 + AC-06 supplementary（apps 路径） | T2 | E2E-070/071/072/073 | T3 |
| E2E-074 apps 分支 / E2E-092 apps 分支 | T2 | E2E-030/030b/031/032/033/034 | T3 |
| E2E-020/021/022/023/024/025（install 命令用例） | T3 | E2E-091/093/093b/093d | T3 |
| E2E-080~085 | T5 | E2E-027（uninstall 多设备） | T5 |
| E2E-020/021 uninstall 变体 / E2E-074 uninstall 分支 | T5 | E2E-092 uninstall 分支 | T5 |
| E2E-074 list 分支 | T1 | E2E-100（go-ios 隔离 grep） | T3（NFR-06 审计） |
| E2E-101（go test 全绿） | 每 task | E2E-093c/102/103/110~113 | validate 手动 |

## 6. Risk Section

| 风险 | 等级 | 缓解 |
|------|------|------|
| **go-ios API 行为不确定（installationproxy iOS17+ 是否需 tunnel）** | 中 | DD-02 分层设计对两种结果稳健；AC-07-3 标条件性；validate 真机定论，若证伪 regress requirements 收窄（不影响交付） |
| **ErrAppNotInstalled 字符串匹配可能不准** | 低 | design 备 pre-check 备选（BrowseUserApps 确认）；T5 执行时 live 验证模式，不准则切 pre-check |
| **tunnel info 复用依赖用户正确启动 tunnel agent** | 低 | 错误提示明确（`sudo ios tunnel start`）；LookupTunnelInfo 失败→graceful 降级到 ErrTunnelRequired |
| **T3 较大（install + tunnel 分层 + tunnel info + flags）** | 中 | T3 是连贯单元（install 命令 + 其必需的 tunnel 处理不可分割；tunnel 是 iOS17+ install 工作前提）。tunnel 不应从 install 拆出。执行时若超预期可内部子步骤但同 commit |
| **任务间共享文件冲突（service.go/backend_impl.go/device.go/device_test.go/service_test.go）** | 中 | **串行执行**（T1→T2→T3→T4→T5），不声明并行；每 task 在前一 task 基础上增量扩展 |
| **任务测试"提前认领"风险** | 低 | 已修正：每 E2E 归属其命令首次出现的 task（apps 变体→T2，install 变体→T3，uninstall 变体→T5）；横切 resolveDevice 经各命令变体分别验证 |
| **改 root.go 命令树（删 stub）** | 低 | 被删为 stub，零用户依赖；T1 含 `go test ./...` 零回归验证 |
| **NFR-07 回归（误改 app_download.go）** | 低 | T4 明确"不改 app_download.go，仅复用其已分解函数"；T4 acceptance 含 app_download_test 全绿验证 |
| **T4 auto-download 持久化副作用** | 低 | rollback 不自动清已下载 IPA/entry；用户可 `library clean` 清理（T4 rollback note 已注） |

## 7. Pre-Execution Baseline（已验证）

- **分支**：`feature/ios-device-manage`（clean）
- **`git status --porcelain`**：空
- **`go build ./...`**：✅ BUILD_OK
- **`go vet ./...`**：✅ VET_OK
- **`go test ./... -count=1`**：✅ 全绿（account/appstore/cli/library 通过；device/ui/config/apperr/doctor 暂无测试文件——本 mission 为 device 新增）

## 8. Decision-Complete 声明

本 plan 从已验收 design 派生，**execution 无需发明**：
- **架构/接口**：device.Service/Backend/InstallerConn/ProxyConn 签名 design 已定；plan §1 Staged Interface 策略明确每 task 扩展哪些方法（无预留 stub，保证每 task 编译绿）
- **任务序**：依赖图明确（串行 T1→T2→T3→T4→T5）
- **错误行为**：DD-02 分层诊断 + ErrCancelled/ErrAppNotInstalled/trust 映射 design 已定
- **验证策略**：每 task acceptance command 可执行；E2E 按命令归属映射；零回归由 `go test ./...` 守护
- **`--latest` 边界**：T3 不注册该 flag（避免半实现），T4 注册 + 互斥校验

执行者可据此直接实现；仅 live 可验行为（installationproxy iOS17+ tunnel、ErrAppNotInstalled 字符串模式）按 design 标注的执行期/live 验证处理。无 placeholder。

## 9. Minor Findings（task ledger，validate 阶段 triage）

> Spock per-task review 产出的 Minor 项，不阻塞 task 推进，记录于此供 validate 阶段统一 triage。

### T1（已 complete）
- [Minor] CLI 测试已补 root-command 路径验证（`TestRoot_RegistersDeviceListCommand`，确认 device list 经 root 注册 + 旧 devices/install stub 已移除）。✅ 已处理。
- [Minor] `service.go` 删除了无功能 `var _ = ios.DeviceEntry{}` 引用与多余 import。✅ 已处理。
- [Minor] `Backend` 注释澄清：类型导出（供 internal/device 内测试 double 实现），但因 `internal/` 包边界不外泄。✅ 已处理。
- [Minor/决议] "NeedsTunnel 显示"措辞澄清：AC-01-1 固定 4 列（UDID/Name/iOS Version/Connection Type），无 tunnel 列；`NeedsTunnel` 是内部字段（T3 tunnel 诊断输入）。tunnel 提示在 install 时（AC-07-2）而非 device list。✅ 已澄清（plan §T1 措辞已更正）。

### T3（已 complete）
- [Minor→已修] Spock Important 1 已修：`--version` 路径仅 `errors.Is(ErrEntryNotFound)` 映射 AC-10-3，其他存储错误→"failed to query library"。
- [Minor→已修] Spock Important 2 已修：mockBackend.OpenInstaller 记录入参 entry，tunnel-reuse 测试断言 `openInstallerEntry.Rsd != nil`（核心不变量：RSD 注入的 entry 确实到达 OpenInstaller）。
- [Minor→已修] Spock Important 3 已修：LookupTunnelInfo 加 httptest 直接单测（happy/404→errTunnelNotFound/500→非 not-found/invalid JSON），用 GO_IOS_AGENT_HOST/PORT env。
- [Minor/决议] LookupTunnelInfo **本地重实现**（只读 HTTP GET，非 import ios/tunnel）——避免拖入 gvisor/quic-go 重依赖，NFR-03 更干净（连 tunnel 包都不 import）。go-ios HttpApiHost/Port 读 env，测试可注入。

### T2（已 complete）
- [Minor] `ErrDeviceNotConnected` 哨兵未在 `--udid` 未连分支 `%w` 包装（当前用 `fmt.Errorf("device '%s' not connected")` 纯消息）。无 `errors.Is` 依赖，AC 不受影响；若未来需 `errors.Is` 可加包装。
- [Minor] "connect 失败→Browse 未调"oracle 目前由代码早返回+nil conn 间接保证；可加显式 `browseCalled` 计数断言提升可证明性。
