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

## 2. Dependency Graph

```
T1 (device list + 命令树重构 + device 包骨架)
   │  建立 device.Service/Backend/DeviceInfo/defaultBackend 骨架 + device 命令组
   ▼
T2 (device apps + resolveDevice + SelectDevice)
   │  │  引入设备选择（被 install/uninstall 复用）
   │  ▼
   │  ├─────────────────┐
   ▼                    ▼
T3 (device install        T5 (device uninstall
    推送 + tunnel)          + 确认 + ErrAppNotInstalled)
   │  │  可与 T5 并行（不同文件）
   ▼
T4 (device install
    自动下载 + --latest)
```

- **T1** 无依赖，是基础（device 包骨架 + 命令组）。阻塞 T2/T3/T4/T5。
- **T2** 依赖 T1；引入 `resolveDevice`+`SelectDevice`，阻塞 T3/T5（install/uninstall 复用设备选择）。
- **T3** 依赖 T1（骨架）+ T2（resolveDevice）；install 推送 + tunnel 检测（DD-02 分层 + tunnel info 复用）。
- **T4** 依赖 T3（install 命令已存在）；扩展 auto-download + `--latest`。
- **T5** 依赖 T1（骨架）+ T2（resolveDevice）；与 T3/T4 轨道**可并行**（主要改 uninstall 命令 + Service.Uninstall）。
- **可并行轨道**：T2 完成后，T3→T4 轨道与 T5 可并行。

**无独立 Foundation 任务**：T1 既是 story task（US-01 device list）又顺带建立 device 包骨架（被其余任务复用）。Foundation 任务数 0 < story 任务数 5，符合约束。

## 3. Task List（垂直切片）

### T1 — device list + 命令树重构 + device 包骨架

- **User Story / AC / E2E**：US-01；AC-01-1/2/3（+ AC-07-1 的 NeedsTunnel 显示雏形）；E2E-001/002/003/004
- **目标**：`device list` 可用；建立 `device.Service`/`Backend`/`DeviceInfo`/`defaultBackend` 架构；移除旧 `devices`+`install` stub，统一 `device` 命令组。
- **新建文件**：
  - `internal/device/service.go` — `Service` 接口、`DeviceInfo`（UDID/Name/IOSVersion/ConnectionType/NeedsTunnel）、`NewService(backend Backend)`、`backendService.ListConnected()`（调 Backend.ListDeviceEntries + best-effort GetLockdownInfo → DeviceInfo）
  - `internal/device/backend.go` — `Backend` 接口（本任务仅实现 `ListDeviceEntries`/`GetDeviceEntry`/`GetLockdownInfo`，其余方法签名预留待 T2/T3/T5 补全）
  - `internal/device/backend_impl.go` — `defaultBackend{}`：`ListDeviceEntries()`（`ios.ListDevices`）、`GetDeviceEntry(udid)`（`ios.GetDevice`）、`GetLockdownInfo(entry)`（`ios.GetValues` → name+version）
  - `internal/device/tunnel.go` — `isIOS17OrLater(version)`（TrimSpace → split "." → Atoi 首段 → ≥17）
  - `internal/cli/device.go` — `deviceCmd(deps)` 命令组 + `deviceListCmd`（表格输出 UDID/Name/iOS Version/Connection Type；0 台→"no connected device" exit 0）
  - `internal/device/service_test.go` — ListConnected 映射测试（mock Backend：lockdown 成功/失败 best-effort、NeedsTunnel 计算）
  - `internal/cli/device_test.go` — E2E-001/002/003/004（mock DeviceService；新增 `mockDeviceService`）
- **修改文件**：
  - `internal/cli/root.go` — 注册 `deviceCmd(deps)` 替代 `devicesCmd()`+`installCmd()`
  - `internal/cli/deps.go` — `Deps` 加 `DeviceService device.Service`；`newProductionDeps` 构造 `device.NewService(device.NewDefaultBackend())`
- **删除文件**：
  - `internal/cli/devices.go`（顶层 `devices` stub）、`internal/cli/install.go`（`install` 组 stub）
  - `internal/device/client.go`（`ListConnectedDevices` stub）、`internal/device/apps.go` 的 stub 函数（保留 `InstalledApp` 类型，迁移到 service.go 或保留 apps.go 仅含类型）
- **测试**：`internal/device/service_test.go`（ListConnected）、`internal/cli/device_test.go`（E2E-001~004）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1 -run "TestDeviceList|TestListConnected"`（全绿）；`go test ./... -count=1`（零回归）
- **Completion criteria**：Spock review pass + 上述测试全绿 + `device list` 命令存在且输出符合 AC-01-1/2/3 + NFR-07（前三 mission 测试仍绿）
- **Rollback note**：删除旧 CLI stub（devices.go/install.go）是破坏性变更但被删为 stub（零用户依赖）；回滚 = git revert T1 commit。`root.go` 命令树变更若需回滚同上。

### T2 — device apps + resolveDevice + SelectDevice（设备选择）

- **User Story / AC / E2E**：US-05、US-06；AC-05-1/2/3/4、AC-06-1/2/3/4/5、AC-09-5；E2E-010/011/012/020/021/022/023/024/025/026/027/028/074
- **目标**：`device apps` 可用；引入 `resolveDevice`（被 install/uninstall 复用）+ `ui.SelectDevice`+`ErrCancelled`。
- **新建文件**：
  - `internal/cli/device_helpers.go` — `resolveDevice(deps, udidFlag) (device.DeviceInfo, error)`（AC-06-1~5：0台/no-udid-未连/单台自动/多台 TTY 交互/多台非TTY 报错；ErrCancelled from SelectDevice）
- **修改文件**：
  - `internal/device/backend.go` — 补 `OpenInstallationProxy(entry) (ProxyConn, error)`、`ProxyConn` 接口（`BrowseUserApps`/`Uninstall`/`Close`，本任务实现 BrowseUserApps 调用；Uninstall 方法签名预留 T5）
  - `internal/device/backend_impl.go` — `OpenInstallationProxy`（`installationproxy.New`）、`ProxyConn` 实现（`BrowseUserApps` → `installationproxy.AppInfo` 映射 `InstalledApp`）
  - `internal/device/service.go` — `ListInstalledApps(udid)`（resolveEntry + OpenInstallationProxy + best-effort diagnoseConnectError + BrowseUserApps → InstalledApp）
  - `internal/cli/device.go` — `deviceAppsCmd`（resolveDevice → ListInstalledApps → 表格/空消息）
  - `internal/ui/prompter.go` — `DeviceOption` 结构、`SelectDevice(options []DeviceOption) (string, error)` 方法
  - `internal/ui/prompt.go` — `SelectDevice` huh 实现（取消→`apperr.ErrCancelled`）
  - `internal/apperr/errors.go` — `ErrCancelled`、`ErrDeviceNotConnected`、`ErrMultipleDevices`
  - `internal/cli/device_test.go` — mockPrompter 加 `SelectDevice` 字段；E2E-010/011/012/020~028/074
  - `internal/device/service_test.go` — ListInstalledApps、resolveEntry best-effort
- **测试**：device/service_test（ListInstalledApps 映射）、cli/device_test（apps + 全部设备选择 E2E）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1`（全绿）；`go test ./... -count=1`（零回归）
- **Completion criteria**：Spock pass + 测试全绿 + `device apps` 可用 + resolveDevice 覆盖 AC-06-1~5 + AC-09-5（apps/uninstall/list 拒绝 `--profile`）
- **Rollback note**：本任务纯增量（新增 resolveDevice/SelectDevice/apps），无破坏性；回滚 = revert。

### T3 — device install（library 推送 + tunnel 分层检测 + tunnel info 复用）

- **User Story / AC / E2E**：US-02、US-07、US-09、US-10；AC-02-1/2/3/4/5/6/7/8/9、AC-07-1/2/4、AC-09-1/2/3/4、AC-10-1/2/3/4；E2E-030/030b/032/033/034、E2E-090/091/093/093b/093d、E2E-060/061/062/063、E2E-070/071/072/073
- **目标**：`device install <bid>`（library 有 IPA 时推送）可用；实现 DD-02 分层 tunnel 检测（connect 失败→ErrTunnelRequired，operate→原样）+ tunnel info 只读复用（iOS 17+ 用户启 tunnel 后 install 闭环）；`--udid`/`--version`/`--profile` flags。
- **新建文件**：
  - `internal/cli/device_install.go` — install 命令编排（决策树 §3.3 的 **library 有** 路径 + `--version` + `--profile`；`--latest` 与 auto-download 留 T4 但 flag 互斥校验 AC-10-4 在此实现）。`downloadToLibrary` 函数签名预留（T4 实现）。
- **修改文件**：
  - `internal/device/backend.go` — 补 `OpenInstaller(entry) (InstallerConn, error)`、`InstallerConn` 接口（`SendFile`/`Close`）、`LookupTunnelInfo(udid) (address string, rsdPort int, err error)`
  - `internal/device/backend_impl.go` — `OpenInstaller`（`zipconduit.New`）、`InstallerConn` 实现（`SendFile` → `zipconduit.SendFile`）、`LookupTunnelInfo`（`tunnel.TunnelInfoForDevice`，只读 HTTP）、`withRsdProvider(entry, udid, address, rsdPort)`（参考 go-ios cli_device_resolution.go，复制 entry 字段 + 设 Rsd provider，不复制 UserspaceTUN）
  - `internal/device/tunnel.go` — `diagnoseConnectError(err, version)`（iOS≥17→ErrTunnelRequired；否则原样）
  - `internal/device/service.go` — `Install(udid, ipaPath)`（resolveEntry + iOS17 时 LookupTunnelInfo→withRsdProvider + OpenInstaller + diagnoseConnectError + SendFile）、`resolveEntry`（GetDeviceEntry 硬错 + GetLockdownInfo best-effort）
  - `internal/cli/device.go` — `deviceInstallCmd`（flags: `--profile`/`--udid`/`--version`/`--latest`；`--latest`+`--version`互斥 AC-10-4）
  - `internal/cli/device_test.go` — E2E-030/030b/032/033/034/060~063/070~073/090/091/093
  - `internal/device/service_test.go` — Install 分层诊断（connect 失败 iOS17→ErrTunnelRequired/iOS16→原样；operate 错误原样；connect 失败时 InstallerConn.SendFile 未调 oracle）、LookupTunnelInfo 复用路径、resolveEntry
- **测试**：device/service_test（Install 分层 + tunnel info）、cli/device_test（install push + tunnel + flags）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1`；NFR-03 审计：`grep -rn "exec.Command.*sudo\|startTunnel\|NewTunnelManager\|ServeTunnelInfo" internal/`（应无结果，仅 `TunnelInfoForDevice` 出现）；NFR-06：`grep -rn "danielpaulus/go-ios" internal/cli`（应无结果）；`go test ./... -count=1`（零回归）
- **Completion criteria**：Spock pass + 测试全绿 + `device install <bid>`（library 有）推送成功 + DD-02 分层诊断单测覆盖（connect/operate 分离 oracle）+ NFR-03/06 审计通过
- **Rollback note**：install 命令新增 + device 包 Install/OpenInstaller/LookupTunnelInfo 新增；纯增量。tunnel.go 的 diagnoseConnectError 是新逻辑；回滚 = revert。

### T4 — device install 自动下载 + `--latest`

- **User Story / AC / E2E**：US-03、US-08；AC-03-1/2/3/4/5/6、AC-08-1/2/3；E2E-040/041/042/043/044/045/046/050/051/052
- **目标**：`device install <bid>`（library 缺 IPA）自动下载再推送；`--latest` 强制刷新 App Store 最新版。
- **修改文件**：
  - `internal/cli/device_install.go` — 实现 `downloadToLibrary(deps, out, profile, bundleID, latest)`（install 专属：默认 library 目录；复用 `app_download.go` 的 `handleDownloadError`/`handleLicenseRequired`/`handleTokenExpired`；latest=true 时 Lookup 比较 version 精确字符串匹配→已有则返回现有/否则下载新版保留旧版；用户取消→`ErrCancelled`）；决策树补 **library 缺** 路径（auto-download）+ `--latest` 路径
  - `internal/cli/device_test.go` — E2E-040~046（auto-download happy/no-creds/not-found/license 交互/非TTY/cancel）、E2E-050~052（--latest 新版/已最新/无 creds）
- **测试**：cli/device_test（auto-download + --latest 全路径）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/cli/ -count=1 -run "TestInstallAutoDownload|TestInstallLatest"`；`go test ./... -count=1`（零回归，含 app_download_test 仍绿——证明未改 app_download.go）
- **Completion criteria**：Spock pass + 测试全绿 + `device install`（library 缺）自动下载 + `--latest` 刷新 + app_download_test 零回归（NFR-07 证据）
- **Rollback note**：仅扩展 device_install.go（downloadToLibrary + 决策树分支）；回滚 = revert。

### T5 — device uninstall + 确认 + ErrAppNotInstalled

- **User Story / AC / E2E**：US-04；AC-04-1/2/3/4/5/6（+ AC-07-3 uninstall 部分）；E2E-080/081/082/083/084/085、E2E-092（uninstall tunnel 一例）
- **目标**：`device uninstall <bid>` 可用（二次确认 + 非TTY拒绝 + ErrAppNotInstalled）。
- **修改文件**：
  - `internal/device/service.go` — `Uninstall(udid, bundleID)`（resolveEntry + OpenInstallationProxy + diagnoseConnectError + conn.Uninstall → 未装错误映射 ErrAppNotInstalled）
  - `internal/device/backend_impl.go` — `ProxyConn.Uninstall(bundleID)` 实现（`installationproxy.Uninstall`）
  - `internal/cli/device.go` — `deviceUninstallCmd`（resolveDevice → 非TTY 拒绝 AC-04-4 → UI.Confirm `[y/N]` → DeviceService.Uninstall → errors.Is ErrTunnelRequired/ErrAppNotInstalled 分支文案）
  - `internal/apperr/errors.go` — `ErrAppNotInstalled`
  - `internal/cli/device_test.go` — E2E-080~085（确认 yes/no、成功后 apps 不见、未装、非TTY、--udid、0 设备横切）、E2E-092（uninstall tunnel）
  - `internal/device/service_test.go` — Uninstall + ErrAppNotInstalled 映射（operate 阶段，不误判 tunnel）
- **测试**：device/service_test（Uninstall + ErrAppNotInstalled）、cli/device_test（uninstall 全路径）
- **Acceptance command**：`go build ./... && go vet ./... && go test ./internal/device/ ./internal/cli/ -count=1`；`go test ./... -count=1`（零回归）
- **Completion criteria**：Spock pass + 测试全绿 + `device uninstall <bid>` 可用 + AC-04-1~6 覆盖
- **Rollback note**：纯增量（Service.Uninstall + uninstall 命令）；回滚 = revert。

## 4. Traceability Matrix — US/AC → Task

| US | AC | Task |
|----|----|------|
| US-01 device list | AC-01-1/2/3 | T1 |
| | AC-07-1（list 列出 iOS17+ 设备 + NeedsTunnel 显示） | T1（NeedsTunnel）+ T3（tunnel 诊断深化） |
| US-02 install push | AC-02-1/2/4 | T3 |
| | AC-02-3（0 设备） | T2（resolveDevice 横切） |
| | AC-02-5（--udid 未连） | T2 |
| | AC-02-6（多设备交互） | T2 |
| | AC-02-7（trust） | T3 |
| | AC-02-8（无 active profile） | T3 |
| | AC-02-9（设备已有 app 仍 push） | T3 |
| US-03 auto-download | AC-03-1/2/3/4/5/6 | T4 |
| US-04 uninstall | AC-04-1/2/3/4/5 | T5 |
| | AC-04-6（0 设备） | T2（横切） |
| US-05 device apps | AC-05-1/2 | T2 |
| | AC-05-3（多设备交互） | T2 |
| | AC-05-4（--udid） | T2 |
| US-06 device selection | AC-06-1/2/3/4/5 | T2 |
| US-07 tunnel | AC-07-2（install tunnel） | T3 |
| | AC-07-3（apps/uninstall tunnel） | T2（apps）+ T5（uninstall）；条件性，validate 定论 |
| | AC-07-4（绝不 sudo） | T3（NFR-03 审计） |
| US-08 --latest | AC-08-1/2/3 | T4 |
| US-09 --profile | AC-09-1/2/3/4 | T3（install --profile） |
| | AC-09-5（apps/uninstall/list 拒绝 --profile） | T2（cobra unknown flag） |
| US-10 --version | AC-10-1/2/3 | T3 |
| | AC-10-4（--latest+--version 互斥） | T3（互斥校验）+ T4（--latest 实现） |

**反向覆盖**：US-01→T1，US-02→T3，US-03→T4，US-04→T5，US-05→T2，US-06→T2，US-07→T3（+T2/T5），US-08→T4，US-09→T3，US-10→T3。全部 10 US 有任务。✅

## 5. Traceability Matrix — E2E → Task

| E2E | Task | E2E | Task |
|-----|------|-----|------|
| E2E-001/002/003/004 | T1 | E2E-040~046 | T4 |
| E2E-010/011/012 | T2 | E2E-050/051/052 | T4 |
| E2E-020~028（设备选择） | T2 | E2E-060/061/062/063 | T3 |
| E2E-030/030b/032/033/034 | T3 | E2E-070/071/072/073 | T3 |
| E2E-031（install 成功后 apps 见） | T3（install）跨 T2（apps） | E2E-074（apps/uninstall 拒 --profile） | T2 |
| E2E-080~085 | T5 | E2E-090/091/093/093b/093d | T3 |
| E2E-082（uninstall 后 apps 不见） | T5 跨 T2 | E2E-092（apps/uninstall tunnel） | T2（apps）+T5（uninstall） |
| E2E-093c/102/103 | validate 手动 | E2E-100（go-ios 隔离 grep） | T3（NFR-06 审计） |
| E2E-101（go test 全绿） | 每个 task | E2E-110~113（live 真机） | validate 手动 |

## 6. Risk Section

| 风险 | 等级 | 缓解 |
|------|------|------|
| **go-ios API 行为不确定（installationproxy iOS17+ 是否需 tunnel）** | 中 | DD-02 分层设计对两种结果稳健；AC-07-3 标条件性；validate 真机定论，若证伪 regress requirements 收窄（不影响交付） |
| **ErrAppNotInstalled 字符串匹配可能不准** | 低 | design 已备 pre-check 备选（BrowseUserApps 确认）；T5 执行时 live 验证模式，不准则切 pre-check |
| **tunnel info 复用依赖用户正确启动 tunnel agent** | 低 | 错误提示明确（`sudo ios tunnel start`）；LookupTunnelInfo 失败→graceful 降级到 ErrTunnelRequired（不崩溃） |
| **T3 较大（install + tunnel 分层 + tunnel info + flags）** | 中 | T3 是连贯单元（install 命令 + 其必需的 tunnel 处理不可分割）；tunnel 检测是 install 在 iOS17+ 工作的前提，不可单独成 task。执行时若超预期可内部子步骤但同 commit |
| **改 root.go 命令树（删 stub）** | 低 | 被删为 stub（"not yet implemented"），零用户依赖；T1 含 `go test ./...` 零回归验证 |
| **go-ios 版本升级破坏 backend_impl** | 低 | Backend 接口隔离使变更限于 backend_impl.go（与 appstore 对 ipatool 隔离策略一致） |
| **NFR-07 回归（误改 app_download.go）** | 低 | T4 明确"不改 app_download.go，仅复用其已分解函数"；T4 acceptance 含 app_download_test 全绿验证 |

## 7. Pre-Execution Baseline（已验证）

- **分支**：`feature/ios-device-manage`（clean）
- **`git status --porcelain`**：空（无未提交变更）
- **`go build ./...`**：✅ BUILD_OK
- **`go vet ./...`**：✅ VET_OK
- **`go test ./... -count=1`**：✅ 全绿（account/appstore/cli/library 测试通过；device/ui/config/apperr/doctor 暂无测试文件——本 mission 将为 device 新增）

## 8. Decision-Complete 声明

本 plan 从已验收的 design.md（含 8 DD + 文件结构 + 5 处理流程 + 接口签名）派生，**无需在 execution 阶段发明**：
- 架构：device.Service/Backend/InstallerConn/ProxyConn（design §2/§3 已定签名）
- 接口：所有公开/内部方法签名已定义（design DD-01/02/03/04 + §3.1）
- 任务序：依赖图明确（T1→T2→{T3→T4, T5}）
- 错误行为：DD-02 分层诊断 + ErrCancelled/ErrAppNotInstalled/trust 映射（design §3.1）
- 验证策略：每个 task 的 acceptance command 可执行 + E2E case 已映射

执行者可据此直接实现，遇仅 live 可验的行为（installationproxy iOS17+ tunnel、ErrAppNotInstalled 字符串模式）按 design 标注的执行期/live 验证处理。
