# Validate Evidence Package — ios-device-manage

> 独立再验证（不依赖 execution 阶段结果，全部 case 重新跑）。配套：[`requirements.md`](./requirements.md)（含 Live Amendment）/ [`design.md`](./design.md) / [`e2e_test.md`](./e2e_test.md) / [`plan.md`](./plan.md)。

## 1. 自动化 E2E 全量重跑（fresh，2026-07-03）

| 命令 | 结果 |
|------|------|
| `go build ./...` | ✅ OK |
| `go vet ./...` | ✅ OK |
| `go test ./... -count=1` | ✅ **201 测试全 PASS**（account/appstore/cli/device/library 全绿；含前三 mission 零回归） |
| NFR-03 审计（`grep -Ern 'exec\.Command.*sudo\|tunnel\.NewTunnelManager\|tunnel\.ServeTunnelInfo\|ios/tunnel'`） | ✅ 无匹配 |
| NFR-06 审计（`grep -rn danielpaulus/go-ios internal/cli/`） | ✅ 无匹配（go-ios 止步 internal/device） |

**自动化 `[AUTO]` E2E case**：全部由 `internal/device/service_test.go` + `internal/cli/device_test.go` 覆盖（device/apps/install/uninstall 全路径 + 设备选择 AC-06 各命令变体 + flags + DD-09 渲染 + auto-download/license + pre-check）。tunnel 相关 case（E2E-090~093d/103/112）已按 Live Amendment 移除/作废。

## 2. Live 真机验收（execution 期用户实测，iOS 26 设备）

| 操作 | 命令 | 结果 |
|------|------|------|
| device list | `device list` | ✅ 列出 iPad Pro (iOS 26.5.2, USB+Network) + iPhone |
| device apps | `device apps --udid <iPad>` | ✅ 列出全部 user app（微信/腾讯视频/Notability…）|
| device install（auto-download） | `device install com.starbuckschina.mystarbucksmoments --udid <iPhone>` | ✅ 自动下载 → zipconduit 完整流水线（CreatingStagingDirectory→InstallComplete）→ `✓ Installed`，**无 tunnel** |
| device install（复测，tunnel 移除后） | `device install com.ss.iphone.ugc.aweme.lite` | ✅ 同上（抖音极速版 → iPad），证明删 tunnel 机器无损 |
| device uninstall（未装） | `device uninstall com.nonexistent.fakeapp --udid <iPhone>` | ✅ pre-check → `Error: app '...' not installed on device`，**单行无 Usage**（DD-09） |

**结论**：list/apps/install/uninstall 四项核心操作在 iOS 26 真机经 usbmuxd 全部可用，无需 tunnel。US-07 前提实证证伪，Live Amendment 成立。

## 3. Spec 合规追溯（逐 US/AC/NFR）

| US | AC | 实现位置 | 测试 | Live | 状态 |
|----|----|----------|------|------|------|
| US-01 device list | AC-01-1/2/3 | cli/device.go deviceListCmd | TestDeviceList_* | ✅ | ✅ |
| US-02 install push | AC-02-1~9 | cli/device_install.go + device.Service.Install | TestDeviceInstall_* | ✅ | ✅ |
| US-03 auto-download | AC-03-1~6 | downloadToLibrary（复用 app_download error-recovery） | TestDeviceInstall_AutoDownload_* | ✅ | ✅ |
| US-04 uninstall | AC-04-1~6 | cli/device.go deviceUninstallCmd + Service.Uninstall (pre-check) | TestDeviceUninstall_* / TestUninstall_* | ✅ | ✅ |
| US-05 device apps | AC-05-1~4 | cli/device.go deviceAppsCmd + Service.ListInstalledApps | TestDeviceApps_* / TestListInstalledApps_* | ✅ | ✅ |
| US-06 device selection | AC-06-1~5 | cli/device_helpers.go resolveDevice | TestResolveDevice_* + 各命令变体 | ✅ | ✅ |
| ~~US-07~~ | ~~AC-07-1~4~~ | ~~tunnel 机器~~ | — | — | **REMOVED**（Live Amendment） |
| US-08 --latest | AC-08-1~3 | downloadToLibrary(latest=true) | TestDeviceInstall_Latest_* | — | ✅ |
| US-09 --profile | AC-09-1~5 | deviceInstallCmd --profile；其余命令 cobra unknown flag | TestDeviceInstall_Profile_* / _RejectsProfileFlag | — | ✅ |
| US-10 --version | AC-10-1~4 | resolveIPASource --version + 互斥 | TestDeviceInstall_Version_* / _LatestVersionMutex | — | ✅ |

NFR：
- NFR-01 ~~tunnel precondition~~ **REMOVED**。
- NFR-02 failure boundary：preflight（resolve profile/device/library）在设备写入前；中途失败原样上浮。✅（架构 + 测试）
- NFR-03 no sudo：✅（审计，无 exec/sudo/tunnel-START；tunnel 机器已移除）。
- NFR-04 actionable errors：✅（每条错误含 cause+suggestion：no-credentials/not-installed/trust-hint/no-active-profile/no-connected-device）。
- NFR-05 macOS：✅（build darwin/arm64 cgo）。
- NFR-06 go-ios isolation：✅（审计）。
- NFR-07 no regression：✅（201 测试含前三 mission）。
- NFR-08 device list <2s：✅（live 观察瞬时）。
- NFR-09 non-interactive safety：✅（TestDeviceUninstall_NonInteractive_Refused）。
- NFR-10 consistency：✅（flag/错误文案同源）。
- DD-09 error rendering：✅（TestExecute_OperationalError_NoUsageSinglePrint / _FormatError_ShowsUsage + live）。

**Spec 合规结论**：9 个 active US（US-07 已移除）+ 全部 active AC + 全部 active NFR 满足。

## 4. Traceability 全链覆盖（US → AC → E2E → task）

- **US → AC**：requirements.md 每个 US 列出其 AC（US-07 整体移除）。
- **AC → E2E**：e2e_test.md traceability matrix（US-07 行标 REMOVED，其余 ✅）。
- **E2E → task**：plan.md traceability matrix（E2E → T1~T5）。
- **task → 实现**：T1-T5 全部 COMPLETE（plan.md 标记 + commit 历史）。

**反向覆盖**：9 active US 均有 AC → E2E → task 全链。无缺口。

## 5. Minor Findings Triage（execution ledger + 观察）

| Minor | 出处 | 处置 | 理由 |
|-------|------|------|------|
| `ErrDeviceNotConnected` 未在 `--udid` 未连分支 `%w` 包装 | T2 | **defer** | 无 `errors.Is` 依赖；消息精确；未来需要再包 |
| connect-fail→Browse 未调 oracle 间接 | T2 | **accept** | 代码早返回+nil conn 保证；pre-check 重构后 Uninstall 路径已显式（bundle 缺→不调 Uninstall） |
| go-ios `slog` INFO 进度日志噪声 | live 观察 | **defer** | 进度信息有用（install 大文件）；若需静默可配 go-ios log level（未来 polish） |
| library 路径双斜杠 `…/profile//file` | live 观察 | **defer** | cosmetic（Unix 路径双斜杠等价，文件系统正常）；属 download mission 路径构造，跨 mission |
| trust heuristic 字符串匹配 | design | **defer** | 需真机 trust 错误样本确认；当前 heuristic（pair/trust/not paired）覆盖常见形式 |
| `isNotInstalledErr` 字符串匹配 | T5 | **已消解** | 改 pre-check 后该函数已移除 |

**triage 结论**：无 Critical/Important 残留；全部 Minor 为 defer（有明确未来处置路径）或 accept/已消解。不阻塞 dock。

## 6. Resolved-blocked tasks 验证

execution 期间**无 blocked 任务**（T1-T5 全部独立完成，无 BLOCKED 判定）。N/A。

## 7. 综合结论

- 自动化 E2E：201 测试全绿，NFR-03/06/07 审计通过。
- Live 真机：4 项核心操作（list/apps/install/uninstall）iOS 26 验证通过，无需 tunnel。
- Spec 合规：9 active US + 全 active AC/NFR 满足；US-07 按 Live Amendment 移除。
- Traceability：US→AC→E2E→task 全链无缺口。
- Minor：全 defer/accept/已消解，无阻塞。

**mission 达成交付标准，可 dock。**
