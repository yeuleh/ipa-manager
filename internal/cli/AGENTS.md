# internal/cli — cobra 命令树 + 编排

CLI 层：cobra 命令树 + 依赖注入 + 命令编排。**不直接 import go-ios/ipatool**——只见 `device.Service` / `appstore.ProfileAppStore` 等我们的接口（NFR-06）。

## 命令树

```
ipa-manager
  auth (login/revoke)         account (list/use/remove/logout)   app (search/download)
  library (list/clean)        device (list/apps/install/uninstall)   doctor
```

`device` 组为本 mission（ios-device-manage）新增，替代了旧 `devices` 单命令 + `install` 组 stub（已删）。

## Deps 依赖注入（`deps.go`）

所有外部依赖经 `cli.Deps` 注入，测试用 mock：
`Store`（account）/ `AppStoreFactory` / `UI`（Prompter）/ `LibraryStore` / `DeviceService` / `ConfigRoot`。`newProductionDeps()` 构造生产实现；测试构造 mock `Deps` 传给命令构造函数。

## 关键约定

- **每命令 = 薄 RunE + helper 函数**（`runDownload` / `runDeviceInstall` / `resolveDevice` / `resolveProfile` 等），便于测试。
- **`execute(version, args, depsFn, out, errOut) int`**（`root.go`）是可测入口；`Execute()` 仅 `os.Exit(execute(...))`。**DD-09 错误渲染**：root 的 `PersistentPreRunE` 设 `cmd.SilenceUsage=true`——操作错误（RunE）单行 `Error: <msg>` 不带 Usage；格式错误（flag/arg）才显示 Usage。
- **`checkInteractive`**（包级 var，可测覆盖）：非 TTY 检测，复用自 `app_download.go`；`device uninstall` 确认 + 多设备非TTY拒绝均用它。
- **`resolveProfile(deps, profileFlag, requireCredentials)`**（`helpers.go`）：解析 profile；`requireCredentials=true` 校验凭据（search/download 需要），`false` 不校验（library list / cached install push 不需要）。
- **`resolveDevice(deps, udidFlag)`**（`device_helpers.go`）：设备选择 AC-06（0台/--udid未连/单台自动/多台TTY交互/多台非TTY报错）；`SelectDevice` 取消→`apperr.ErrCancelled`→"cancelled" exit 0。
- **`--profile` 仅 `device install` 注册**（涉及 library+凭据）；`device list/apps/uninstall` 不注册→cobra 自动 unknown flag（AC-09-5）。

## 测试

`*_test.go` 含 mock 实现（`mockStore`/`mockAppStore`/`mockLibraryStore`/`mockDeviceService`/`mockPrompter`）+ helper（`helperDownloadStore`/`helperDeviceInstallDeps` 等）。CLI E2E 断言 stdout/exit/副作用；DD-09 测试经 `execute()` 全路径捕获 stderr（操作错误→恰好一次+无 Usage）。

## install 编排（`device_install.go`）

决策树：`--latest`（downloadToLibrary 刷新）/ `--version`（library.GetVersion）/ 默认（library 有→mostRecentByDownloadedAt；无→auto-download）。`downloadToLibrary` 是 install 专属下载（默认 library 目录），**复用** `app_download.go` 的 `handleDownloadError`/`handleLicenseRequired`/`handleTokenExpired`（不重构 app download，保 NFR-07 零回归）。
