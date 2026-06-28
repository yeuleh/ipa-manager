# Research — ipa-manager（Stage 3 层-2 工具链与集成细节）

> 查询时间：2026-06-28
> 研究方法：zread 读源码、context7 查文档、webfetch GitHub API、grep-app

## 0. 关键结论摘要

| 主题             | 结论                                                                                                                                                                                              |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ipatool 多账号   | **ipatool 当前 AppStore 实现把账号凭据固定写入 keychain key `"account"`**。要支持多账号，不能直接复用默认 key；应为每个 profile 提供独立 `keychain.Keychain` 包装器 + 独立 cookie jar。                     |
| ipatool 凭据内容 | keychain 中保存的是 JSON 化 `appstore.Account`：email、passwordToken、dsid、storeFront、password、pod、name。登录 cookie 通过 `persistent-cookiejar` 单独保存。                                          |
| ⚠️ keyring 包纠正 | ipatool 实际使用 `github.com/byteness/keyring`（非前提中的 `99designs/keyring`）。实现时以锁定版本的 go.mod 为准。tech-stack.md 中的描述需以此研究结论为准。                                              |
| go-ios 设备/安装 | 列设备 `ios.ListDevices()` / `ios.GetDevice(udid)`；安装 IPA `zipconduit.New(device).SendFile(path)`；列应用/卸载 `installationproxy.New(device)`。                                                       |
| iOS 17+ tunnel   | README 明确：iOS 17+ 需先 `sudo ios tunnel start`。ZipConduit 对支持 RSD 的设备走 `com.apple.streaming_zip_conduit.shim.remote`，否则走 usbmuxd。                                                          |
| 交互库           | v1 推荐 **`charmbracelet/huh`**。`survey` 已 archive；`promptui` 未归档但更新弱；`huh` 维护活跃、覆盖 Select/Confirm/Input。                                                                              |
| cobra/lipgloss   | Cobra latest v1.10.2；Lip Gloss latest v2.0.4（v2 导入路径为 `charm.land/lipgloss/v2`，含 `table` 子包）。                                                                                                |
| 项目结构         | 推荐 `cmd/ipa-manager/main.go` + `internal/{cli,account,appstore,device,library,config,ui}`。不暴露 `/pkg`，除非未来提供 SDK。                                                                          |
| 测试/lint/构建   | 标准 `testing` + 少量 `testify/require`；golangci-lint v2；构建先 `go build`，发布稳定后引入 GoReleaser。                                                                                                |

---

## 1. ipatool 多账号 keychain 集成模式

### 1.1 keychain API 与存储 key

`ipatool` 的 keychain 抽象很小：

```go
type Keychain interface {
    Get(key string) ([]byte, error)
    Set(key string, data []byte) error
    Remove(key string) error
}
```

`AccountInfo()` 固定读 `t.keychain.Get("account")`；`Login()` 固定写 `t.keychain.Set("account", data)`；`Revoke()` 固定删 `"account"`。

**结论**：ipatool 自身无 profile/account namespace；默认只支持一个活跃账号。多账号必须由我们的 wrapper 提供。

来源：`majd/ipatool/pkg/keychain/keychain.go`、`pkg/keychain/keychain_set.go`、`pkg/appstore/appstore_login.go`、`appstore_account_info.go`、`appstore_revoke.go`（GitHub raw / zread / grep-app，2026-06-28）

### 1.2 keychain 里保存什么

```go
type Account struct {
    Email               string `json:"email,omitempty"`
    PasswordToken       string `json:"passwordToken,omitempty"`
    DirectoryServicesID string `json:"directoryServicesIdentifier,omitempty"`
    Name                string `json:"name,omitempty"`
    StoreFront          string `json:"storeFront,omitempty"`
    Password            string `json:"password,omitempty"`
    Pod                 string `json:"pod,omitempty"`
}
```

⚠️ `password` 明文在结构内（虽存 Keychain），UI 与日志必须屏蔽。

来源：`majd/ipatool/pkg/appstore/account.go`、`appstore_login.go`（GitHub raw，2026-06-28）

### 1.3 cookie jar 也是状态

ipatool 用 `persistent-cookiejar`，默认 `~/.ipatool/cookies`，AppStore 把同一个 CookieJar 传给所有 HTTP clients。

**结论**：多账号不仅要隔离 keychain entry，还要隔离 cookie jar 文件，避免登录 cookie 跨账号污染。

来源：`majd/ipatool/cmd/common.go`（GitHub raw，2026-06-28）

### 1.4 ⚠️ keyring 包纠正

ipatool 当前 `go.mod` 与源码显示使用 `github.com/byteness/keyring v1.9.0`，**并非** `99designs/keyring`。实现以锁定版本的 go.mod 为准。

来源：`majd/ipatool/go.mod`、`pkg/keychain/keyring.go`（GitHub raw，2026-06-28）

### 1.5 推荐实现：ProfileKeychain wrapper（key namespace）

最小侵入方案 —— 包装 Keychain，把 ipatool 内部固定 key `"account"` 映射为我们的 profile key：

```go
package appstorex

import (
    "fmt"
    ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
)

type ProfileKeychain struct {
    Base      ipakeychain.Keychain
    ProfileID string
}

func (k ProfileKeychain) mapKey(key string) string {
    return fmt.Sprintf("profiles/%s/%s", k.ProfileID, key)
}
func (k ProfileKeychain) Get(key string) ([]byte, error)    { return k.Base.Get(k.mapKey(key)) }
func (k ProfileKeychain) Set(key string, data []byte) error { return k.Base.Set(k.mapKey(key), data) }
func (k ProfileKeychain) Remove(key string) error           { return k.Base.Remove(k.mapKey(key)) }
```

每个账号实例化独立 AppStore：

```go
func NewProfileAppStore(profileID, cookiePath string) appstore.AppStore {
    osys := operatingsystem.New()
    mach := machine.New(machine.Args{OS: osys})
    baseKeychain := newBaseKeychain(mach)           // 参考 ipatool/cmd/common.go newKeychain()
    return appstore.NewAppStore(appstore.Args{
        Keychain:        appstorex.ProfileKeychain{Base: baseKeychain, ProfileID: profileID},
        CookieJar:       must(cookiejar.New(&cookiejar.Options{Filename: cookiePath})),
        OperatingSystem: osys,
        Machine:         mach,
    })
}
```

### 1.6 备选方案对比

| 方案                          | 优点                       | 风险                                   |
| ----------------------------- | -------------------------- | -------------------------------------- |
| 包装 key namespace（推荐 v1） | 一个 ServiceName，统一管理 | 需自写 wrapper                         |
| 每 profile 一个 ServiceName   | 不改 key，隔离更彻底       | Keychain 项多；迁移/清理复杂           |
| 直接复用默认 `"account"`      | 实现最少                   | **不支持多账号**，会互相覆盖（不可接受）|

**v1 决策**：包装 key namespace + 每 profile 独立 cookie jar。

---

## 2. go-ios 设备获取与安装 API

### 2.1 列出设备

```go
devices, err := ios.ListDevices()
for _, d := range devices.DeviceList {
    fmt.Println(d.Properties.SerialNumber, d.ConnectionTypeLabel())
}
device, err := ios.GetDevice(udid) // 按 UDID
```

来源：`danielpaulus/go-ios/ios/listdevices.go`、`ios/utils.go`（zread/GitHub raw，2026-06-28）

### 2.2 安装 IPA

```go
conn, err := zipconduit.New(device)
defer conn.Close()
err = conn.SendFile("/path/to/app.ipa")
```

`zipconduit.New` 内部：非 RSD 设备走 `com.apple.streaming_zip_conduit`；RSD/tunnel 设备走 `com.apple.streaming_zip_conduit.shim.remote`。

来源：`ios/zipconduit/zipconduit_installer.go`、`main.go installApp`（zread/grep-app，2026-06-28）

### 2.3 列举/卸载应用

```go
svc, _ := installationproxy.New(device)
defer svc.Close()

apps, _ := svc.BrowseUserApps()   // 或 BrowseAllApps / BrowseSystemApps / BrowseFileSharingApps
for _, app := range apps {
    fmt.Println(app.CFBundleIdentifier(), app.CFBundleName(), app.CFBundleShortVersionString())
}

svc.Uninstall("com.example.app")
```

来源：`ios/installationproxy/installationproxy.go`、`main.go uninstallApp`（zread/grep-app，2026-06-28）

### 2.4 iOS 17+ tunnel 前提

README 明确：iOS 17+ 需 `sudo ios tunnel start`。缺 tunnel 时 `ConnectToShimService` 返回 `missing tunnel address and RSD port. To start the tunnel, run 'ios tunnel start'`。

| 操作          | iOS < 17          | iOS 17+                       |
| ------------- | ----------------- | ----------------------------- |
| ListDevices   | usbmuxd 可用      | 可列 USB，服务操作通常需 tunnel |
| 安装 IPA      | usbmuxd ZipConduit | 需 tunnel；走 shim remote      |
| 列应用/卸载   | installation_proxy over usbmuxd | 文档建议 tunnel；v1 按"需 tunnel"处理 |

**v1 实现注意**：不静默失败、不自动 `sudo` 提权；检测到 iOS 17+ / SupportsRsd 缺失时，提示用户运行 `sudo ios tunnel start`。

来源：`danielpaulus/go-ios/README.md`、`ios/connect.go`、`ios/rsd.go`、`zread_search_doc: App Installation and Uninstallation`（2026-06-28）

---

## 3. 交互提示库状态与 v1 推荐

### 3.1 维护状态（GitHub API）

| 库                    | Stars | pushed_at   | archived | 结论             |
| --------------------- | ----: | ----------- | -------: | ---------------- |
| `manifoldco/promptui` |  6399 | 2024-08-06  |    false | 可用但维护较弱   |
| `AlecAivazis/survey`  |  4112 | 2024-04-07  |     true | 已归档，不采用   |
| `charmbracelet/huh`   |  6989 | 2026-06-15  |    false | **推荐**         |

来源：GitHub API `repos/{owner}/{repo}`（webfetch，2026-06-28）

### 3.2 huh 用法（适合精简 CLI）

```go
var profile string
huh.NewSelect[string]().
    Title("选择 Apple 账号").
    Options(huh.NewOption("alice@example.com", "alice"), huh.NewOption("bob@example.com", "bob")).
    Value(&profile).Run()

var ok bool
huh.NewConfirm().Title("安装到该设备？").Affirmative("安装").Negative("取消").Value(&ok).Run()
```

**v1 决策**：使用 `charmbracelet/huh`。来源：Context7 `/charmbracelet/huh`（2026-06-28）

---

## 4. cobra + lipgloss 当前用法

### 4.1 Cobra（latest v1.10.2）

```go
rootCmd := &cobra.Command{Use: "ipa-manager", Short: "Manage IPA downloads and device installs"}
rootCmd.AddCommand(authCmd(), appsCmd(), devicesCmd(), installCmd())
if err := rootCmd.Execute(); err != nil { os.Exit(1) }
```

每命令一个文件：`internal/cli/{root,auth,account,apps,devices,install}.go`。来源：Context7 `/spf13/cobra`、GitHub API v1.10.2（2026-06-28）

### 4.2 Lip Gloss（latest v2.0.4，导入路径 `charm.land/lipgloss/v2`）

```go
import (
    "charm.land/lipgloss/v2"
    "charm.land/lipgloss/v2/table"
)

t := table.New().
    Border(lipgloss.NormalBorder()).
    Headers("Profile", "Email", "Active").
    Rows(rows...).
    StyleFunc(func(row, col int) lipgloss.Style {
        if row == table.HeaderRow { return lipgloss.NewStyle().Bold(true) }
        return lipgloss.NewStyle().Padding(0, 1)
    })
lipgloss.Println(t)
```

来源：Context7 `/charmbracelet/lipgloss`、GitHub API v2.0.4（2026-06-28）

---

## 5. Go 项目目录结构

```
ipa-manager/
  cmd/ipa-manager/main.go        # 极薄，只调 internal/cli.Execute()
  internal/
    cli/      root.go auth.go account.go apps.go devices.go install.go
    account/  profile.go store.go keychain.go   # ProfileKeychain wrapper
    appstore/ client.go download.go search.go    # ipatool adapter
    device/   list.go install.go apps.go uninstall.go  # go-ios orchestration
    library/  ipa_store.go        # 按账号隔离本地 .ipa
    config/   paths.go config.go
    ui/       prompt.go table.go  # huh + lipgloss
  docs/
  go.mod
```

原则：业务代码全进 `internal/`；不建 `/pkg` 除非未来对外 SDK；`appstore`/`device` 包只做 adapter，不泄漏第三方类型到 CLI 层。

来源：Standard Go Project Layout（GitHub raw，2026-06-28）

---

## 6. 测试 / lint / 构建

### 6.1 测试
标准库 `testing` + `testify/require`（v1.11.1）。第三方调用通过小接口（`AccountStore`/`AppStoreClient`/`DeviceInstaller`）封装便于 fake。

```go
func TestProfileKeychainMapsAccountKey(t *testing.T) {
    fake := newFakeKeychain()
    kc := ProfileKeychain{Base: fake, ProfileID: "alice"}
    require.NoError(t, kc.Set("account", []byte("data")))
    require.Equal(t, []byte("data"), fake.data["profiles/alice/account"])
}
```

### 6.2 lint（golangci-lint v2.12.2）
```yaml
version: "2"
linters:
  default: standard
  enable: [govet, staticcheck, errcheck, ineffassign, unused, misspell, gosec, revive]
```
不要一开始 `default: all`；Apple ID/password/token 日志需靠 code review 把关（gosec 不足以发现所有泄露）。

### 6.3 构建
起步：`go build ./cmd/ipa-manager`、`go test ./...`。GoReleaser（v2.16.0）在需要多架构产物 / GitHub Release / Homebrew tap / 签名公证时再引入。

来源：GitHub API releases（testify v1.11.1、golangci-lint v2.12.2、goreleaser v2.16.0，2026-06-28）

---

## 7. 已发现的坑与注意事项

1. **多账号 keychain**：ipatool 内部 key 固定 `"account"`，直接复用会覆盖；keychain 含 `Password` 明文字段须屏蔽日志；cookie jar 须按 profile 隔离；keyring 包实为 `byteness/keyring`。
2. **iOS 17+ tunnel**：go-ios 明确要求 `sudo ios tunnel start`；iOS 18.2+ QUIC 被移除，协议仍在演进；tunnel 错误须设计成可诊断文本；v1 不自动提权。
3. **工具查询失败记录**：部分 zread/context7/webfetch 首次 timeout，重试或换 GitHub raw 后成功；不影响结论。

---

## 8. v1 明确建议汇总

1. 多账号：`ProfileKeychain` wrapper（`"account"` → `"profiles/<id>/account"`）+ 每 profile 独立 cookie jar。
2. 交互库：`charmbracelet/huh`。
3. CLI：cobra 命令树 + huh 必要交互 + lipgloss table。
4. 设备操作：直接 import go-ios（`ios.ListDevices` / `zipconduit.SendFile` / `installationproxy.BrowseUserApps` / `.Uninstall`）。
5. iOS 17+：tunnel 作为前置条件，错误明确提示 `sudo ios tunnel start`，不自动提权。
