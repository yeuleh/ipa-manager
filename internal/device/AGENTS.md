# internal/device — go-ios adapter

把 go-ios（`github.com/danielpaulus/go-ios`，MIT）的设备能力适配为 CLI 可用的 `device.Service`。**go-ios 类型止步于此包**（NFR-06）——CLI 只见 `Service` 接口 + `DeviceInfo`/`InstalledApp` 值类型。

## Service / Backend 双层

- **`Service`**（`service.go`，CLI 可见）：`ListConnected` / `ListInstalledApps` / `Install` / `Uninstall`。
- **`Backend`**（`backend.go`，包内）：封装 go-ios 调用（含 `ios.DeviceEntry` 等类型）；`defaultBackend` 调真实 go-ios，测试注入 mock。
- **连接接口**：`InstallerConn`（zipconduit，SendFile）/ `ProxyConn`（installationproxy，BrowseUserApps/Uninstall/Close）。
- 双层 mock：CLI 测试 mock `Service`（行为级），device 包测试 mock `Backend`（go-ios 调用级）。

## ⚠️ 无需 tunnel（Live Amendment，重要）

**iOS 17+ install/apps/uninstall 经 usbmuxd 全部可用，无需 tunnel**（iOS 26 真机实证）。原"iOS 17+ 必须 tunnel"前提（源自过时 research.md）已证伪，整套 tunnel 机器（`LookupTunnelInfo`/`WithRsd`/`diagnoseConnectError`/`ErrTunnelRequired`/`isIOS17OrLater`）已移除。

`Service.Install/ListInstalledApps/Uninstall` 现在是简单的 **connect（原样错误）+ operate**：`GetDeviceEntry` → `Open*`（失败原样上浮，不臆测 tunnel）→ operate（SendFile/Browse/Uninstall）。若未来某环境真出现 usbmuxd 装不上，按实证驱动加定点处理，**不重新引入推测性 tunnel 机器**。详见 `docs/features/ios-device-manage/` Live Amendment。

## Uninstall pre-check（AC-04-3）

go-ios 的 `installationproxy.Uninstall` 对未装 bundle **幂等成功**（不报错）。故 `Service.Uninstall` 在调 `Uninstall` 前**先 `BrowseUserApps` 确认存在**；不存在 → `apperr.ErrAppNotInstalled`（防止"typo → 误导性 ✓ Uninstalled"）。

## 关键约定

- **绝不自动 sudo / 不启动 tunnel**（NFR-03）。
- go-ios 类型（`ios.DeviceEntry`/`installationproxy.AppInfo`/`zipconduit`）仅出现在 `backend.go`/`backend_impl.go`。
- `DeviceInfo`（UDID/Name/IOSVersion/ConnectionType）无 go-ios 类型；lockdown 失败（untrusted）时 Name/IOSVersion 空、设备仍列出。
