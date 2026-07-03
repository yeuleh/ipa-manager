# internal/appstore — ipatool adapter

把 ipatool（`github.com/yeuleh/ipatool/v2`，MIT fork `fix-auth` tag）的 AppStore 适配为 per-profile 隔离的账号能力。**ipatool 类型止步于此包**——CLI 只见 `ProfileAppStore` 接口与我们的值类型。

## 核心接口

- **`ProfileAppStore`**（`adapter.go`）：CLI 依赖的接口（ISP），只暴露用到的方法（Login/Revoke/AccountInfo/Search/Lookup/Download/ReplicateSinf/Purchase/RefreshSession/GetAuthEndpoint）。ipatool 的 AppStore 有 12 方法，我们用其中 ~10。
- **`AppStoreFactory`**（`factory.go`）：`func(account.Profile) (ProfileAppStore, error)`——生产实现闭包 ConfigRoot，测试返回 mock。
- **值类型**（`query.go`/`download_types.go`）：`AppInfo`/`AccountInfoResult`/`DownloadInput`/`DownloadResult`/`Sinf`/`Progress`/`LoginInput`/`LoginResult`——ipatool 类型的"我们的版本"，在包边界替换第三方类型。**`AccountInfoResult` 不暴露 Password/PasswordToken/DirectoryServicesID（NFR-04）**。

## 多账号隔离（ADR 0002）

`ProfileKeychain`（来自 `internal/account`）包裹 ipatool 的 `pkg/keychain.Keychain`，把 ipatool 固定 key `"account"` 映射为 `profiles/<id>/account`，实现多账号。每 profile 独立 cookie jar（`persistent-cookiejar`）。

## 下载编排（CLI 层 `app_download.go` 调用）

完整流程：`AccountInfo`（adapter 缓存 Account）→ `Lookup(bundleID)` → skip-check（已存在跳过）→ `Download` → `handleDownloadError`（license retry + token retry）→ `ReplicateSinf`（写 DRM 密钥，未做则 IPA 无法安装）→ 注册 library index。`device install` 的 `downloadToLibrary` 复用这套 error-recovery 函数。

## 关键约定

- **fork replace**：`go.mod` replace 指向 `yeuleh/ipatool/v2@fix-auth`（修复真实 Apple 登录）。改 fork 耦合需先问。
- **Password 明文**：ipatool 的 `Account` JSON 含 `Password`（存 Keychain），UI/日志必须屏蔽。
- **免费授权**：`Purchase` 仅支持 price=0；付费 app 不支持。
