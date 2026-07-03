# E2E Test — ios-device-manage

> ## ⚠️ LIVE AMENDMENT（execution 阶段 live 实测权威更正）
>
> iOS 26 真机实测：`device install`/`apps`/`uninstall` 经 usbmuxd 可用，**无需 tunnel**。原"iOS 17+ 必须 tunnel"前提证伪。据此 **§3 的 iOS 17+ tunnel 整节（E2E-090/091/092/093/093b/093c/093d）作废**——这些 case 已从自动化测试移除（代码无 tunnel 机器）。其余 E2E case（list/apps/install/uninstall/device-selection/flags）不变。单一事实源：[`requirements.md` Live Amendment](./requirements.md)。

> 从 [`requirements.md`](./requirements.md) AC 单向派生（spec → cases）。验证 [`design.md`](./design.md) 的实现满足每个 AC 的可观测行为。

## 1. Test Scope

| 范围 | 方式 |
|------|------|
| **自动化 E2E**（`internal/cli/device_test.go`） | mock `device.Service` + 既有 mockStore/mockAppStore/mockLibraryStore；覆盖全部 AC 的 CLI 可观测行为（stdout/exit/副作用） |
| **device 包单测**（`internal/device/service_test.go`） | mock `Backend`；覆盖 go-ios 调用契约 + tunnel 错误翻译 |
| **隔离/安全审计** | grep 检查（NFR-03/06） |
| **live 设备验收**（validate 阶段手动） | 真实 iOS 设备 install/apps/uninstall + iOS 17+ tunnel 场景；无 CI 设备，不在自动化范围 |

### Environment Prerequisites

- macOS（go-ios 设备后端 + Keychain）。
- Go ≥ 1.26；`go test ./... -count=1` 全绿为通过判据。
- 自动化测试：零真实设备依赖（全 mock）。
- live 验收（手动）：≥1 台 iOS 设备（含 ≥1 台 iOS 17+ 验证 tunnel 路径）；已 trust Mac。

## 2. Validation Oracles

| Oracle 类型 | 验证手段 |
|------------|----------|
| CLI stdout | `bytes.Buffer` 捕获，`assert.Contains` 关键串 |
| exit code | cobra `Execute()` 返回 nil → exit 0；返回 error → exit 1（root.go os.Exit(1)） |
| library 副作用 | mockLibraryStore 记录 Add/Get 调用参数断言 |
| device 副作用 | mock device.Service 记录 Install/Uninstall 调用参数断言 |
| 设备 app 增删 | live 验收：`device apps` 前后对比（手动） |
| 错误文案 | `assert.Contains` err.Error() 关键串 + suggestion（NFR-04） |

## 3. E2E Cases

> 命名 `E2E-NNN`。type: happy/failure/edge/NFR。自动化标记 `[AUTO]`；手动 `[MANUAL]`。

### device list（US-01）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-001 | AC-01-1 | happy `[AUTO]` | Given mock DeviceService 返回 1 台设备（UDID/Name/版本/USB）；When `device list`；Then 表格输出含 UDID+Name+iOSVersion+"USB"，exit 0 |
| E2E-002 | AC-01-2 | edge `[AUTO]` | Given mock 返回 0 台；When `device list`；Then stdout 含 `"no connected device"`，exit 0 |
| E2E-003 | AC-01-3 | happy `[AUTO]` | Given mock 返回 3 台；When `device list`；Then 3 行设备各含完整 UDID，exit 0 |
| E2E-004 | AC-01-1/AC-07-1 | edge `[AUTO]` | Given mock 返回 1 台（GetLockdownInfo 失败→版本未知，Name/IOSVersion=""，NeedsTunnel=false）；When `device list`；Then 该设备仍被列出（UDID+ConnectionType 可见，Name/版本为占位），exit 0 |

### device apps（US-05）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-010 | AC-05-1 | happy `[AUTO]` | Given 1 台设备 + mock ListInstalledApps 返回 2 app；When `device apps`；Then 表格输出 2 行（Bundle-ID/Name/Version），无系统 app，exit 0 |
| E2E-011 | AC-05-2 | edge `[AUTO]` | Given 1 台 + mock 返回空；When `device apps`；Then `"no user apps installed on device '<name>'"`，exit 0 |
| E2E-012 | AC-05-4 | happy `[AUTO]` | Given 2 台 + mock 按 udid 区分返回；When `device apps --udid <id2>`；Then 列举 id2 设备的 app（mock 收到 udid=id2），exit 0 |

### device selection（US-06，横切 install/apps/uninstall）

> 横切断言：以下 case 用 install 代表，但 **apps/uninstall 必须接入同一 `resolveDevice` helper**——为此另设 E2E-026/027/028 验证 apps 与 uninstall 也走该路径（防止漏接）。mock 函数调用断言（如 `mock Install 收到 udid`）是**外部边界副作用 oracle**（验证传入 Service 的参数），非内部实现耦合。

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-020 | AC-06-1 | failure `[AUTO]` | Given mock 返回 0 台；When `device install <bid>`（及 `device apps`/`device uninstall <bid>` 各一例）；Then `"no connected device; connect a device and trust this Mac"`，exit 1 |
| E2E-021 | AC-06-2 | failure `[AUTO]` | Given 2 台（udid=a,b）；When `device install <bid> --udid ghost`（及 apps/uninstall 各一例）；Then `"device 'ghost' not connected"`，exit 1 |
| E2E-022 | AC-06-3 | happy `[AUTO]` | Given 2 台 + mockUI.SelectDevice 返回 a；When `device install <bid>`（无 --udid，TTY）；Then mock DeviceService.Install 收到 udid=a，exit 0 |
| E2E-023 | AC-06-3 | edge `[AUTO]` | Given 2 台 + mockUI.SelectDevice 返回 ErrCancelled；When `device install <bid>`；Then `"cancelled"`，exit 0（不调 Install） |
| E2E-024 | AC-06-4 | failure `[AUTO]` | Given 2 台 + checkInteractive()=false；When `device install <bid>`；Then `"multiple devices connected; specify --udid (non-interactive mode)"`，exit 1 |
| E2E-025 | AC-06-5 | happy `[AUTO]` | Given 1 台；When `device install <bid>`；Then 自动选中该台（mock Install 收到其 udid，无 prompt），exit 0 |
| E2E-026 | AC-06-3/AC-05-3 | happy `[AUTO]` | Given 2 台 + mockUI.SelectDevice 返回 a；When `device apps`（无 --udid，TTY）；Then mock ListInstalledApps 收到 udid=a（证明 apps 接入 resolveDevice），exit 0 |
| E2E-027 | AC-06-3 | happy `[AUTO]` | Given 2 台 + mockUI.SelectDevice 返回 a + 确认 yes；When `device uninstall <bid>`（无 --udid，TTY）；Then mock Uninstall 收到 udid=a（证明 uninstall 接入 resolveDevice），exit 0 |
| E2E-028 | AC-06-4 | failure `[AUTO]` | Given 2 台 + 非TTY；When `device apps`；Then `"multiple devices connected; specify --udid (non-interactive mode)"`，exit 1（apps 也拒绝） |

### device install — push from library（US-02）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-030 | AC-02-1 | happy `[AUTO]` | Given active profile + LibraryStore.Get 返回 1 entry（FilePath=/x.ipa）+ 1 台设备 + mock Install=nil；When `device install <bid>`；Then mock Install 收到 (udid,"/x.ipa")，stdout 含 "Installed"+app+version+设备名，exit 0 |
| E2E-030b | AC-02-4 | happy `[AUTO]` | Given 2 台（udid=a,b）+ LibraryStore 有 entry + mock Install 按 udid 记录；When `device install <bid> --udid b`；Then mock Install 收到 udid=b（显式 --udid 选中，非自动），exit 0（直接验证 AC-02-4，非 E2E-025 单台等价） |
| E2E-031 | AC-02-2 | happy `[AUTO]` | Given install 成功；When（同流程）mock ListInstalledApps 含该 bid；Then apps 输出含该 app（验证 install 副作用可见） |
| E2E-032 | AC-02-7 | failure `[AUTO]` | Given mock Install 返回 trust 错误（"device not paired"）；When `device install <bid>`；Then 错误含 trust 提示（"trust this Mac"），exit 1 |
| E2E-033 | AC-02-8 | failure `[AUTO]` | Given 无 active profile（mockStore activeID=""）；When `device install <bid>`；Then 错误含 "no active profile"+"accounts use"，exit 1 |
| E2E-034 | AC-02-9 | edge `[AUTO]` | Given mock Install=nil（设备已有 app 不阻断）；When `device install <bid>`；Then 仍调 Install（push 语义不跳过），exit 0 |

> AC-02-3（0 设备）/ AC-02-4（--udid 有效）/ AC-02-5（--udid 未连）/ AC-02-6（多设备交互）由 E2E-020/021/025/022 覆盖（设备选择横切，install 共用）。

### device install — auto-download（US-03）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-040 | AC-03-1 | happy `[AUTO]` | Given LibraryStore.Get 返回空 + mockAppStore Lookup/Download 成功 + 1 台设备 + mock Install=nil；When `device install <bid>`；Then mockAppStore.Download 被调，LibraryStore.Add 被调（副作用丰富 library），mock Install 收到下载后 FilePath，stdout 含下载+安装两步，exit 0 |
| E2E-041 | AC-03-2 | happy `[AUTO]` | Given 自动下载完成；When 检查 LibraryStore.Add 调用；Then entry 含 bid+version+FilePath（library list 可见） |
| E2E-042 | AC-03-3 | failure `[AUTO]` | Given LibraryStore.Get 空 + profile 未登录（credentials=false）；When `device install <bid>`；Then `"profile '<id>' has no credentials"`+"auth login"，exit 1（不触发 Download） |
| E2E-043 | AC-03-4 | failure `[AUTO]` | Given LibraryStore.Get 空 + mockAppStore.Lookup 返回 ErrAppNotFound；When `device install <bid>`；Then `"app not found: <bid>"`，exit 1 |
| E2E-044 | AC-03-5 | happy `[AUTO]` | Given LibraryStore.Get 空 + Download 首次返回 ErrLicenseRequired(price=0) + mockUI.Confirm=true + TTY；When `device install <bid>`；Then Purchase 被调，Download 重试成功，Install 成功，exit 0 |
| E2E-045 | AC-03-5 | edge `[AUTO]` | Given 同上但 mockUI.Confirm=false；When `device install <bid>`；Then `"cancelled"`，exit 0（不 Purchase 不 Install） |
| E2E-046 | AC-03-6 | failure `[AUTO]` | Given LibraryStore.Get 空 + Download 返回 ErrLicenseRequired + checkInteractive()=false；When `device install <bid>`；Then `"license acquisition requires interactive confirmation; cannot proceed non-interactively"`，exit 1 |

### device install — `--latest`（US-08）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-050 | AC-08-1 | happy `[AUTO]` | Given LibraryStore.Get 返回 [v1.0] + mockAppStore Lookup 返回 v2.0 + Download 成功 + 1 台；When `device install <bid> --latest`；Then Download 被调（下载 v2.0），LibraryStore.Add 含 v2.0 且 v1.0 保留（Get 仍返回 2 条），Install 推 v2.0，stdout 含 v2.0，exit 0 |
| E2E-051 | AC-08-2 | happy `[AUTO]` | Given LibraryStore.Get 返回 [v2.0] + mockAppStore Lookup 返回 v2.0（同版本）；When `device install <bid> --latest`；Then Download **不**被调，Install 推现有 v2.0，stdout 含 `"already up to date (v2.0)"`，exit 0 |
| E2E-052 | AC-08-3 | failure `[AUTO]` | Given --latest + profile 未登录；When `device install <bid> --latest`；Then `"no credentials"`，exit 1（Lookup 未调） |

### device install — `--version`（US-10）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-060 | AC-10-1 | happy `[AUTO]` | Given LibraryStore.Get 返回 [v1.0, v2.0]；When `device install <bid> --version 1.0`；Then mock Install 收到 v1.0 的 FilePath，exit 0 |
| E2E-061 | AC-10-2 | happy `[AUTO]` | Given LibraryStore.Get 返回 [v1.0(早), v2.0(晚)]；When `device install <bid>`（无 --version）；Then mock Install 收到 v2.0 FilePath（最近下载），exit 0 |
| E2E-062 | AC-10-3 | failure `[AUTO]` | Given LibraryStore.Get 返回 [v1.0, v2.0]；When `device install <bid> --version 9.9`；Then `"version '9.9' not in library for '<bid>'"`，exit 1 |
| E2E-063 | AC-10-4 | failure `[AUTO]` | Given 任意；When `device install <bid> --latest --version 1.0`；Then 互斥错误，exit 1 |

### device install — `--profile`（US-09）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-070 | AC-09-1 | happy `[AUTO]` | Given profile=bob（非 active）已登录 + LibraryStore.Get(bob) 返回 entry；When `device install <bid> --profile bob`；Then mock Install 推 bob 的 library IPA（LibraryStore 收到 profileID=bob），exit 0 |
| E2E-071 | AC-09-2 | happy `[AUTO]` | Given profile=bob 未登录（credentials=false）+ LibraryStore.Get(bob) 返回 entry；When `device install <bid> --profile bob`；Then 推送成功（cached 无需凭据），exit 0 |
| E2E-072 | AC-09-3 | failure `[AUTO]` | Given profile=bob 未登录 + LibraryStore.Get(bob) 空（需下载）；When `device install <bid> --profile bob`；Then `"no credentials"`，exit 1 |
| E2E-073 | AC-09-4 | failure `[AUTO]` | Given profile=ghost 不存在；When `device install <bid> --profile ghost`；Then `"profile 'ghost' not found"`+"accounts list"，exit 1 |
| E2E-074 | AC-09-5 | failure `[AUTO]` | Given 任意；When `device apps --profile bob`（及 uninstall/list 各一例）；Then cobra `"unknown flag: --profile"`，exit 1 |

### device uninstall（US-04）

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-080 | AC-04-1 | happy `[AUTO]` | Given 1 台 + mock Uninstall=nil + TTY + mockUI.Confirm=true；When `device uninstall <bid>`；Then stdout 含确认提示 `"uninstall '<bid>' from device '<name>'?"`，mock Uninstall 收到 (udid,bid)，成功消息，exit 0 |
| E2E-081 | AC-04-1 | edge `[AUTO]` | Given 同上但 mockUI.Confirm=false；When `device uninstall <bid>`；Then `"cancelled"`，exit 0（不调 Uninstall） |
| E2E-082 | AC-04-2 | happy `[AUTO]` | Given uninstall 成功；When mock ListInstalledApps 不再含 bid；Then apps 输出不含该 app（验证副作用） |
| E2E-083 | AC-04-3 | failure `[AUTO]` | Given mock Uninstall 返回 ErrAppNotInstalled；When `device uninstall <bid>`（确认 yes）；Then `"app '<bid>' not installed on device"`，exit 1 |
| E2E-084 | AC-04-4 | failure `[AUTO]` | Given checkInteractive()=false；When `device uninstall <bid>`；Then `"confirmation required in non-interactive mode; cannot proceed"`，exit 1（无 prompt） |
| E2E-085 | AC-04-5 | happy `[AUTO]` | Given 2 台 + --udid=id2；When `device uninstall <bid> --udid id2`；Then mock Uninstall 收到 (id2,bid)，exit 0 |

> AC-04-6（0 设备）由 E2E-020 覆盖（横切）。

### iOS 17+ tunnel（US-07）

> tunnel 检测分层（design DD-02）：连接阶段失败（OpenInstaller/OpenInstallationProxy）+ iOS≥17 已配对 → ErrTunnelRequired；操作阶段错误原样上浮。device 包单测（mock Backend）验证分层：OpenInstaller 返回 error 时返回的 InstallerConn 的 SendFile **未被调用**（连接失败→操作不可达，oracle 可证）。

| ID | AC | type | Given/When/Then |
|----|----|------|-----------------|
| E2E-090 | AC-07-1 | happy `[AUTO]` | Given mock DeviceService.ListConnected 返回 1 台 NeedsTunnel=true（GetLockdownInfo 返回 version=17.5）；When `device list`；Then 该设备被列出（usbmuxd 可见），exit 0 |
| E2E-091 | AC-07-2 | failure `[AUTO]` | Given LibraryStore 有 IPA + mock DeviceService.Install 返回 ErrTunnelRequired；When `device install <bid>`；Then 输出含 `"iOS 17+ tunnel required; run: sudo ios tunnel start"`，exit 1 |
| E2E-092 | AC-07-3 | failure `[AUTO]` | Given mock ListInstalledApps 返回 ErrTunnelRequired；When `device apps`；Then 同 tunnel 提示，exit 1（同验证 uninstall 一例） |
| E2E-093 | AC-07-4 | NFR `[AUTO]`+审计 | Given 源码；When grep 执行路径；Then 无 `exec.Command("sudo"...)`、无 go-ios tunnel START 调用（`startTunnel`/`NewTunnelManager*`/`ServeTunnelInfo`）；**允许**只读 `TunnelInfoForDevice`（HTTP 查询）。CLI 行为：缺 tunnel 时仅打印提示 exit 1，不请求密码 |
| E2E-093b | DD-02 分层 | unit `[AUTO]` | Given device 包单测：mock Backend.OpenInstaller 返回 error（连接失败）+ InstallerConn mock；When Service.Install；Then 返回 ErrTunnelRequired（iOS≥17）或原样（iOS<17），**且 InstallerConn.SendFile 断言未被调用**（连接失败→操作不可达）。另：OpenInstaller 成功 + SendFile 返回 generic err → 原样上浮（**不**误判 tunnel） |
| E2E-093d | DD-02 闭环 | happy `[AUTO]` | Given mock：iOS≥17 设备 + Backend.LookupTunnelInfo 返回 (addr,port,nil) + OpenInstaller(nil)+SendFile(nil)；When Service.Install；Then LookupTunnelInfo 被调、OpenInstaller 收到注入 RSD 的 entry、SendFile 成功，exit 0（用户启 tunnel 后 install 闭环） |
| E2E-093c | AC-07-3 | NFR `[MANUAL]` | Given iOS 17+ 真机未启 tunnel；When `device apps`/`device uninstall`；Then **观察**：若成功（installationproxy 走 usbmuxd）→ AC-07-3 对 apps/uninstall 不适用（regress requirements 收窄）；若失败 → 触发 tunnel 提示。validate 阶段定论 |

### NFR 审计与回归

| ID | AC/NFR | type | Given/When/Then |
|----|--------|------|-----------------|
| E2E-100 | NFR-06 | NFR `[审计]` | When `grep -r "danielpaulus/go-ios" internal/cli`；Then 无结果（go-ios 类型不泄露 CLI 层） |
| E2E-101 | NFR-07 | NFR `[AUTO]` | When `go test ./... -count=1`；Then exit 0（含前三 mission 全部测试） |
| E2E-102 | NFR-08 | NFR `[MANUAL]` | When 计时 `device list`；Then < 2s（正常 usbmuxd） |
| E2E-103 | NFR-01 | NFR `[MANUAL]` | Given iOS 17+ 设备无 tunnel；When `device install`；Then < 5s exit 1 + tunnel 提示 |

### live 设备验收（validate 阶段手动，[MANUAL]）

| ID | 覆盖 | 场景 |
|----|------|------|
| E2E-110 | US-02/03 闭环 | 真机：`device install <新 bid>`（library 无）→ 自动下载 → 推送 → `device apps` 见到 |
| E2E-111 | US-04 | 真机：`device uninstall <bid>` → `device apps` 不再见到 |
| E2E-112 | US-07 | iOS 17+ 真机：① 未启 tunnel → `device install` 报 tunnel 提示（AC-07-2）；② 启 `sudo ios tunnel start` 后重试 install → LookupTunnelInfo 复用 → 成功（DD-02 闭环）；③ `device apps`/`uninstall` 观察是否需 tunnel（AC-07-3 适用性定论——若成功则 regress requirements 收窄） |
| E2E-113 | US-06 | 2 台真机连接：`device install` 交互选设备；`--udid` 指定 |

## 4. Traceability Matrix（E2E ↔ US ↔ AC）

| US | AC | E2E | 覆盖状态 |
|----|----|-----|---------|
| US-01 device list | AC-01-1 | E2E-001, E2E-004 | ✅ |
| | AC-01-2 | E2E-002 | ✅ |
| | AC-01-3 | E2E-003 | ✅ |
| US-02 install push | AC-02-1 | E2E-030 | ✅ |
| | AC-02-2 | E2E-031 | ✅ |
| | AC-02-3 | E2E-020 | ✅（横切） |
| | AC-02-4 | E2E-030b | ✅（直接 case） |
| | AC-02-5 | E2E-021 | ✅ |
| | AC-02-6 | E2E-022, E2E-023 | ✅ |
| | AC-02-7 | E2E-032 | ✅ |
| | AC-02-8 | E2E-033 | ✅ |
| | AC-02-9 | E2E-034 | ✅ |
| US-03 auto-download | AC-03-1 | E2E-040 | ✅ |
| | AC-03-2 | E2E-041 | ✅ |
| | AC-03-3 | E2E-042 | ✅ |
| | AC-03-4 | E2E-043 | ✅ |
| | AC-03-5 | E2E-044, E2E-045 | ✅ |
| | AC-03-6 | E2E-046 | ✅ |
| US-04 uninstall | AC-04-1 | E2E-080, E2E-081 | ✅ |
| | AC-04-2 | E2E-082 | ✅ |
| | AC-04-3 | E2E-083 | ✅ |
| | AC-04-4 | E2E-084 | ✅ |
| | AC-04-5 | E2E-085 | ✅ |
| | AC-04-6 | E2E-020 | ✅（横切） |
| US-05 device apps | AC-05-1 | E2E-010 | ✅ |
| | AC-05-2 | E2E-011 | ✅ |
| | AC-05-3 | E2E-026 | ✅（apps 直接接入 resolveDevice） |
| | AC-05-4 | E2E-012 | ✅ |
| US-06 device selection | AC-06-1 | E2E-020 | ✅ |
| | AC-06-2 | E2E-021 | ✅ |
| | AC-06-3 | E2E-022, E2E-023, E2E-026, E2E-027 | ✅（install+apps+uninstall 均验证） |
| | AC-06-4 | E2E-024, E2E-028 | ✅（install+apps） |
| | AC-06-5 | E2E-025 | ✅ |
| US-07 tunnel | AC-07-1 | E2E-090 | ✅ |
| | AC-07-2 | E2E-091, E2E-093b | ✅ |
| | AC-07-3 | E2E-092, E2E-093c | ✅（条件性：apps/uninstall 真失败时触发；installationproxy 走 usbmuxd 大概率成功→适用性 validate 定论，若证伪则 regress requirements 收窄） |
| | AC-07-4 | E2E-093 | ✅ |
| US-08 --latest | AC-08-1 | E2E-050 | ✅ |
| | AC-08-2 | E2E-051 | ✅ |
| | AC-08-3 | E2E-052 | ✅ |
| US-09 --profile | AC-09-1 | E2E-070 | ✅ |
| | AC-09-2 | E2E-071 | ✅ |
| | AC-09-3 | E2E-072 | ✅ |
| | AC-09-4 | E2E-073 | ✅ |
| | AC-09-5 | E2E-074 | ✅ |
| US-10 --version | AC-10-1 | E2E-060 | ✅ |
| | AC-10-2 | E2E-061 | ✅ |
| | AC-10-3 | E2E-062 | ✅ |
| | AC-10-4 | E2E-063 | ✅ |

**反向覆盖**：全部 10 个 user story 均有 E2E case；全部 40+ AC 均映射到 ≥1 E2E case。无遗漏。

## 5. Required Unit/Integration Tests

| 层 | 文件 | 覆盖 |
|----|------|------|
| device 包单测 | `internal/device/service_test.go` | mock Backend：ListConnected 映射 DeviceInfo（含 lockdown 失败 best-effort）/ DD-02 分层诊断（connect 失败 iOS≥17→ErrTunnelRequired、operate 错误原样）/ connect 失败时 InstallerConn/ProxyConn 操作方法未被调用 / LookupTunnelInfo 复用路径 / BrowseUserApps→InstalledApp 映射 / ErrAppNotInstalled 识别 |
| CLI E2E | `internal/cli/device_test.go` | 上表全部 `[AUTO]` E2E case |
| 既有 mock 复用 | `mockStore`/`mockAppStore`/`mockLibraryStore`（已存在） | profile/AppStore/library 注入 |
| 新 mock | `mockDeviceService`（实现 device.Service） | 记录 Install/Uninstall/ListConnected/ListInstalledApps 调用参数 + 可配置返回值/错误 |
| 新 mock | `mockUI.SelectDevice` 扩展 | 现有 mockPrompter 加 SelectDevice 字段 |
| app download 回归 | 既有 `app_download_test.go` | app_download.go **不改**（DD-04），其测试原样全绿（NFR-07）；install 复用 handleDownloadError 系列，行为由 device_test 覆盖 |

### Pass/Fail 判据

- 每个 `[AUTO]` E2E：stdout/exit/副作用断言全过 → PASS。
- NFR 审计（E2E-093/100）：grep 无结果 + go test 全绿 → PASS。
- live 验收（E2E-110~113）：手动执行，真实设备行为符合预期 → PASS。
- 任一失败：根因分析（实现 bug 修实现 / spec 缺陷回归 design→requirements）。
