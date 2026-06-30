# Design — download-ipa-by-account

> 本设计基于 `requirements.md`（42 AC / 10 NFR / 10 US）和 ipatool fork `v2.3.1-fix-auth.5` 源码实证。所有 ipatool API 签名均从 module cache 实际源码验证，非猜测。

---

## 1. Goals & Non-Goals（重述）

### 满足的 User Stories

| US | 描述 | 设计覆盖 |
|----|------|----------|
| US-01 (P1) | 按名字搜索 App Store | DD-06 `apps search`，DD-01 Search 方法 |
| US-02 (P1) | 下载 IPA 到本地 | DD-04 download 编排，DD-01 Download 方法 |
| US-03 (P1) | per-account 隔离存储 | DD-03 library 路径布局，DD-02 LibraryStore |
| US-04 (P1) | 列出已下载 IPA | DD-06 `library list`，DD-02 LibraryStore.List |
| US-05 (P1) | 清理 library | DD-06 `library clean`，DD-02 LibraryStore.Remove/Clean |
| US-06 (P2) | 幂等跳过 + --force | DD-04 步骤 6（existence check） |
| US-07 (P2) | 免费授权交互提示 | DD-04 步骤 8（license retry），DD-10 非交互检测 |
| US-08 (P2) | --profile flag | DD-07 profile 解析器 |
| US-09 (P3) | 指定版本下载 | DD-04 ExternalVersionID 参数 |
| US-10 (P3) | --output 自定义路径 | DD-04 OutputPath 参数，DD-02 索引跟踪 |

### Non-goals（约束设计的边界）

- **不修改 ipatool fork 源码**——所有 Apple API 调用通过 adapter 隔离。
- **不实现并发互斥**——同 profile 并发 download 行为未定义（继承 R3）。
- **不做 `apps versions`**——列举历史版本是未来 mission。
- **不做设备侧操作**——`install push/uninstall/update` 是未来 mission。
- **不引入新的外部库**——除 ipatool 已传递依赖的 `schollz/progressbar/v3`（已在 go.sum）。
- **不做 library 自动清理策略**（LRU / 容量上限）——用户手动 clean。
- **不做付费 app 购买**——Purchase API 仅支持 price=0。

---

## 2. Architecture & Key Decisions

### 组件总览

```
┌─────────────────────────────────────────────────────────────────────┐
│                        CLI Layer (internal/cli)                      │
│  download.go   │  apps.go(search) │  library.go(list/clean)         │
│       │                │                     │                        │
│       └────────────────┼─────────────────────┘                        │
│                        ▼                                              │
│              resolveProfile() (DD-07)                                │
│              --profile / active → account.Profile                    │
└───────┬────────────────┬─────────────────────┬──────────────────────┘
        │                │                     │
        ▼                ▼                     ▼
┌───────────────┐ ┌──────────────┐ ┌──────────────────┐
│ account.Store │ │ appstore.     │ │ library.Store    │
│ (existing)    │ │ AppStoreFactory│ │ (DD-02, NEW)     │
│ profile CRUD  │ │ → ProfileApp- │ │ per-profile IPA  │
│ + credentials │ │   Store       │ │ index + files    │
└───────────────┘ └──────┬───────┘ └────────┬─────────┘
                         │                  │
                  ┌──────┴───────┐          │
                  │ ProfileApp-  │          │
                  │ Store        │          │
                  │ (DD-01,      │          │
                  │  EXTENDED)   │          │
                  └──────┬───────┘          │
                         │                  │
                         ▼                  │
                  ┌──────────────┐          │
                  │ profileApp-  │          │
                  │ StoreAdapter │          │
                  │ (wraps       │          │
                  │  ipatool)    │          │
                  └──────┬───────┘          │
                         │                  │
                         ▼                  │
                  ┌──────────────┐          │
                  │ ipatool      │          │
                  │ AppStore     │          │
                  │ (Apple API)  │          │
                  └──────────────┘          │
                                             │
                                     <configRoot>/library/<profileID>/
                                     ├── index.json (DD-02)
                                     └── *.ipa files
```

### 核心设计决策

#### DD-01：扩展 ProfileAppStore 接口（+6 方法）

**决策**：在现有 `ProfileAppStore` 接口（3 方法）上新增 6 个方法，覆盖 search/download 所需的 ipatool API。不拆分为多个小接口。

**新增方法**（全部在 `internal/appstore/adapter.go`）：
```go
type ProfileAppStore interface {
    // --- existing (auth, unchanged) ---
    GetAuthEndpoint() (string, error)
    Login(input LoginInput) (LoginResult, error)
    Revoke() error

    // --- NEW: query ---
    AccountInfo() (AccountInfoResult, error)
    Lookup(bundleID string) (AppInfo, error)
    Search(query string, limit int64) ([]AppInfo, error)

    // --- NEW: download ---
    Download(input DownloadInput) (DownloadResult, error)
    Purchase(bundleID string, appID int64, price float64) error
    ReplicateSinf(sinfs []Sinf, packagePath string) error
}
```

**我们的类型**（不泄露 ipatool 类型，ISP + DIP）：
```go
// AppInfo is our version of ipatool's App (only fields we use).
type AppInfo struct {
    ID       int64   // ipatool App.ID (trackId)
    BundleID string  // ipatool App.BundleID
    Name     string  // ipatool App.Name (trackName)
    Version  string  // ipatool App.Version
    Price    float64 // ipatool App.Price
}

// AccountInfoResult wraps the Account fields needed by callers.
// Does NOT expose Password/PasswordToken (security: NFR-04).
type AccountInfoResult struct {
    Email      string
    Name       string
    StoreFront string
    // internal: adapter keeps full ipatool Account for Download/Purchase calls
    // but does NOT expose it through this struct
}

// DownloadInput is our version of ipatool's DownloadInput.
type DownloadInput struct {
    BundleID          string
    AppID             int64
    OutputPath        string   // empty = default library path
    ExternalVersionID string   // empty = latest
    Progress          Progress // nil-safe interface (DD-05)
}

// DownloadResult summarizes a completed download.
type DownloadResult struct {
    DestinationPath string
    Version         string
    Sinfs           []Sinf // for ReplicateSinf
}

// Sinf is our version of ipatool's Sinf (DRM key fragment).
type Sinf struct {
    ID   int64
    Data []byte
}

// Progress is our abstraction over ipatool's *progressbar.ProgressBar.
// nil = no progress display; concrete impl wraps progressbar (DD-05).
type Progress interface {
    // Methods we actually call on progressbar (narrowed to our usage).
    ChangeMax64(int64)
    Set64(int64) error
}
```

**理由**：
- 单一 adapter 实现所有方法——拆分为 AuthAppStore + DownloadAppStore 会迫使 `Deps` 持有两个 factory 或 factory 返回 union 接口，增加复杂度而无实际收益。
- ipatool 类型（`ipaappstore.App` / `ipaappstore.Account` / `ipaappstore.Sinf`）仅在 adapter 内部使用，调用方只见我们的类型（NFR-08）。
- `AccountInfoResult` **故意不含 Password/PasswordToken**——CLI 层永远不应接触凭据字段（NFR-04）。adapter 内部保留完整 Account 用于 Download/Purchase 的 Apple API 调用。

**备选方案**：
- 拆分为 `AuthAppStore` + `DownloadAppStore` 两个接口 —— **被否**：两者都由同一个 adapter struct 实现，拆分增加类型复杂度但无隔离收益。
- 直接暴露 ipatool 的 `AppStore` 接口 —— **被否**：破坏 R1 mitigation（ipatool 升级时 ipatool 类型泄露到 CLI 层）。

**Adapter 实现**（`client_impl.go` 扩展）：
```go
func (a *profileAppStoreAdapter) AccountInfo() (AccountInfoResult, error) {
    out, err := a.inner.AccountInfo()
    if err != nil {
        return AccountInfoResult{}, err
    }
    // Cache full account for subsequent Download/Purchase calls
    a.account = &out.Account
    return AccountInfoResult{
        Email:      out.Account.Email,
        Name:       out.Account.Name,
        StoreFront: out.Account.StoreFront,
    }, nil
}

func (a *profileAppStoreAdapter) Lookup(bundleID string) (AppInfo, error) {
    if a.account == nil {
        return AppInfo{}, fmt.Errorf("AccountInfo must be called before Lookup")
    }
    out, err := a.inner.Lookup(ipaappstore.LookupInput{
        Account:  *a.account,
        BundleID: bundleID,
    })
    if err != nil {
        return AppInfo{}, err
    }
    return appToAppInfo(out.App), nil
}

func (a *profileAppStoreAdapter) Search(query string, limit int64) ([]AppInfo, error) {
    if a.account == nil {
        return nil, fmt.Errorf("AccountInfo must be called before Search")
    }
    out, err := a.inner.Search(ipaappstore.SearchInput{
        Account: *a.account,
        Term:    query,
        Limit:   limit,
    })
    if err != nil {
        return nil, err
    }
    results := make([]AppInfo, len(out.Results))
    for i, app := range out.Results {
        results[i] = appToAppInfo(app)
    }
    return results, nil
}

func (a *profileAppStoreAdapter) Download(input DownloadInput) (DownloadResult, error) {
    if a.account == nil {
        return DownloadResult{}, fmt.Errorf("AccountInfo must be called before Download")
    }
    var pb *progressbar.ProgressBar
    if input.Progress != nil {
        // unwrap our Progress to *progressbar.ProgressBar (DD-05)
        pb = input.Progress.(*progressBarWrapper).inner
    }
    out, err := a.inner.Download(ipaappstore.DownloadInput{
        Account:           *a.account,
        App:               appInfoToApp(input.BundleID, input.AppID),
        OutputPath:        input.OutputPath,
        Progress:          pb,
        ExternalVersionID: input.ExternalVersionID,
    })
    if err != nil {
        return DownloadResult{}, mapDownloadError(err) // DD-08
    }
    return DownloadResult{
        DestinationPath: out.DestinationPath,
        Version:         extractVersionFromPath(out.DestinationPath),
        Sinfs:           sinfsToOur(out.Sinfs),
    }, nil
}

func (a *profileAppStoreAdapter) Purchase(bundleID string, appID int64, price float64) error {
    if a.account == nil {
        return fmt.Errorf("AccountInfo must be called before Purchase")
    }
    return a.inner.Purchase(ipaappstore.PurchaseInput{
        Account: *a.account,
        App:     ipaappstore.App{ID: appID, BundleID: bundleID, Price: price},
    })
}

func (a *profileAppStoreAdapter) ReplicateSinf(sinfs []Sinf, packagePath string) error {
    ipaSinfs := make([]ipaappstore.Sinf, len(sinfs))
    for i, s := range sinfs {
        ipaSinfs[i] = ipaappstore.Sinf{ID: s.ID, Data: s.Data}
    }
    return a.inner.ReplicateSinf(ipaappstore.ReplicateSinfInput{
        Sinfs:       ipaSinfs,
        PackagePath: packagePath,
    })
}
```

**Adapter 状态说明**：adapter struct 新增 `account *ipaappstore.Account` 字段。`AccountInfo()` 调用后缓存完整 Account（含 Password/Token/DSID），供后续 Lookup/Search/Download/Purchase 使用。这是必要的——ipatool 的这些 API 都需要完整 Account 作为参数。缓存局限在 adapter 内部，不泄露到接口层。

**关键签名（源码验证）**：
```go
// ipatool pkg/appstore/appstore.go
type AppStore interface {
    AccountInfo() (AccountInfoOutput, error)     // ← 读 keychain "account"
    Lookup(input LookupInput) (LookupOutput, error)
    Search(input SearchInput) (SearchOutput, error)
    Download(input DownloadInput) (DownloadOutput, error)
    Purchase(input PurchaseInput) error
    ReplicateSinf(input ReplicateSinfInput) error
    // ... Login/Revoke/Bag (existing)
}

// ipatool pkg/appstore/appstore_download.go
type DownloadInput struct {
    Account           Account
    App               App
    OutputPath        string
    Progress          *progressbar.ProgressBar
    ExternalVersionID string
}
type DownloadOutput struct {
    DestinationPath string
    Sinfs           []Sinf
}

// ipatool pkg/appstore/appstore_search.go
type SearchInput struct {
    Account Account
    Term    string
    Limit   int64
}
type SearchOutput struct {
    Count   int
    Results []App
}

// ipatool pkg/appstore/appstore_lookup.go
type LookupInput struct {
    Account  Account
    BundleID string
}
type LookupOutput struct {
    App App
}

// ipatool pkg/appstore/appstore_replicate_sinf.go
type ReplicateSinfInput struct {
    Sinfs       []Sinf
    PackagePath string
}
```

#### DD-02：LibraryStore 接口与 JSON 索引

**决策**：新增 `library.Store` 接口 + JSON 实现，管理 per-profile IPA 元数据索引。

**接口**（`internal/library/store.go`）：
```go
// Store manages the per-profile IPA library: file paths + metadata index.
// Used as Deps.LibraryStore in CLI layer.
type Store interface {
    // Add registers a downloaded IPA in the profile's index + verifies file exists.
    Add(profileID string, entry Entry) error

    // List returns all IPA entries for the profile.
    List(profileID string) ([]Entry, error)

    // Get returns the entry for a specific bundle-id (or ErrEntryNotFound).
    Get(profileID, bundleID string) (Entry, error)

    // Remove deletes the IPA file (if exists) + removes the index entry.
    // Returns ErrEntryNotFound if bundle-id not in index.
    Remove(profileID, bundleID string) error

    // CleanAll removes all IPA files + clears the index for the profile.
    // Returns the count of removed entries.
    CleanAll(profileID string) (int, error)
}

// Entry is a single IPA record in the library index.
type Entry struct {
    BundleID     string    `json:"bundle_id"`
    AppID        int64     `json:"app_id"`
    Version      string    `json:"version"`
    FilePath     string    `json:"file_path"`      // absolute path to .ipa
    FileSize     int64     `json:"file_size"`      // bytes
    DownloadedAt time.Time `json:"downloaded_at"`  // UTC
}
```

**JSON 索引格式**（`<configRoot>/library/<profileID>/index.json`）：
```json
{
  "entries": [
    {
      "bundle_id": "com.tencent.xin",
      "app_id": 123456789,
      "version": "8.0.34",
      "file_path": "/Users/x/.ipa-manager/library/alice_example_com/com.tencent.xin_123456789_8.0.34.ipa",
      "file_size": 234567890,
      "downloaded_at": "2026-07-01T12:00:00Z"
    }
  ]
}
```

**空状态**（profile 无任何 IPA）：文件不存在或 `{"entries": []}`。

**原子写**：与 config.json 一致（tmp + rename，DD-10 of multi-account mission）。

**理由**：
- JSON 文件最简——无外部依赖（SQLite 过度），人类可读（便于调试），单文件原子写可靠。
- `file_path` 存绝对路径——支持 `--output` 自定义路径（AC-10-2 索引跟踪）。
- per-profile 独立索引文件——天然隔离（NFR-03），无跨 profile 查询需求。

**备选方案**：
- SQLite —— **被否**：引入 CGO 依赖（除非用 modernc.org/sqlite 纯 Go），对 v1 单用户场景过重。
- 从文件系统推导（扫描 .ipa 文件 + 解析文件名）—— **被否**：无 DownloadedAt 时间戳；文件名解析脆弱（bundle-id 可含特殊字符）。
- 嵌入 config.json —— **被否**：library 与 config 关注点分离（Stage 4 设计）。

#### DD-03：Library 路径布局

**决策**：
```
<configRoot>/                              ← ~/.ipa-manager/
├── config.json                            ← profile 元数据（existing, 不动）
├── profiles/
│   └── <profileID>/
│       └── cookies                        ← cookie jar（existing, 不动）
├── keychain/                              ←（existing, v1 不使用）
└── library/                               ← NEW
    └── <profileID>/
        ├── index.json                     ← per-profile 元数据索引（DD-02）
        └── <bundleID>_<appID>_<version>.ipa  ← IPA 文件
```

**IPA 文件名规则**（与 ipatool 的 `fileName()` 一致，`appstore_download.go:201`）：
```
<bundleID>_<appID>_<version>.ipa
例：com.tencent.xin_123456789_8.0.34.ipa
```

**默认下载路径解析**（`library.Store` 提供 helper）：
```go
func (s *store) defaultIPAPath(profileID string, entry Entry) string {
    return filepath.Join(s.libraryRoot, profileID,
        fmt.Sprintf("%s_%d_%s.ipa", entry.BundleID, entry.AppID, entry.Version))
}
```

**`--output` 路径**：覆盖默认路径，IPA 存到用户指定位置，但 `index.json` 的 `file_path` 记录该绝对路径（AC-10-2）。

**理由**：
- `<configRoot>/library/<profileID>/` 天然 per-profile 隔离（NFR-03）。
- 文件名含 version——同一 app 不同版本不冲突（`--external-version-id` 场景）。
- 与 ipatool 的 `fileName()` 一致——降低认知负担。

#### DD-04：Download 流程编排

**决策**：在 `internal/cli/download.go` 的 RunE 中编排完整下载流程。不使用 ipatool 的 retry-go，用显式错误分支（与 auth login 的两阶段模式一致）。

**Happy Path**：
```
User: ipa-manager download <bundle-id>
  │
  ├─ [resolveProfile(deps, --profile)] → profile (DD-07)
  │    └─ ✗ not found / not logged in → error + exit 1
  │
  ├─ [factory(profile)] → appStore (ProfileAppStore)
  │
  ├─ [appStore.AccountInfo()] → accountResult
  │    └─ ✗ keychain read fail → "profile not logged in" error + exit 1
  │
  ├─ [appStore.Lookup(bundleID)] → app (AppInfo)
  │    └─ ✗ "app not found" → error + exit 1 (AC-02-4)
  │
  ├─ [resolveOutputPath(profile, app, --output)]
  │    ├─ --output given → validate path (DD-09) → use it
  │    └─ no --output → library.defaultIPAPath(profile, app)
  │
  ├─ [libraryStore.Get(profile.ID, bundleID)] → existing?
  │    ├─ exists AND same version AND not --force → "already exists" + exit 0 (AC-02-5)
  │    └─ not exists OR --force → continue
  │
  ├─ [appStore.Download(DownloadInput{...})] → downloadResult
  │    ├─ ✗ ErrLicenseRequired → license retry (下方)
  │    ├─ ✗ ErrPasswordTokenExpired → token retry (下方)
  │    └─ ✗ other → error + exit 1
  │
  ├─ [appStore.ReplicateSinf(downloadResult.Sinfs, downloadResult.DestinationPath)]
  │    └─ ✗ → "failed to apply DRM keys" error + exit 1 (NFR-02)
  │
  ├─ [libraryStore.Add(profile.ID, Entry{...})]  ← 记录到索引
  │
  └─ print "✓ Downloaded: <name> <version> → <path>"
     exit 0 (AC-02-1)
```

**License Retry 子流程**（AC-02-7 / AC-02-8 / AC-02-11）：
```
Download returns ErrLicenseRequired:
  │
  ├─ app.Price > 0?
  │    └─ YES → "paid apps are not supported" error + exit 1 (AC-02-8)
  │
  ├─ isInteractive(stdin)?
  │    └─ NO → "license acquisition requires interactive confirmation" error + exit 1 (AC-02-11)
  │
  ├─ [ui.Confirm("this app requires a free license. acquire?")]
  │    ├─ NO → "cancelled" + exit 0 (AC-02-7, 非错误)
  │    └─ YES ↓
  │
  ├─ [appStore.Purchase(bundleID, appID, price=0)]
  │    └─ ✗ → error + exit 1
  │
  └─ [appStore.Download(DownloadInput{...})] → retry download
       └─ ✗ → error + exit 1
```

**Token Expired Retry 子流程**（AC-02-10）：
```
Download returns ErrPasswordTokenExpired:
  │
  ├─ [appStore.Login(LoginInput{Email: account.Email, Password: account.Password})]
  │    ├─ ✗ → "re-login failed" error + exit 1 (AC-02-10)
  │    └─ ✓ → acc updated
  │
  └─ [appStore.Download(DownloadInput{...})] → retry download
       ├─ ✗ → error + exit 1
       └─ ✓ → continue to ReplicateSinf
```

**关键点**：
- License retry 和 Token retry 各最多 1 次（不循环重试）。与 ipatool 的 retry-go（2 attempts）语义一致但更显式。
- Token retry 需要 Password——来自 `AccountInfo()` 返回的 Account（keychain 中存储，A-03）。adapter 内部缓存完整 Account。
- `appStore.AccountInfo()` 必须在 Lookup/Download 前调用（adapter 的前置条件）。

**备选方案**：
- ipatool 的 retry-go 模式 —— **被否**：retry-go 是通用重试库，对我们的"恰好两次"语义过度；显式 if 更可读（与 auth login 设计一致）。

#### DD-05：Progress 进度条抽象

**决策**：用窄接口 `Progress` 包装 `schollz/progressbar/v3`，仅在交互式终端构造。

**实现**（`internal/appstore/progress.go`）：
```go
type progressBarWrapper struct {
    inner *progressbar.ProgressBar
}

func (w *progressBarWrapper) ChangeMax64(max int64) { w.inner.ChangeMax64(max) }
func (w *progressBarWrapper) Set64(v int64) error   { return w.inner.Set64(v) }

// NewProgress creates a Progress for interactive terminal download.
// Returns nil for non-interactive (CI/pipe) — ipatool handles nil gracefully.
func NewProgress() Progress {
    if !isTerminal(os.Stdout) {
        return nil
    }
    pb := progressbar.NewOptions64(1,
        progressbar.OptionSetDescription("downloading"),
        progressbar.OptionSetWriter(os.Stdout),
        progressbar.OptionShowBytes(true),
        progressbar.OptionSetWidth(20),
        progressbar.OptionFullWidth(),
        progressbar.OptionThrottle(65*time.Millisecond),
        progressbar.OptionShowCount(),
        progressbar.OptionClearOnFinish(),
        progressbar.OptionSpinnerType(14),
        progressbar.OptionSetRenderBlankState(true),
        progressbar.OptionSetElapsedTime(false),
        progressbar.OptionSetPredictTime(false),
    )
    return &progressBarWrapper{inner: pb}
}
```

**理由**：
- 窄接口（仅 2 方法）——ISP；我们只调 `ChangeMax64` 和 `Set64`（源码 `appstore_download.go:146-147`）。
- nil-safe——ipatool 的 `downloadFile` 检查 `if progress != nil`（`appstore_download.go:145`），nil 时直接 `io.Copy` 无进度条。非交互模式优雅降级（NFR-05）。
- `isTerminal` 检测 `os.Stdout` 是否 TTY——用 `golang.org/x/term` 的 `IsTerminal(int(fd.Fd()))`（go-ios 已传递依赖）。

#### DD-06：CLI 命令结构

**决策**：

| 命令 | 文件 | 说明 |
|------|------|------|
| `download <bundle-id>` | `internal/cli/download.go` (NEW) | 顶层命令（从 install 组移出） |
| `apps search <term>` | `internal/cli/apps.go` (MODIFY) | 填实 search |
| `library list` | `internal/cli/library.go` (NEW) | 列出 active profile 的 IPA |
| `library clean [bundle-id]` | `internal/cli/library.go` (NEW) | 清理（全部/单 app） |

**命令注册**（`root.go` 修改）：
```go
func newRootCmd(deps Deps) *cobra.Command {
    root := &cobra.Command{...}
    root.AddCommand(
        authCmd(deps),
        accountCmd(deps),
        appsCmd(deps),         // ← 改为接收 deps
        downloadCmd(deps),     // ← NEW 顶层
        libraryCmd(deps),      // ← NEW
        devicesCmd(),
        installCmd(),          // ← download 子命令移除
        doctorCmd(),
    )
    return root
}
```

**Flags**：
```
download <bundle-id>
  --profile <id>          指定 profile（缺省 active）
  --output <path>         自定义输出路径
  --force                 强制覆盖已存在
  --external-version-id <id>  指定版本

apps search <term>
  --profile <id>          指定 profile
  --limit <N>             结果数上限（默认 5，与 ipatool 一致）

library list
  --profile <id>          指定 profile

library clean [bundle-id]
  --profile <id>          指定 profile
```

**`install download` 移除**：从 `installCmd()` 的 `AddCommand` 中移除 `installDownloadCmd()`。`install` 组仅保留 `push` / `uninstall` / `update`（均为 stub，未来 mission）。

#### DD-07：Profile 解析器（`--profile` flag 共享逻辑）

**决策**：在 `internal/cli/helpers.go` 新增 `resolveProfile()` 函数，所有需要 profile 的命令共用。

```go
// resolveProfile resolves the target profile from --profile flag or active.
// Returns ErrProfileNotFound / ErrProfileNotLoggedIn / ErrNoActiveProfile.
// requireCredentials=true 时校验凭据（search/download 需要；library list/clean 不需要）。
func resolveProfile(deps Deps, profileFlag string, requireCredentials bool) (account.Profile, error) {
    var profile account.Profile
    var err error

    if profileFlag != "" {
        profile, err = deps.Store.Get(profileFlag)
        if err != nil {
            return account.Profile{}, fmt.Errorf("%w. Run `accounts list` to see available profiles", apperr.ErrProfileNotFound)
        }
    } else {
        activeID, err := deps.Store.GetActiveID()
        if err != nil {
            return account.Profile{}, err
        }
        if activeID == "" {
            return account.Profile{}, fmt.Errorf("%w. Run `accounts use <profile-id>` to set one", apperr.ErrNoActiveProfile)
        }
        profile, err = deps.Store.Get(activeID)
        if err != nil {
            return account.Profile{}, err
        }
    }

    if requireCredentials {
        has, err := deps.Store.HasCredentials(profile.ID)
        if err != nil {
            return account.Profile{}, err
        }
        if !has {
            return account.Profile{}, fmt.Errorf("%w. Run `auth login` to authenticate", apperr.ErrProfileNotLoggedIn)
        }
    }

    return profile, nil
}
```

**理由**：
- 所有命令共享同一校验顺序：存在性 → 凭据（如需）→ 返回 profile。
- `requireCredentials` 参数区分：search/download 需要凭据（调 Apple API）；library list/clean 不需要（纯本地操作）。
- 错误消息含下一步建议（NFR-06）。

#### DD-08：错误类型映射

**决策**：adapter 将 ipatool 的 sentinel errors 映射为我们的 sentinel errors。CLI 层 catch 并格式化。

| 我们的 Sentinel | ipatool 来源 | 触发条件 | stderr 模板 |
|----------------|-------------|----------|-------------|
| `ErrAppNotFound` | `errors.New("app not found")` (lookup) | bundle-id 无搜索结果 | `app not found: <bundle-id>. Verify the bundle identifier.` |
| `ErrLicenseRequired` | `ipaappstore.ErrLicenseRequired` | download 返回 FailureTypeLicenseNotFound | （内部处理，不直接报错——走 license retry） |
| `ErrPaidAppNotSupported` | （我们生成） | app.Price > 0 且 ErrLicenseRequired | `paid apps are not supported. Only free apps can be downloaded.` |
| `ErrPasswordTokenExpired` | `ipaappstore.ErrPasswordTokenExpired` | download/purchase 返回 FailureTypePasswordTokenExpired | （内部处理——走 token retry） |
| `ErrDownloadNonInteractive` | （我们生成） | 非交互模式遇 license prompt | `license acquisition requires interactive confirmation; cannot proceed non-interactively` |
| `ErrReplicateSinfFailed` | （wrap ipatool error） | ReplicateSinf 返回错误 | `failed to apply DRM keys: <err>. The IPA may not be installable.` |

**新增 sentinels**（`internal/apperr/errors.go`）：
```go
var (
    ErrAppNotFound            = errors.New("app not found")
    ErrPaidAppNotSupported    = errors.New("paid apps are not supported")
    ErrDownloadNonInteractive = errors.New("interactive confirmation required")
    ErrReplicateSinfFailed    = errors.New("failed to apply DRM keys")
)
```

**ipatool error 映射**（adapter 内部 `mapDownloadError`）：
```go
func mapDownloadError(err error) error {
    if errors.Is(err, ipaappstore.ErrLicenseRequired) {
        return ErrLicenseRequired // re-export as our sentinel
    }
    if errors.Is(err, ipaappstore.ErrPasswordTokenExpired) {
        return ErrPasswordTokenExpired
    }
    return err // unknown: pass through
}
```

**ErrLicenseRequired / ErrPasswordTokenExpired 的处理**：这两个不是"报错给用户"的错误——它们是 download 编排的**控制流信号**（DD-04 的 retry 子流程）。CLI 层 catch 后走 retry 逻辑，不直接打印。

#### DD-09：`--output` 路径校验

**决策**：在 download 编排中，`--output` 给定时做以下校验（AC-10-4/5/6）：

```go
func validateOutputPath(path string) error {
    // 1. Check if path is an existing directory
    info, err := os.Stat(path)
    if err == nil && info.IsDir() {
        return fmt.Errorf("output path is a directory: %s", path)  // AC-10-5
    }
    // 2. Check parent directory exists
    parent := filepath.Dir(path)
    if _, err := os.Stat(parent); os.IsNotExist(err) {
        return fmt.Errorf("output directory does not exist: %s", parent)  // AC-10-4
    }
    // 3. Check write permission on parent
    if err := access(parent, os.O_WRONLY); err != nil {
        return fmt.Errorf("cannot write to output path: %s (permission denied)", path)  // AC-10-6
    }
    return nil
}
```

#### DD-10：非交互模式检测

**决策**：用 `golang.org/x/term.IsTerminal(int(os.Stdin.Fd()))` 检测。go-ios 已传递依赖该包。

```go
func isInteractive() bool {
    return term.IsTerminal(int(os.Stdin.Fd()))
}
```

**使用点**：
- `download` 的 license prompt（AC-02-11）：非交互 → 报错 exit 1
- `download` 的 progress bar（DD-05）：非交互 → nil progress（无进度条）
- `library clean` 的确认提示（AC-05-9）：非交互 + 有文件可删 → 报错 exit 1

#### DD-11：Deps 扩展

**决策**：`Deps` struct 新增 `LibraryStore` 字段。

```go
type Deps struct {
    Store           account.Store            // existing
    AppStoreFactory appstore.AppStoreFactory // existing
    UI              ui.Prompter              // existing
    ConfigRoot      string                   // existing
    LibraryStore    library.Store            // NEW (DD-02)
}
```

**`newProductionDeps` 扩展**：
```go
func newProductionDeps() (Deps, error) {
    // ... existing ...
    return Deps{
        Store:           account.NewStore(paths.Config, baseKeychain),
        AppStoreFactory: appstore.NewAppStoreFactory(paths.Root),
        UI:              ui.NewPrompter(),
        ConfigRoot:      paths.Root,
        LibraryStore:    library.NewStore(paths.Library),  // NEW
    }, nil
}
```

#### DD-12：Mock 扩展策略

**决策**：扩展现有 `mockAppStore`（`auth_test.go`）以实现新接口方法。新方法用可配置函数字段（与 login 的 slice 模式一致）。

```go
type mockAppStore struct {
    // --- existing auth fields (unchanged) ---
    endpoint     string
    endpointErr  error
    loginResults []appstore.LoginResult
    loginErrors  []error
    loginCalls   int
    revokeErr    error
    revokeCalled bool

    // --- NEW: query fields ---
    accountInfoResult appstore.AccountInfoResult
    accountInfoErr    error
    accountInfoCalled bool
    lookupResult      appstore.AppInfo
    lookupErr         error
    searchResults     []appstore.AppInfo
    searchErr         error

    // --- NEW: download fields ---
    downloadResult   appstore.DownloadResult
    downloadErr      error
    downloadCalls    int
    purchaseErr      error
    purchaseCalled   bool
    replicateSinfErr error
}

// New methods — return configured values or zero values when not set.
// This ensures existing auth tests don't break (they never call these).
func (m *mockAppStore) AccountInfo() (appstore.AccountInfoResult, error) {
    m.accountInfoCalled = true
    return m.accountInfoResult, m.accountInfoErr
}
func (m *mockAppStore) Lookup(string) (appstore.AppInfo, error) {
    return m.lookupResult, m.lookupErr
}
func (m *mockAppStore) Search(string, int64) ([]appstore.AppInfo, error) {
    return m.searchResults, m.searchErr
}
func (m *mockAppStore) Download(appstore.DownloadInput) (appstore.DownloadResult, error) {
    m.downloadCalls++
    return m.downloadResult, m.downloadErr
}
func (m *mockAppStore) Purchase(string, int64, float64) error {
    m.purchaseCalled = true
    return m.purchaseErr
}
func (m *mockAppStore) ReplicateSinf([]appstore.Sinf, string) error {
    return m.replicateSinfErr
}
```

**理由**：
- 零值默认——auth 测试不配置新字段时，新方法返回零值（不 panic），编译通过，行为不变。
- download/search 测试配置需要的字段，其他留零值。
- 新增 `mockLibraryStore`（在 `library_test.go` 或 `download_test.go`）实现 `library.Store` 接口，同样用可配置字段。

---

## 3. Data Models, State & Interfaces

### 3.1 数据模型（持久化）

**`<configRoot>/library/<profileID>/index.json`**（DD-02）：
```json
{
  "entries": [
    {
      "bundle_id": "string (unique within profile)",
      "app_id": "int64",
      "version": "string",
      "file_path": "string (absolute path to .ipa)",
      "file_size": "int64 (bytes)",
      "downloaded_at": "RFC3339 timestamp (UTC)"
    }
  ]
}
```

**IPA 文件**（`<configRoot>/library/<profileID>/`）：
- 文件名：`<bundleID>_<appID>_<version>.ipa`
- 内容：ipatool 下载 + ReplicateSinf 处理后的 IPA（含 DRM 密钥 + iTunesMetadata.plist）

### 3.2 数据模型（进程内）

**`library.Entry`**（见 DD-02）—— per-profile 索引条目。

**`library.indexFile`**（内部，不导出）：
```go
type indexFile struct {
    Entries []Entry `json:"entries"`
}
```

**`library.store`**（内部实现 struct）：
```go
type store struct {
    libraryRoot string  // <configRoot>/library
}
```

### 3.3 接口契约

**`library.Store`**（见 DD-02）。

**`library.NewStore`**：
```go
// NewStore constructs a Store backed by the given library root directory.
// Each profile's data lives under <libraryRoot>/<profileID>/.
func NewStore(libraryRoot string) Store
```

**`appstore.ProfileAppStore`**（扩展后，见 DD-01）。

### 3.4 状态转换

**Library entry 生命周期**：
```
                    download (success)
  (不存在) ──────────────────────► INDEXED
                                     │
                                     │ library clean <bundle-id>
                                     │ library clean (all)
                                     ▼
                                 (不存在)

  INDEXED + 文件被外部删除 ──► STALE (索引有但文件无)
                                     │
                                     │ library clean <bundle-id> (AC-05-8)
                                     │ library list (显示 + 标注 absent?)
                                     ▼
                                 (不存在)
```

**注**：`library list` 对 STALE 条目的行为——显示条目但 `file_path` 指向不存在的文件。设计选择：list 照常显示（用户可见索引记录），clean 时 AC-05-8 处理（幂等移除）。不在 list 中做文件存在性检查（避免每次 list 都 stat 全部文件，NFR-10）。

---

## 4. Code Structure（文件映射）

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/appstore/query.go` | `AppInfo` / `AccountInfoResult` 类型定义 + `appToAppInfo` / `appInfoToApp` 转换 helper |
| `internal/appstore/download_types.go` | `DownloadInput` / `DownloadResult` / `Sinf` / `Progress` 类型定义 |
| `internal/appstore/progress.go` | `progressBarWrapper` + `NewProgress()` + `isInteractive()` |
| `internal/appstore/errors.go` (扩展) | `ErrLicenseRequired` / `ErrPasswordTokenExpired` 别名（映射 ipatool sentinels） |
| `internal/library/store.go` | **完全重写**：`Store` 接口 + `store` 实现 + JSON 读写 + 文件管理 |
| `internal/library/store_test.go` | Store 单元测试（temp dir 隔离） |
| `internal/cli/download.go` | 顶层 `download` 命令 + DD-04 编排 |
| `internal/cli/download_test.go` | download 命令测试（mock AppStore + mock LibraryStore） |
| `internal/cli/library.go` | `library list` + `library clean` 命令 |
| `internal/cli/library_test.go` | library 命令测试 |

### 修改文件

| 文件 | 修改 |
|------|------|
| `internal/appstore/adapter.go` | 扩展 `ProfileAppStore` 接口（+6 方法）；新增 `AccountInfo` / `Lookup` / `Search` / `Download` / `Purchase` / `ReplicateSinf` 的 adapter 方法声明位置 |
| `internal/appstore/client_impl.go` | 实现 6 个新 adapter 方法；adapter struct 加 `account *ipaappstore.Account` 缓存字段；`mapDownloadError` |
| `internal/appstore/apps.go` | **删除** stub `Search` / `Download`（迁移到 adapter + cli 层）；保留包注释 |
| `internal/library/ipa_store.go` | **删除**旧 stub（`IPAStore` struct / `Path` / `Add` / `List`），由 `store.go` 取代 |
| `internal/cli/apps.go` | `appsCmd()` 改为接收 `deps Deps`；`appsSearchCmd()` 填实（DD-07 resolveProfile + Search） |
| `internal/cli/install.go` | 移除 `installDownloadCmd()`；`installCmd()` 的 `AddCommand` 移除 download |
| `internal/cli/root.go` | `appsCmd(deps)` 传参；新增 `downloadCmd(deps)` + `libraryCmd(deps)` |
| `internal/cli/deps.go` | `Deps` 加 `LibraryStore library.Store` 字段；`newProductionDeps` 加 `library.NewStore(paths.Library)` |
| `internal/cli/helpers.go` | 新增 `resolveProfile()` (DD-07) + `validateOutputPath()` (DD-09) + `isInteractive()` (DD-10) |
| `internal/cli/auth_test.go` | `mockAppStore` 扩展 6 个新方法（零值默认，DD-12） |
| `internal/apperr/errors.go` | 新增 `ErrAppNotFound` / `ErrPaidAppNotSupported` / `ErrDownloadNonInteractive` / `ErrReplicateSinfFailed` |
| `go.mod` | `schollz/progressbar/v3` 从 indirect 转 direct；新增 `golang.org/x/term` direct（如尚未） |

### 不变文件（含理由）

| 文件 | 理由 |
|------|------|
| `internal/account/*` | profile 管理不变（Store / ProfileKeychain / Profile 均复用） |
| `internal/appstore/factory.go` | `AppStoreFactory` 签名不变（`func(p) (ProfileAppStore, error)`），只是返回的接口方法多了 |
| `internal/appstore/client.go` | 包注释 + KeychainServiceName 常量不变 |
| `internal/config/*` | 路径定义不变（`Paths.Library` 已存在） |
| `internal/ui/*` | Prompter 接口不变（`Confirm` 复用；新增 download 不需新 prompt 方法） |
| `internal/device/*` | 非本 mission 范围 |
| `internal/doctor/*` | 非本 mission 范围 |
| `internal/cli/account.go` | accounts 命令不变 |
| `internal/cli/auth.go` | auth 命令不变 |

---

## 5. Processing Flows

### 5.1 `download <bundle-id>` — Happy Path

```
User: ipa-manager download com.tencent.xin
  │
  ├─ [resolveProfile(deps, --profile="", requireCreds=true)]
  │    └─ → Profile{ID:"alice_example_com", ...}
  │
  ├─ [factory(profile)] → appStore
  ├─ [appStore.AccountInfo()] → AccountInfoResult{Email, Name, StoreFront}
  │    └─ adapter internally caches full Account
  │
  ├─ [appStore.Lookup("com.tencent.xin")] → AppInfo{ID:123456789, Name:"WeChat", Version:"8.0.34", Price:0}
  │
  ├─ [resolveOutputPath(--output="")] → "/home/.ipa-manager/library/alice_example_com/com.tencent.xin_123456789_8.0.34.ipa"
  │
  ├─ [libraryStore.Get("alice_example_com", "com.tencent.xin")]
  │    └─ → ErrEntryNotFound (not in index) → continue
  │
  ├─ [progress := NewProgress()] → progressBarWrapper (TTY) or nil
  │
  ├─ [appStore.Download(DownloadInput{BundleID, AppID, OutputPath, Progress})]
  │    └─ → DownloadResult{DestinationPath, Version:"8.0.34", Sinfs}
  │
  ├─ [appStore.ReplicateSinf(Sinfs, DestinationPath)]
  │    └─ → nil (DRM keys applied)
  │
  ├─ [libraryStore.Add("alice_example_com", Entry{BundleID, AppID, Version, FilePath, FileSize, DownloadedAt})]
  │
  └─ print "✓ Downloaded: WeChat 8.0.34 → /home/.ipa-manager/library/alice_example_com/com.tencent.xin_123456789_8.0.34.ipa"
     exit 0 (AC-02-1)
```

### 5.2 `download` — License Required (free app, interactive)

```
User: ipa-manager download com.example.freeapp
  │
  ├─ ... (AccountInfo, Lookup, existence check) ...
  │
  ├─ [appStore.Download(...)]
  │    └─ ✗ ErrLicenseRequired (FailureTypeLicenseNotFound)
  │
  ├─ app.Price == 0? → YES
  ├─ isInteractive()? → YES
  ├─ [ui.Confirm("this app requires a free license. acquire?")]
  │    └─ user: YES
  │
  ├─ [appStore.Purchase("com.example.freeapp", appID, price=0)]
  │    └─ → nil (license acquired)
  │
  ├─ [appStore.Download(...)] → retry
  │    └─ → DownloadResult{...} ✓
  │
  ├─ [appStore.ReplicateSinf(...)]
  ├─ [libraryStore.Add(...)]
  └─ print "✓ Downloaded: ..." exit 0 (AC-02-7)
```

### 5.3 `download` — Token Expired (auto re-login)

```
  ├─ [appStore.Download(...)]
  │    └─ ✗ ErrPasswordTokenExpired
  │
  ├─ [appStore.Login(LoginInput{Email: account.Email, Password: account.Password})]
  │    └─ ✓ (re-authenticated, keychain updated by ipatool)
  │
  ├─ [appStore.Download(...)] → retry
  │    └─ → DownloadResult{...} ✓
  │
  └─ continue to ReplicateSinf → Add → success (AC-02-10)
```

### 5.4 `download` — Already Exists (skip)

```
  ├─ [libraryStore.Get(profileID, bundleID)]
  │    └─ → Entry{Version:"8.0.34", FilePath:"..."} (found, same version)
  │
  ├─ --force? → NO
  │
  └─ print "already exists: <path> (use --force to overwrite)"
     exit 0 (AC-02-5, idempotent)
```

### 5.5 `apps search <term>` — Happy Path

```
User: ipa-manager apps search wechat --limit 5
  │
  ├─ [resolveProfile(deps, --profile="", requireCreds=true)]
  ├─ [factory(profile)] → appStore
  ├─ [appStore.AccountInfo()] → accountResult
  ├─ [appStore.Search("wechat", 5)] → []AppInfo{
  │      {ID:123456789, BundleID:"com.tencent.xin", Name:"WeChat", Version:"8.0.34", Price:0},
  │      {ID:987654321, BundleID:"com.tencent.wx", Name:"WeChat for iPad", ...},
  │      ...
  │    }
  │
  └─ [lipgloss Table] render:
       NAME              │ BUNDLE-ID         │ VERSION │ PRICE
       WeChat            │ com.tencent.xin   │ 8.0.34  │ Free
       WeChat for iPad   │ com.tencent.wx    │ 8.0.34  │ Free
       ...
      exit 0 (AC-01-1)
```

### 5.6 `library list` — Happy Path

```
User: ipa-manager library list
  │
  ├─ [resolveProfile(deps, --profile="", requireCreds=false)]  ← 不需要凭据
  ├─ [libraryStore.List(profile.ID)] → []Entry
  │    └─ empty → "no IPAs in library for profile '<id>'" exit 0 (AC-04-2)
  │
  └─ [lipgloss Table] render:
       BUNDLE-ID         │ VERSION │ SIZE     │ DOWNLOADED
       com.tencent.xin   │ 8.0.34  │ 234.5 MB │ 2026-07-01 12:00
       com.example.app   │ 1.2.3   │ 45.6 MB  │ 2026-07-01 11:30
      exit 0 (AC-04-1)
```

### 5.7 `library clean` (no args) — Happy Path

```
User: ipa-manager library clean
  │
  ├─ [resolveProfile(deps, --profile="", requireCreds=false)]
  ├─ [libraryStore.List(profile.ID)] → []Entry (N=3, M=1 custom path)
  │    └─ empty → "library is already empty" exit 0 (AC-05-2)
  │
  ├─ isInteractive()? → YES
  ├─ print "remove all 3 IPA(s) for profile 'alice_example_com'? [y/N]"
  │        "  - /Users/x/custom/app.ipa"   ← 披露自定义路径 (AC-05-1, R2-F-01)
  ├─ [ui.Confirm(...)]
  │    └─ YES
  │
  ├─ [libraryStore.CleanAll(profile.ID)] → count=3
  └─ print "✓ Removed 3 IPA(s)." exit 0 (AC-05-1)
```

### 5.8 `library clean <bundle-id>` — File Already Absent

```
User: ipa-manager library clean com.example.app
  │
  ├─ [libraryStore.Get(profile.ID, "com.example.app")] → Entry{FilePath:"/.../app.ipa"}
  ├─ [ui.Confirm("remove 'com.example.app' (version 1.2.3, 45.6 MB)? [y/N]")] → YES
  │
  ├─ [libraryStore.Remove(profile.ID, "com.example.app")]
  │    └─ os.Remove(FilePath) → os.IsNotExist (file already gone externally)
  │    └─ remove index entry anyway
  │
  └─ print "file already absent for 'com.example.app'" exit 0 (AC-05-8, idempotent)
```

### 5.9 `library clean` — Non-interactive (destructive)

```
User: echo "" | ipa-manager library clean   (stdin is pipe, not TTY)
  │
  ├─ [libraryStore.List(profile.ID)] → []Entry (N=3)
  ├─ isInteractive()? → NO
  │
  └─ print "confirmation required in non-interactive mode; cannot proceed"
     exit 1 (AC-05-9)
```

### 5.10 Failure Paths 汇总

| 场景 | 触发 | 行为 | AC |
|------|------|------|----|
| Profile not found | `--profile <bad-id>` | "profile not found" + exit 1 | AC-08-1 |
| Profile not logged in | active profile 无凭据 | "has no credentials" + exit 1 | AC-08-2 |
| No active profile | 无 `--profile`，active 为空 | "no active profile" + exit 1 | AC-02-3 |
| App not found | Lookup 返回 0 结果 | "app not found: <bundle-id>" + exit 1 | AC-02-4 |
| Paid app + license required | ErrLicenseRequired + Price > 0 | "paid apps not supported" + exit 1 | AC-02-8 |
| License required + non-interactive | ErrLicenseRequired + 非 TTY | "interactive confirmation required" + exit 1 | AC-02-11 |
| Output dir missing | `--output /bad/path` | "output directory does not exist" + exit 1 | AC-10-4 |
| Output is directory | `--output /existing/dir` | "output path is a directory" + exit 1 | AC-10-5 |
| Permission denied | `--output` 父目录无写权限 | "cannot write to output path" + exit 1 | AC-10-6 |
| Invalid --limit | `--limit 0` / `-1` / `"abc"` | "invalid --limit value" + exit 1 | AC-01-6 |
| ReplicateSinf fails | DRM 写入失败 | "failed to apply DRM keys" + exit 1 | NFR-02 |

---

## 6. Impact Analysis

### 6.1 兼容性风险

| 维度 | 影响 | 说明 |
|------|------|------|
| **ipatool 升级** | 中 | adapter 直接依赖 `ipaappstore.SearchInput` / `LookupInput` / `DownloadInput` / `AccountInfoOutput` 等签名。ipatool v2.x 内若改这些，编译断（adapter 有编译期接口断言 `var _ ProfileAppStore = (*profileAppStoreAdapter)(nil)`）。 |
| **ProfileAppStore 接口扩展** | 低-中 | 现有 auth 测试的 `mockAppStore` 需加 6 个零值方法（DD-12）。机械修改，不改变 auth 测试逻辑。 |
| **`install download` 移除** | 低 | v1 未发布，无外部用户影响。`install --help` 输出不再含 `download` 子命令。 |
| **progressbar 依赖** | 低 | 已在 go.sum（transitive），转 direct 仅 go.mod 变更。 |

### 6.2 迁移需求

**N/A**。v1 首次发布。`library/` 目录不存在时，首次 download 自动创建（`os.MkdirAll`）。

### 6.3 安全/隐私

| 项 | 评估 |
|----|------|
| **Account.Password 暴露** | `AccountInfoResult` **故意不含** Password/PasswordToken（DD-01）。adapter 内部缓存完整 Account 用于 Apple API 调用，但不通过接口泄露。CLI 层永远不接触凭据字段（NFR-04）。 |
| **Token 过期重登录** | 使用 keychain 中存储的 Password（A-03）。Password 在 keychain 加密存储（macOS Keychain）。重登录后 ipatool 更新 keychain 中的 token。 |
| **IPA 文件内容** | iTunesMetadata.plist 含 email（Apple 固有行为，A-06）。IPA 本身不含 password/token。本地磁盘明文存储可接受。 |
| **Library 索引** | index.json 不含凭据。仅 bundle-id / version / size / path / timestamp。 |
| **日志输出** | download 成功消息含文件路径（不含凭据）。错误消息含 Apple 返回的失败原因（不含凭据）。 |

### 6.4 性能/可靠性

| 项 | 评估 |
|----|------|
| **Search 延迟** | iTunes Search API 通常 < 2s；本工具额外开销 < 200ms（profile 解析 + keychain 读）。NFR-10: < 5s 端到端。 |
| **Download 延迟** | 取决于 IPA 大小（通常 10-500MB）+ 网络速度。进度条提供反馈（NFR-05）。 |
| **原子下载** | ipatool 内部先写 `.tmp` 再 rename（`appstore_download.go:83-96`）。我们额外做 ReplicateSinf（也是 tmp + rename，`appstore_replicate_sinf.go:33-86`）。两层原子保护。 |
| **Library 索引一致性** | index.json 原子写（tmp + rename）。download 成功后 Add，失败不 Add（文件可能存在但索引无记录——下次 download 会检测到文件存在并跳过，AC-02-5）。 |
| **并发** | 同 profile 并发 download 行为未定义（R3 继承）。不同 profile 并发安全（独立目录 + 独立 keychain）。 |

### 6.5 可观测性

- download 输出阶段进度：`"Looking up app..."` → `"Downloading..."` (progress bar) → `"Applying DRM keys..."` → `"✓ Downloaded"`。
- search 输出表格。
- library list/clean 输出表格 + 确认提示。
- 无结构化日志（v1 不需要；个人工具）。

### 6.6 Rollout/Rollback

**N/A**。单二进制，无服务端。用户替换二进制升级/回退。library 目录 schema 向前兼容（新增字段 omitempty）。

---

## 7. Design Completeness Self-Check

implementation 阶段能否不猜谜地推进？

- [x] **每个 ipatool API 已验证**：AccountInfo / Lookup / Search / Download / Purchase / ReplicateSinf —— 全部从 `v2.3.1-fix-auth.5` 源码读到，签名非猜测。
- [x] **数据格式确定**：index.json schema（DD-02）、IPA 文件名规则（DD-03）、路径布局。
- [x] **每个 AC 有对应处理流**：§5 的 10 个流程图覆盖 42 个 AC 的所有 Then 子句。
- [x] **错误路径有定义**：DD-08 错误映射表 + §5.10 失败路径汇总。
- [x] **接口完整**：ProfileAppStore（+6 方法）、library.Store（5 方法）、Deps（+LibraryStore）。
- [x] **文件修改清单完整**：§4 列出每个新增/修改/不变文件。
- [x] **Mock 策略确定**：DD-12 mockAppStore 扩展方案。
- [x] **无 "TODO: figure out"**：所有设计点都有决策 + 理由 + 备选。

→ 可进入 plan 阶段做任务分解。
