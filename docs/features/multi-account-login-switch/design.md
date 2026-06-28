# Design — multi-account-login-switch

> 本设计基于 `requirements.md`（v2，含 NFR-05 design-phase 修正）和 ipatool v2.3.0 源码实证研究。所有外部 API 签名均从 module cache 中的实际源码验证，非猜测。

---

## 1. Goals & Non-Goals（重述）

### 满足的 User Stories

| US | 描述 | 设计覆盖章节 |
|----|------|------------|
| US-01 (P1) | 首次添加账号（login = add + login + refresh）| §3.2 `auth login` 流程，DD-01/05 |
| US-02 (P1) | 切换账号 | §3.3 `accounts use` 流程，DD-07 |
| US-03 (P1) | 列举账号与状态 | §3.4 `accounts list` 流程，DD-06 |
| US-04 (P2) | 删除账号 | §3.5 `accounts remove` 流程，DD-08 |
| US-05 (P2) | 登出但保留元数据 | §3.6 `auth logout` 流程 |
| US-06 (P2) | 2FA 登录 | §3.2 2FA 重试子流程 |
| US-07 (P3) | 错误可读性 | DD-09 错误映射 |

### Non-goals（约束设计的边界）

- **不修改 ipatool 源码**——隔离完全通过依赖注入（ADR 0002）。
- **不实现并发互斥**——同 profile 并发操作行为未定义（R3）。
- **不做密码脱敏**——ipatool 把含 Password 的 account JSON 写入 keychain 是固有行为，本项目接受（NFR-05 已修订）。
- **不引入新的外部依赖**——除脚手架已锁定的（ipatool / go-ios / cobra / huh / lipgloss / testify），仅新增 ipatool 已引入的传递依赖 `github.com/juju/persistent-cookiejar` 和 `github.com/99designs/keyring`（已在 go.sum 中）。
- **不做 `--email` / `--password` / `--id` flag**——v1 纯交互式，保持 CLI 简单。

---

## 2. Architecture & Key Decisions

### 组件总览

```
┌──────────────────────────────────────────────────────────────────┐
│                       CLI Layer (internal/cli)                    │
│   auth.go      │  account.go    │                                 │
│   login/logout │  list/use/remove                                  │
└───────┬────────┴───────┬────────┴──────────────────────────────────┘
        │                │
        ▼                ▼
┌───────────────┐ ┌──────────────────┐
│ account.Store │ │ appstore.Client  │
│ (profile CRUD │ │ (factory: builds │
│  + active)    │ │  per-profile     │
│  config.json) │ │  AppStore)       │
└───────┬───────┘ └────────┬─────────┘
        │                  │
        │           ┌──────┴───────┐
        │           ▼              ▼
        │   ┌─────────────┐ ┌──────────────┐
        │   │ account.     │ │ cookiejar    │
        │   │ ProfileKey-  │ │ (per-profile │
        │   │ chain        │ │ file)        │
        │   └──────┬───────┘ └──────┬───────┘
        │          │                │
        ▼          ▼                │
┌────────────────────────┐          │
│ macOS Keychain         │          │
│ (via 99designs/keyring)│          │
│ key: profiles/<id>/... │          │
└────────────────────────┘          │
                                    ▼
                            ┌───────────────┐
                            │ ipatool       │
                            │ appstore.     │
                            │ AppStore      │
                            │ (Apple API)   │
                            └───────────────┘
```

### 核心设计决策

#### DD-01：`appstore.NewProfileAppStore(profile)` 工厂接线

**决策**：单一工厂入口，接收 `account.Profile`，输出绑定了 per-profile 隔离的 `appstore.AppStore`。

**接线步骤**（全部源自 ipatool `cmd/common.go` 参考实现）：
1. `keyring.Open(keyring.Config{ServiceName: "ipa-manager", AllowedBackends: [KeychainBackend], FileDir, FilePasswordFunc})` → `keyring.Keyring`
2. `ipakeychain.New(ipakeychain.Args{Keyring: ring})` → `ipakeychain.Keychain`
3. `account.ProfileKeychain{Base: that, ProfileID: profile.ID}` → namespacing wrapper
4. `cookiejar.New(&cookiejar.Options{Filename: account.CookieJarPath(profile.ID, configRoot)})` → `http.CookieJar`
5. `operatingsystem.New()` + `machine.New(machine.Args{OS: os})` → 共享单例
6. `appstore.NewAppStore(appstore.Args{Keychain: profileKeychain, CookieJar: jar, OperatingSystem: os, Machine: machine})` → `appstore.AppStore`

**关键签名（源码验证）**：
```go
// ipatool pkg/appstore/appstore.go
type Args struct {
    Keychain        keychain.Keychain
    CookieJar       http.CookieJar
    OperatingSystem operatingsystem.OperatingSystem
    Machine         machine.Machine
}
func NewAppStore(args Args) AppStore

// ipatool pkg/http/cookiejar.go
type CookieJar interface {
    http.CookieJar
    Save() error
}

// ipatool pkg/keychain/keychain.go
type Keychain interface {
    Get(key string) ([]byte, error)
    Set(key string, data []byte) error
    Remove(key string) error
}
```

**备选方案**：
- 每次操作都临时构造 AppStore（无缓存）—— **被否**：keyring.Open 有 Keychain 解锁开销，应按命令缓存。
- 全局单例 AppStore —— **被否**：多 profile 需要不同 keychain/cookiejar 注入，单例做不到。

#### DD-02：Keyring ServiceName 与后端选择

**决策**：
- `ServiceName = "ipa-manager"`（与 raw ipatool 的 `"ipatool-auth.service"` 隔离，避免 keychain item 冲突）
- `AllowedBackenders = [keyring.KeychainBackend]`（v1 仅 macOS Keychain，无 file fallback）
- `FileDir = ~/.ipa-manager/keychain`（file backend 目录，v1 不使用但 keyring.Config 需要填充）
- `FilePasswordFunc`：v1 不触发（仅 KeychainBackend），返回 error 占位

**理由**：v1 只支持 macOS（NFR-08），KeychainBackend 是原生选择。File backend 作为 fallback 会引入明文 keyring 文件的密钥管理问题，不值得。若 Keychain 不可用（R5），直接报错。

#### DD-03：config.json 单文件 schema

**决策**：`~/.ipa-manager/config.json` 持有全部状态：

```json
{
  "active_profile_id": "alice_example_com",
  "profiles": [
    {
      "id": "alice_example_com",
      "name": "Alice Smith",
      "email": "alice@example.com",
      "store_front": "143441-1,29"
    }
  ]
}
```

**空状态**（无 profile）：
```json
{}
```
（文件可能不存在，或存在但为空 JSON。）

**Schema 约束**：
- `active_profile_id` 可为空字符串（无 active）或必须指向 `profiles[]` 中存在的 `id`
- `profiles[]` 的 `id` 字段唯一
- `store_front` 由 ipatool Login 返回填充，可选（`omitempty`）

#### DD-04：`account.Store` 作为 config.json 单一所有者

**决策**：`account.Store` 读写整个 config.json（含 `active_profile_id` 和 `profiles[]`）。`config.Config` / `config.Load` / `config.Save` **弃用**（scaffold 遗留，v1 不使用，保留文件但不接线）。

**理由**：
- 避免 `config` ↔ `account` 循环依赖（`config.Config` 若持有 `[]account.Profile` 则 config→account；`account.Store` 若调 `config.Load` 则 account→config）
- 单文件 = 单一所有者原则
- `config.Paths` 保留（路径常量仍是 config 包职责）

**Store 接口**：
```go
type Store struct {
    configPath string  // resolved path to config.json
    keyringBackend keyring.Keyring  // shared for credential checks
}

// Profile CRUD
func (s *Store) List() ([]Profile, error)
func (s *Store) Get(id string) (Profile, error)
func (s *Store) Upsert(p Profile) error  // add or update by ID
func (s *Store) Remove(id string) error  // metadata only; caller handles keychain cascade

// Active profile
func (s *Store) GetActiveID() (string, error)
func (s *Store) SetActive(id string) error  // empty string allowed (clears active)
func (s *Store) ClearActive() error          // convenience: SetActive("")

// Credential state (read-only keychain probe)
func (s *Store) HasCredentials(id string) (bool, error)
```

#### DD-05：`auth login` 流程编排（含 2FA）

**决策**：不使用 ipatool 的 `retry-go` 模式。用**显式两阶段调用**更清晰可控：

```
1. huh Input → email
2. huh Input (masked) → password
3. deriveProfileID(email) → id
4. store.Get(id) → exists? (用于决定 active 行为，不影响 login 本身)
5. appStore := NewProfileAppStore(Profile{ID: id, Email: email})
6. bag, err := appStore.Bag(BagInput{})    ← 必须先取 endpoint
   if err → fail (Apple API 不可达 / 网络问题)
7. output, err := appStore.Login(LoginInput{email, password, AuthCode: "", Endpoint: bag.AuthEndpoint})
8. if errors.Is(err, ErrAuthCodeRequired):
     huh Input → authCode
     output, err = appStore.Login(LoginInput{email, password, AuthCode: authCode, Endpoint: bag.AuthEndpoint})
     if err → fail (含 Apple 返回的失败原因)
   else if err → fail (错误密码 / 账号锁定 / 其他)
9. acc := output.Account
10. store.Upsert(Profile{ID: id, Name: acc.Name, Email: acc.Email, StoreFront: acc.StoreFront})
11. if store.GetActiveID() == "" → store.SetActive(id)    ← 仅首个 profile 自动 active
12. store.Save()
13. print "✓ Logged in: <name> (<email>), profile: <id>"
```

**备选方案**：
- ipatool 的 `retry-go` 模式 —— **被否**：retry-go 是通用重试库，对我们的"恰好两次"语义过度；显式 if 更可读。
- 用 `--auth-code` flag 预传 —— **被否**：v1 纯交互式（Non-goals）。

#### DD-06：Credential 检测（`HasCredentials`）

**决策**：`HasCredentials(id)` 通过构造 `ProfileKeychain` 并调用 `Get("account")` 检测：
- 返回 `nil` error 且数据非空 → `true`（logged-in）
- 返回任何 error → `false`（logged-out）

**关键点**：不构造完整 `AppStore`（避免 cookie jar 文件创建等副作用），只构造 keychain 链路。

**实现**：
```go
func (s *Store) HasCredentials(id string) (bool, error) {
    base := ipakeychain.New(ipakeychain.Args{Keyring: s.keyringBackend})
    pk := ProfileKeychain{Base: base, ProfileID: id}
    _, err := pk.Get("account")
    return err == nil, nil  // keychain read itself doesn't fail fatally
}
```

#### DD-07：`accounts use` 严格校验顺序

**决策**：`accounts use <id>` 按以下顺序短路：
1. profile 存在性 → `ErrProfileNotFound`（AC-02-2）
2. profile 凭据存在性 → `ErrProfileNotLoggedIn`（AC-02-3）
3. 通过 → `store.SetActive(id)` → `store.Save()`

存在性校验先于凭据校验——一个不存在的 ID 不应暴露"是否曾有凭据"的信息（虽然 v1 不在意这个安全细节，但顺序正确）。

#### DD-08：`accounts remove` 级联清理

**决策**：删除按以下顺序，**任一步失败记录但不中断**（best-effort），最后汇总：

```
1. store.Get(id) → exists? → 否则 ErrProfileNotFound
2. huh Confirm("Remove profile '<id>'?") → 否则 exit 0（AC-04-4，非错误）
3. appStore := NewProfileAppStore(profile)
4. err1 := appStore.Revoke()                  ← 移除 keychain "profiles/<id>/account"
5. err2 := os.Remove(CookieJarPath(id, root)) ← 删 cookie jar 文件
6. err3 := store.Remove(id)                   ← 移除 metadata
7. if store.GetActiveID() == id → store.ClearActive()
8. store.Save()
9. 任一 err1/err2/err3 != nil → exit 非零 + 报告失败步骤（NFR-04）
   否则 → exit 0 + "Profile '<id>' removed."
```

**对"不存在文件"错误的处理**：步骤 5（删 cookie jar）遇到 `os.IsNotExist` → 视为成功（幂等，cookie jar 可能从未创建过）。步骤 4（Revoke 即 keychain.Remove）遇到 key 不存在 → 视为成功（ipatool 的 keychain.Remove 透传 keyring 的 not-exist error，我们 wrap 为 nil）。

#### DD-09：错误类型与用户消息映射

**决策**：`internal/apperr/errors.go` 新增 sentinel errors，CLI 层 catch 并格式化。

| Sentinel | 触发条件 | stderr 模板（含下一步建议，AC-07-3） |
|----------|---------|-------------------------------------|
| `ErrProfileNotFound` | `use`/`logout`/`remove` 指定的 ID 不在 profiles[] | `profile '<id>' not found. Run \`accounts list\` to see available profiles.` |
| `ErrProfileNotLoggedIn` | `use` 指向 logged-out profile | `profile '<id>' has no credentials. Run \`auth login\` to authenticate.` |
| `ErrNoActiveProfile` | `logout` 无参数且 active 为空 | `no active profile. Run \`accounts use <profile-id>\` to set one.` |
| `ErrAuthFailed` (wrap) | ipatool Login 返回非 ErrAuthCodeRequired 错误 | Apple 原始消息 + `: verify your credentials and retry.` |
| `Err2FAFailed` (wrap) | 第二次 Login（带 AuthCode）失败 | Apple 原始消息 + `: verify your 2FA code and retry.` |

**范围排除**（AC-07-3 明确）：Ctrl-C（信号终止）、Go panic、ipatool/go-ios 的非 Login 类透传错误不附下一步建议。

#### DD-10：config.json 原子写

**决策**：写入时先写临时文件 `config.json.tmp`，再 `os.Rename` 为 `config.json`。同一文件系统上 rename 是原子的，避免崩溃留下半写文件。

**备选**：直接覆写 —— **被否**：写一半崩溃会损坏整个 config，影响所有 profile。

#### DD-11：命令树调整

**决策**：
- **移除** `accountsAddCmd()`（`internal/cli/account.go`）—— 职责被 `auth login` 吸收
- **移除** 对 `accountsAddCmd()` 的引用（`accountCmd()` 的 `AddCommand` 调用）
- `authLoginCmd()` 改为无参数纯交互式
- `authLogoutCmd()` 增加可选 `[profile-id]` 参数（`cobra.MaximumNArgs(1)`）

**不变项**：`accountsListCmd` / `accountsUseCmd` / `accountsRemoveCmd` 的命令路径与参数契约不变（实现填实）。

---

## 3. Processing Flows

### 3.1 文本流图约定

- `[action]` = 本地操作
- `{action}` = 网络操作（Apple API）
- `✓` / `✗` = 成功 / 失败分支
- `(AC-XX-Y)` = 该步骤对应的 acceptance criteria

### 3.2 `auth login` — Happy Path（新 profile + 2FA）

```
User: ipa-manager auth login
  │
  ├─ [huh Input] email ← "alice@example.com"
  ├─ [huh Input masked] password ← "••••••"
  │
  ├─ [deriveProfileID] id ← "alice_example_com"
  ├─ [store.Get("alice_example_com")] → not found (新 profile)
  │
  ├─ [NewProfileAppStore(Profile{ID, Email})]
  │    └─ {keyring.Open} → {keychain.New} → ProfileKeychain
  │    └─ {cookiejar.New} at ~/.ipa-manager/profiles/alice_example_com/cookies
  │    └─ appstore.NewAppStore(Args{...})
  │
  ├─ {appStore.Bag(BagInput{})} → bag ✓
  │    └─ ✗ network/Apple down → print + exit 1
  │
  ├─ {appStore.Login(email, password, AuthCode:"", bag.AuthEndpoint)}
  │    └─ returns ErrAuthCodeRequired (2FA needed)
  │
  ├─ [huh Input] authCode ← "123456"
  ├─ {appStore.Login(email, password, authCode, bag.AuthEndpoint)}
  │    └─ ✓ success → LoginOutput{Account{Name:"Alice", Email:"alice@...", StoreFront:"..."}}
  │    │      └─ ipatool internally: keychain.Set("account", json) → "profiles/alice_example_com/account"
  │    └─ ✗ 2FA wrong → print Apple error + "verify 2FA" → exit 1 (AC-06-2)
  │
  ├─ [store.Upsert(Profile{ID, Name:"Alice", Email, StoreFront})]
  ├─ [store.GetActiveID()] → "" (首个 profile)
  ├─ [store.SetActive("alice_example_com")] (AC-01-3)
  ├─ [store.Save()] ← atomic write (DD-10)
  │
  └─ print "✓ Logged in: Alice (alice@example.com), profile: alice_example_com"
     exit 0 (AC-01-1, AC-01-3, AC-06-1)
```

### 3.3 `auth login` — Refresh Existing Profile

与 3.2 相同，差异点：
- 步骤 `[store.Get(id)]` → **found**（profile 已存在）
- 不改变 active（AC-01-4）：跳过步骤 `[store.SetActive]`
- `[store.Upsert]` 更新 name/store_front（若 Apple 返回值变了）

### 3.4 `auth login` — Wrong Password（Failure Path）

```
...
├─ {appStore.Login(email, wrongPassword, AuthCode:"", endpoint)}
│    └─ ✗ Apple returns FailureType=invalidCredentials
│         (NOT ErrAuthCodeRequired → 不进 2FA 提示)
│
├─ print Apple's error message + ": verify your credentials and retry."
└─ exit 1 (AC-07-1)
   [no profile metadata written, no keychain entry]
```

### 3.5 `accounts use` — Happy + Failure

```
User: ipa-manager accounts use <id>
  │
  ├─ [store.Get(id)]
  │    ├─ ✗ not found → print ErrProfileNotFound message → exit 1 (AC-02-2)
  │    └─ ✓ found
  │
  ├─ [store.HasCredentials(id)]
  │    ├─ ✗ no keychain entry → print ErrProfileNotLoggedIn → exit 1 (AC-02-3)
  │    └─ ✓ has credentials
  │
  ├─ [store.SetActive(id)]
  ├─ [store.Save()]
  └─ print "✓ Active profile: <id>"
     exit 0 (AC-02-1)
```

### 3.6 `accounts list`

```
User: ipa-manager accounts list
  │
  ├─ [store.List()] → []Profile
  │    └─ empty → print "No profiles configured. Run `auth login` to add one." → exit 0 (AC-03-1)
  │
  ├─ [activeID := store.GetActiveID()]
  │
  ├─ for each profile:
  │    [store.HasCredentials(p.ID)] → loggedIn bool
  │
  └─ [lipgloss Table] render:
       ACTIVE │ ID                   │ EMAIL              │ NAME  │ STATUS
       *      │ alice_example_com    │ alice@example.com  │ Alice │ logged-in
              │ bob_example_com      │ bob@example.com    │ Bob   │ logged-in
              │ charlie_example_com  │ charlie@example.com│ ...   │ logged-out
     exit 0 (AC-03-2, AC-03-3)
```

### 3.7 `accounts remove` — Happy + Cascade

```
User: ipa-manager accounts remove <id>
  │
  ├─ [store.Get(id)]
  │    └─ ✗ not found → print ErrProfileNotFound → exit 1 (AC-04-5)
  │
  ├─ [huh Confirm("Remove profile '<id>'? This deletes credentials and metadata.")]
  │    ├─ "no" → exit 0 (AC-04-4，非错误)
  │    └─ "yes" ↓
  │
  ├─ var cascadeErrors []error
  │
  ├─ [appStore := NewProfileAppStore(profile)]
  ├─ if err := appStore.Revoke(); err != nil && !isKeychainNotExist(err):
  │    cascadeErrors = append(cascadeErrors, "revoke keychain: " + err)
  │
  ├─ if err := os.Remove(CookieJarPath(id, root)); err != nil && !os.IsNotExist(err):
  │    cascadeErrors = append(cascadeErrors, "delete cookie jar: " + err)
  │
  ├─ [store.Remove(id)]  ← metadata removal
  ├─ if store.GetActiveID() == id: [store.ClearActive()]  (AC-04-3)
  ├─ [store.Save()]
  │
  ├─ if len(cascadeErrors) > 0:
  │    print "Profile '<id>' removed with errors:" + each error
  │    exit 1 (NFR-04)
  │
  └─ print "✓ Profile '<id>' removed."
     exit 0 (AC-04-1, AC-04-2 or AC-04-3)
```

### 3.8 `auth logout` — Happy + Edge Cases

```
User: ipa-manager auth logout [profile-id]
  │
  ├─ resolve target:
  │    if len(args) > 0: targetID = args[0]
  │    else: targetID = store.GetActiveID()
  │           if targetID == "" → print ErrNoActiveProfile → exit 1 (AC-05-4)
  │
  ├─ [store.Get(targetID)]
  │    └─ ✗ not found → print ErrProfileNotFound → exit 1 (AC-05-3)
  │
  ├─ [store.HasCredentials(targetID)]
  │    ├─ ✗ already logged-out → exit 0 silently (AC-05-5, idempotent)
  │    └─ ✓ has credentials ↓
  │
  ├─ [appStore := NewProfileAppStore(profile)]
  ├─ [appStore.Revoke()]  ← removes keychain "profiles/<id>/account"
  ├─ [os.Remove(CookieJarPath(id, root))]  ← best-effort, ignore NotExist
  │
  ├─ [do NOT touch store metadata]
  ├─ [do NOT change active]  (AC-05-1)
  │
  └─ print "✓ Logged out: <id> (profile metadata retained)."
     exit 0 (AC-05-1 or AC-05-2)
```

---

## 4. Data Models, State & Interfaces

### 4.1 数据模型（持久化）

**`~/.ipa-manager/config.json`**（DD-03）：

```json
{
  "active_profile_id": "string (profile ID or empty)",
  "profiles": [
    {
      "id": "string (derived from email, unique)",
      "name": "string (from Apple account)",
      "email": "string (Apple ID)",
      "store_front": "string (Apple store code, omitempty)"
    }
  ]
}
```

**keychain**（per profile，由 ipatool 管理）：
- key: `profiles/<id>/account`（ProfileKeychain 映射）
- value: ipatool `Account` JSON（含 Email, PasswordToken, DirectoryServicesID, Name, StoreFront, Password, Pod）

**cookie jar**（per profile）：
- path: `~/.ipa-manager/profiles/<id>/cookies`
- format: `juju/persistent-cookiejar` 二进制

### 4.2 数据模型（进程内）

**`account.Profile`**（现有，无变化）：
```go
type Profile struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Email       string `json:"email"`
    StoreFront  string `json:"store_front,omitempty"`
}
```

**`account.profileFile`**（内部，不导出）：
```go
type profileFile struct {
    ActiveProfileID string    `json:"active_profile_id,omitempty"`
    Profiles        []Profile `json:"profiles"`
}
```

**`account.Store`**（DD-04）：
```go
type Store struct {
    configPath    string
    keyringBackend keyring.Keyring  // shared for HasCredentials probes
    os            operatingsystem.OperatingSystem  // for atomic write
}
```

### 4.3 状态转换

**Profile 凭据状态机**：
```
                    auth login (success)
  (不存在的 profile) ──────────────────► LOGGED_IN
                                            │
                                            │ auth logout
                                            ▼
                                        LOGGED_OUT
                                            │
                                            │ auth login (refresh)
                                            ▼
                                        LOGGED_IN
                                            │
                                            │ accounts remove
                                            ▼
                                       (不存在的 profile)
```

**Active 指针状态机**：
```
  "" ──(首个 auth login 成功)──► "<id>"
  "<id1>" ──(accounts use <id2>)──► "<id2>"
  "<id1>" ──(accounts remove <id1>)──► ""
  "<id1>" ──(auth logout <id1>)──► "<id1>" (不变, AC-05-1)
  "<id1>" ──(auth login <id2>)──► "<id1>" (不变, AC-01-4)
```

### 4.4 接口契约

**`appstore.NewProfileAppStore`**（替换现有 stub）：
```go
// NewProfileAppStore constructs an ipatool AppStore scoped to a single
// account profile with isolated keychain namespace and cookie jar.
func NewProfileAppStore(p account.Profile, configRoot string) (appstore.AppStore, error)
```

**`account.DeriveProfileID`**（新增）：
```go
// DeriveProfileID converts an email to a stable profile ID per §4.1 of requirements.
// Algorithm: lowercase, replace each non-[a-z0-9_-] rune with '_'.
func DeriveProfileID(email string) string
```

**`account.Store`** 方法签名见 DD-04。

**`ui` 提示器**（替换现有 stubs）：
```go
func SelectProfile(profiles []account.Profile, activeID string) (string, error)  // huh Select
func Confirm(title string) (bool, error)                                         // huh Confirm
func InputAuthCode() (string, error)                                             // huh Input (6 digits)
func InputEmail() (string, error)                                                // huh Input
func InputPassword() (string, error)                                             // huh Input (masked)
```

**`apperr` sentinel errors**（新增）：
```go
var (
    ErrProfileNotFound    = errors.New("profile not found")
    ErrProfileNotLoggedIn = errors.New("profile has no credentials")
    ErrNoActiveProfile    = errors.New("no active profile")
)
```

---

## 5. Code Structure（文件映射）

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/account/store.go` | **完全重写**：`Store` 实现 + config.json 读写 + `HasCredentials` + `DeriveProfileID`（后者也可独立文件） |
| `internal/account/derive_id.go` | `DeriveProfileID` 函数 + 表驱动测试（§4.1 算法验证） |
| `internal/account/store_test.go` | Store 单元测试（temp dir 隔离）|
| `internal/appstore/client_impl.go` | `NewProfileAppStore` 真实实现（替换 stub）|
| `internal/appstore/client_test.go` | 工厂接线测试（mock keychain 验证 namespace）|

### 修改文件

| 文件 | 修改 |
|------|------|
| `internal/appstore/client.go` | 移除 stub（迁移到 `client_impl.go`），保留包注释 |
| `internal/cli/auth.go` | `authLoginCmd` 填实（DD-05 流程）；`authLogoutCmd` 增加可选 arg + 填实 |
| `internal/cli/account.go` | 移除 `accountsAddCmd`；`list`/`use`/`remove` 填实 |
| `internal/ui/prompt.go` | 填实 `SelectProfile` / `Confirm` / `InputAuthCode`；新增 `InputEmail` / `InputPassword` |
| `internal/apperr/errors.go` | 新增 `ErrProfileNotFound` / `ErrProfileNotLoggedIn` / `ErrNoActiveProfile` |
| `internal/config/paths.go` | 填实 `Default()`（`os.UserHomeDir()` + `~/.ipa-manager`）|

### 不变文件（含理由）

| 文件 | 理由 |
|------|------|
| `internal/account/profile.go` | `Profile` 类型已定义且满足需求 |
| `internal/account/keychain.go` | `ProfileKeychain` 已实现 + 测试通过（ADR 0002），不动 |
| `internal/account/cookiejar.go` | `CookieJarPath` 已实现；`NewCookieJar` 被 `appstore.NewProfileAppStore` 内部直接用 `cookiejar.New` 替代，该函数标记 deprecated 或删除 |
| `internal/account/keychain_test.go` | 现有隔离测试仍有效 |
| `internal/appstore/errors.go` | `ErrAuthCodeRequired` alias 不变 |
| `internal/ui/table.go` | lipgloss 表格渲染已实现，直接用 |
| `internal/cli/root.go` | 命令树根不变 |
| `internal/cli/{apps,devices,install,doctor}.go` | 非本 mission 范围，不动 |
| `internal/config/config.go` | 弃用（DD-04），但保留文件不删，避免破坏 import（若有）；v1 不调用 `Load`/`Save` |
| `internal/device/*` | 非本 mission 范围 |
| `internal/library/*` | 非本 mission 范围 |
| `internal/doctor/*` | 非本 mission 范围 |

---

## 6. Impact Analysis

### 6.1 兼容性风险

| 维度 | 影响 | 说明 |
|------|------|------|
| **ipatool 升级** | 中 | `NewProfileAppStore` 直接依赖 `appstore.Args` / `appstore.NewAppStore` / `LoginInput` 等签名。ipatool v2.x 内若改这些，编译断（我们有编译期 `appstore.AppStore` 引用）。 |
| **keyring 后端** | 低 | `99designs/keyring` 是稳定成熟库。ServiceName 改变会丢失旧数据，但 v1 首次发布无迁移问题。 |
| **现有 ProfileKeychain 测试** | 无 | 不修改 `keychain.go`，现有 alice/bob 隔离测试继续有效。 |
| **现有 CLI 命令树** | 低 | 移除 `accounts add` 是 breaking change，但 v1 尚未发布，无外部影响。 |

### 6.2 迁移需求

**N/A**。v1 首次发布，无历史数据需迁移。`config.json` 不存在时视为空状态（AC-03-1）。

### 6.3 安全/隐私

| 项 | 评估 |
|----|------|
| **Password 存储** | ipatool 固有行为：password 进 keychain（受 macOS Keychain 加密）。NFR-05 已修订明示。不额外脱敏（DD-用户已确认 Option A）。 |
| **Password 日志** | 我们自写 CLI 层，**绝不** log password。ipatool verbose 模式会 log，但我们不复刻。 |
| **Profile 元数据** | email/name 明文存 config.json。可接受（本机用户已可见）。 |
| **Keychain 访问** | 首次 keyring.Open 会触发 macOS Keychain 授权对话框（用户输密码或点 Always Allow）。正常 macOS 行为。 |

### 6.4 性能/可靠性

| 项 | 评估 |
|----|------|
| **`accounts list` 性能** | 每 profile 一次 keychain.Get（本地操作）。≤10 profiles < 500ms（NFR-01）。 |
| **config.json 写** | 原子写（DD-10），无部分写风险。 |
| **网络故障** | `Bag()` / `Login()` 失败时，命令以可读错误退出，不修改任何本地状态（keychain 在 ipatool Login 成功后才写，config 在最后才 Save）。 |
| **Keychain 故障** | `HasCredentials` 出错时视为 logged-out（保守）。Revoke 失败时级联报告（NFR-04）。 |

### 6.5 可观测性

- `auth login` 输出阶段进度（NFR-09）：`"Contacting Apple..."` → `"2FA code required"` → `"Login successful"`。不输出 password / token。
- 其他命令输出简洁的成功/失败行。
- 无结构化日志（v1 不需要；个人工具）。

### 6.6 Rollout/Rollback

**N/A**。单二进制，无服务端。用户通过替换二进制升级/回退。config.json schema 向前兼容（新增字段 omitempty）。

---

## 7. Validation Strategy

→ 详见 `e2e_test.md`。本节仅概述策略：

- **E2E 测试**：以 `requirements.md` 的 31 个 AC 为源头，每个 AC 至少 1 个 E2E 用例。用 Apple API mock（拦截 `Bag` / `Login` 返回）模拟 happy / 2FA / failure 路径，避免真实 Apple 账号。
- **单元测试**：`account.DeriveProfileID`（表驱动）、`account.Store`（temp dir + CRUD + active 状态机）、`appstore.NewProfileAppStore`（mock keychain 验证 namespace 隔离）。
- **集成边界**：真实 Apple API 调用不在自动化测试覆盖范围（需真实账号 + 2FA，无法 CI 化）。手动验收。
- **不可测项**：macOS Keychain 解锁对话框、ipatool 的真实 Apple 通信——这些是外部依赖，本 mission 测试到"我们正确调用了 ipatool API + 正确处理了返回值"为止。

---

## 8. Design Completeness Self-Check

implementation 阶段能否不猜谜地推进？

- [x] **每个 stub 的真实 API 已验证**：`appstore.NewAppStore` / `Login` / `Revoke` / `Bag` / keychain / cookiejar——全部从 ipatool v2.3.0 源码读到，非猜测。
- [x] **数据格式确定**：config.json schema（DD-03）、keychain key 映射（已有）、cookie jar 路径（已有）。
- [x] **每个 AC 有对应处理流**：§3 的 8 个流程图覆盖 31 个 AC 的所有 Then 子句。
- [x] **错误路径有定义**：DD-09 错误映射表 + 每个流程图的 ✗ 分支。
- [x] **状态转换完整**：§4.3 两个状态机覆盖所有 profile/active 状态变化。
- [x] **文件修改清单完整**：§5 列出每个新增/修改/不变文件及其精确职责。
- [x] **无 "TODO: figure out"**：所有设计点都有决策 + 理由 + 备选。

→ 可进入 plan 阶段做任务分解。
