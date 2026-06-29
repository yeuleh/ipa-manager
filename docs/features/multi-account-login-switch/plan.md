# Plan — multi-account-login-switch

> 从 `requirements.md`（31 AC）+ `design.md`（12 DD + 8 flows）+ `e2e_test.md`（36 cases）单向派生。每个任务是 vertical slice（carries its own test cycle + independently reviewable）。

---

## 1. Implementation Context

| 维度 | 内容 |
|------|------|
| **Runtime/Language** | Go ≥ 1.26（go.mod locked at `go 1.26.4`）|
| **Key dependencies** | ipatool v2.3.0, go-ios v1.2.0, cobra v1.10.2, huh v2.0.3, lipgloss v2.0.4, testify v1.11.1, juju/persistent-cookiejar（transitive）, 99designs/keyring v1.2.1（transitive）|
| **Testing framework** | Go `testing` + testify（assertions/require）；mock 通过接口注入（DD-12 Deps）|
| **Typecheck** | `go build ./...` + `go vet ./...`（无 npm；Go 项目）|
| **Test command** | `go test ./...` |
| **Key constraints** | 不改 ipatool 源码；ADR 0002 ProfileKeychain 不动；仅 macOS；第三方类型不泄漏到 CLI 层 |
| **Important non-goals** | 不做并发互斥（R3）；不做 password 脱敏（NFR-05 已修订）；不实现 download/install/devices/doctor |

---

## 2. Task Split（Vertical Slices）

### T1 (Foundation) — Profile management infrastructure

**Why Foundation**：Store / Deps / apperr / DeriveProfileID / NewProfileAppStore 是所有命令的基础设施，不能拆进任一 command slice 而不引入横向分层。Store 自身有完整的状态机 + 原子写 + CRUD 测试周期。

**Blocks**：T2, T3, T4, T5, T6

**Scope**：
- `internal/config/paths.go` — 填实 `Default()`：`os.UserHomeDir()` + `filepath.Join(home, ".ipa-manager")` + 子路径
- `internal/account/derive_id.go` (新增) — `DeriveProfileID(email) string` 函数
- `internal/account/derive_id_test.go` (新增) — 表驱动测试（E2E-035 的 7 个 case）
- `internal/account/store.go` — **完全重写**：`Store` struct + `profileFile` 内部 struct + `Load/Save/Upsert/Remove/Get/List/GetActiveID/SetActive/ClearActive/HasCredentials` 全部实现；`Save` 走 tmp+rename 原子写（DD-10）；`Remove` 强制 active-clearing 不变量（DD-04）
- `internal/account/store_test.go` (新增) — Store 单元测试：temp dir 隔离；覆盖 Load(absent file)→empty state、Upsert、Remove(含 active-clearing)、SetActive/ClearActive、Save+reload 一致性、atomic write（E2E-036 状态机路径）
- `internal/apperr/errors.go` — 新增 `ErrProfileNotFound` / `ErrProfileNotLoggedIn` / `ErrNoActiveProfile`
- `internal/appstore/client_impl.go` (新增) — `NewProfileAppStore(p account.Profile, configRoot string)` 真实实现（DD-01 接线步骤 1-6）
- `internal/appstore/client.go` — 清理 stub（迁移到 client_impl.go），保留包注释
- `internal/appstore/factory.go` (新增) — `type AppStoreFactory func(p account.Profile) (appstore.AppStore, error)`（DD-12 DI 类型）
- `internal/appstore/client_test.go` (新增) — mock keychain 验证 namespace 隔离（构造两个不同 ProfileID 的 AppStore，验证 keychain key 映射正确）
- `internal/cli/deps.go` (新增) — `Deps` struct + `Store`/`Prompter` 接口 + `newProductionDeps()` 构造函数（DD-12）
- `internal/ui/prompter.go` (新增) — `Prompter` 接口定义（不含实现；实现留给需要它的 T4/T5/T6）
- `internal/cli/root.go` — 修改 `Execute` + `newRootCmd` 签名以接收 `Deps`（DD-12 CLI 构造）

**US/AC traceability**：N/A（Foundation 不直接满足 user story；为 T2-T6 提供基础）。间接关联 AC-01-2（DeriveProfileID）。

**E2E traceability**：E2E-035（DeriveProfileID 表驱动）、E2E-036（Store 状态机）、E2E-032（atomic write）

**Tests**：
- Unit：`derive_id_test.go`（7 case 表驱动）、`store_test.go`（CRUD + active 状态机 + atomic write + reload 一致性）、`client_test.go`（namespace 隔离）
- Contract：编译期 `var _ appstore.AppStore` 引用断言（已有模式）

**Acceptance commands**：
```bash
go build ./...                           # must exit 0
go vet ./...                             # must exit 0
go test ./internal/account/... -v        # DeriveProfileID + Store 全通过
go test ./internal/appstore/... -v       # NewProfileAppStore namespace 测试通过
```

**Completion criteria**：
- Spock task review pass
- `go build ./...` + `go vet ./...` + `go test ./...` 全 exit 0
- Store 状态机所有路径有测试覆盖（Upsert 新/已存在、Remove 含/不含 active、SetActive/ClearActive、Save+reload）

**Rollback note**：T1 不碰 keychain 数据或 config.json（Store.Load 在文件不存在时返回 empty state，不创建文件）。Rollback = revert code，无数据影响。`cli/root.go` 的 Deps 签名变更会影响所有下游 command 注册——但此时 T2-T6 尚未实现，root.go 内仅为 T1 调整骨架。

---

### T2 (Story) — `accounts list`：列出所有 profile 及状态

**Depends on**：T1

**Scope**：
- `internal/cli/account.go` — 填实 `accountsListCmd(deps Deps)` 的 RunE：`store.Load()` → `store.List()` → 遍历 `store.HasCredentials(p.ID)` → `store.GetActiveID()` → 调用 lipgloss table 渲染（已有 `ui/table.go`）
- `internal/cli/account.go` — 从 `accountCmd()` 的 `AddCommand` 移除 `accountsAddCmd()` 引用（DD-11），删除 `accountsAddCmd()` 函数
- `internal/cli/account_test.go` (新增) — CLI 层测试：mock Deps（mock Store 返回预设 profiles + credentials），断言 stdout 含正确 active 标记 + logged-in/logged-out 区分 + ID/email 列

**US/AC**：US-03 / AC-03-1, AC-03-2, AC-03-3

**E2E**：E2E-009（空列表）、E2E-010（多 profile 状态）、E2E-011（ID+email 可见）

**Tests**：
- Integration：`account_test.go`（mock Store，验证 table 输出格式 + 状态标记）
- 空列表路径单独覆盖（E2E-009）

**Acceptance commands**：
```bash
go build ./... && go vet ./... && go test ./internal/cli/... -run TestAccountsList -v
```

**AC-07-3 error paths**（SPK-M1）：N/A — `accounts list` 是纯只读命令，不产生 ipa-manager 自身的命令错误（Store.Load 失败属系统级错误，按 AC-07-3 排除项处理）。

**Completion criteria**：
- Spock task review pass
- `go build` + `go vet` + `go test ./...` 全 exit 0
- `accounts list` 在空 / 多 profile 场景下输出符合 AC-03-1/2/3

**Rollback note**：纯只读命令，不修改任何持久化状态。Rollback = revert code。

---

### T3 (Story) — `accounts use`：切换 active profile

**Depends on**：T1

**Scope**：
- `internal/cli/account.go` — 填实 `accountsUseCmd(deps Deps)` 的 RunE（DD-07 严格校验顺序）：
  1. `store.Get(id)` → 不存在 → `ErrProfileNotFound`
  2. `store.HasCredentials(id)` → 无凭据 → `ErrProfileNotLoggedIn`
  3. `store.SetActive(id)` → `store.Save()`
- `internal/cli/account_test.go` — 扩展：use 成功 / use 不存在 / use logged-out 三路径测试；E2E-008（factory-panic-if-called 验证 use 不构造 AppStore）

**US/AC**：US-02, US-07 / AC-02-1, AC-02-2, AC-02-3, AC-02-4

**E2E**：E2E-005（切换成功）、E2E-006（不存在）、E2E-007（logged-out）、E2E-008（不构造 AppStore）

**Tests**：
- Integration：use 成功 + 两种失败路径
- AppStore 未被调用的验证（factory panic mock）

**Acceptance commands**：
```bash
go build ./... && go vet ./... && go test ./internal/cli/... -run TestAccountsUse -v
```

**AC-07-3 error paths**（SPK-M1）：
- profile 不存在 → stderr 含 "not found" + "Run `accounts list`"（E2E-006）
- profile 无凭据 → stderr 含 "no credentials" + "Run `auth login`"（E2E-007）

**Completion criteria**：
- Spock task review pass
- 全部测试 exit 0
- 错误路径 stderr 含下一步建议（AC-07-3）

**Rollback note**：`accounts use` 修改 `config.json` 的 `active_profile_id` 字段。Rollback = 手动 `accounts use <previous-id>` 或直接编辑 config.json。

---

### T4 (Story) — `auth login`：统一登录入口（add + refresh + 2FA）

**Depends on**：T1

**Scope**：
- `internal/ui/prompt.go` — 填实 `InputEmail()` / `InputPassword()` / `InputAuthCode()` / `Confirm()` / `SelectProfile()`（huh 包装；Prompter 接口实现）
- `internal/ui/prompt_test.go` — **不创建**（SPK-m2 修正）：huh 交互在非终端环境难以自动化测试；UI 层通过 T4 的 CLI 测试中 mock `Prompter` 接口间接验证；huh 真实交互归入手动验收（§1.2 out of scope）
- `internal/cli/auth.go` — 填实 `authLoginCmd(deps Deps)` 的 RunE（DD-05 完整流程）：
  1. `ui.InputEmail()` → `ui.InputPassword()`
  2. `account.DeriveProfileID(email)` → id
  3. `store.Load()` + `store.Get(id)` → exists?（决定后续 active 行为）
  4. `deps.AppStoreFactory(Profile{ID: id, Email: email})` → appStore
  5. `appStore.Bag(BagInput{})` → bag（失败 → 透传网络错误）
  6. `appStore.Login(LoginInput{email, password, AuthCode: "", Endpoint: bag.AuthEndpoint})`
  7. 若 `ErrAuthCodeRequired` → `ui.InputAuthCode()` → 再 Login（带 AuthCode）
  8. 成功 → `store.Upsert(...)` + 若首个 profile → `store.SetActive(id)` → `store.Save()`
  9. 输出 "Logged in: <name> (<email>)"
- `internal/cli/auth.go` — 从 `authCmd()` 移除 `accountsAddCmd` 引用（若 T2 未处理）
- `internal/cli/auth_test.go` (新增) — login 测试：
  - 新 profile + 2FA happy path（mock AppStore 第一次返回 ErrAuthCodeRequired，第二次成功）
  - 新 profile 无 2FA（mock 直接成功）
  - refresh 已存在 profile（mock 成功，验证 profile 数不增、active 不变）
  - 错误密码（mock 返回非 ErrAuthCodeRequired 错误）
  - 错误 2FA（第二次 Login 失败）
  - Ctrl-C 中止（mock UI Input 返回 error）
  - 首个 profile 自动 active

**US/AC**：US-01, US-06, US-07 / AC-01-1, AC-01-2, AC-01-3, AC-01-4, AC-06-1, AC-06-2, AC-06-3, AC-07-1, AC-07-2

**E2E**：E2E-001（首次+2FA+自动 active）、E2E-002（派生 ID）、E2E-003（第二个不顶替）、E2E-004（refresh）、E2E-025（2FA happy）、E2E-026（2FA 错误）、E2E-027（无 2FA）、E2E-028（错误密码）、E2E-029（Ctrl-C）

**Tests**：
- Integration：上述 7 个 login 场景（mock AppStore + mock UI + temp dir）
- AC-07-2（Ctrl-C）：mock `ui.InputEmail` 返回 error，验证无副作用
- NFR-02 timing（SPK-M3）：单元测试用 mock AppStore（瞬时返回），测量从 `Login` 返回到 `store.Save()` 完成的 wall clock < 1s
- NFR-09 progress output（SPK-M3）：断言 stdout 含阶段性进度关键词（如 "Contacting Apple" / "2FA" / "Logged in"）；断言 stdout/stderr **不含** password 与 token 值

**Acceptance commands**：
```bash
go build ./... && go vet ./... && go test ./internal/cli/... -run TestAuthLogin -v
```

**AC-07-3 error paths**（SPK-M1）：
- 错误密码 → stderr 含 Apple 原始消息 + ": verify your credentials and retry"（E2E-028）
- 错误 2FA → stderr 含 Apple 原始消息 + ": verify your 2FA code and retry"（E2E-026）
- Bag/网络失败 → stderr 含网络错误描述（ipatool 透传，AC-07-3 排除项范围）
- 注：Ctrl-C 是用户取消（AC-07-2），不要求 hint（AC-07-3 排除项）

**Completion criteria**：
- Spock task review pass
- 全部测试 exit 0
- login 流程的所有分支（happy / 2FA / refresh / 错误密码 / 错误 2FA / Ctrl-C）有测试覆盖
- stdout 不含 password / token 值（NFR-05/09）
- stdout 含阶段性进度关键词（NFR-09）
- NFR-02 timing 测试通过（mock AppStore 场景下 CLI 自身开销 < 1s）

**Rollback note**：`auth login` 成功时修改 keychain（通过 ipatool）+ config.json。Rollback = `auth logout <id>` 或 `accounts remove <id>`（但这些命令在 T5/T6 才实现）。在 T4 完成但 T5/T6 未完成时，手动回滚需直接操作 keychain + 编辑 config.json。建议 T4→T5→T6 连续完成。

---

### T5 (Story) — `auth logout`：登出但保留元数据

**Depends on**：T1（NewProfileAppStore 在 T1 已实现）。**顺序排在 T4 后**仅为工作流连贯性——logout 测试通过 mock Store + mock keychain 直接构造 logged-in 测试状态，技术上也只依赖 T1（SPK-m1 修正）。

**Scope**：
- `internal/cli/auth.go` — 填实 `authLogoutCmd(deps Deps)` 的 RunE（§3.8 flow）：
  1. 解析 target（args[0] 或 store.GetActiveID()）
  2. `store.Get(target)` → 不存在 → ErrProfileNotFound
  3. `store.HasCredentials(target)` → 已 logged-out → exit 0 幂等（AC-05-5）
  4. `deps.AppStoreFactory(profile)` → appStore（失败 → 收集 error）
  5. `appStore.Revoke()`（失败 → 收集 error）
  6. `os.Remove(CookieJarPath(id, root))`（best-effort，ignore NotExist）
  7. 不动 metadata，不动 active
  8. 若有 errors → exit 1 + 报告；否则 exit 0
- `internal/cli/auth_test.go` — 扩展 logout 测试：
  - 默认登出 active（AC-05-1）
  - 显式指定 profile（AC-05-2）
  - logout 不存在的 profile（AC-05-3）
  - logout 无 active（AC-05-4）
  - logout 已 logged-out profile 幂等（AC-05-5）
  - logout 后 metadata 保留（AC-05-6）
  - active 指向 logged-out profile 的契约（AC-05-7 / E2E-034）
  - Revoke 失败的错误报告

**US/AC**：US-05, US-07 / AC-05-1 through AC-05-7

**E2E**：E2E-019-024, E2E-034, E2E-030（AC-07-3 跨命令验证的 logout 子场景：E2E-021 profile 不存在 + E2E-022 无 active，SPK-M2 修正）

**Tests**：
- Integration：7 个 logout 场景（mock AppStore + mock keychain + temp dir）
- AC-07-3 验证：E2E-021（profile 不存在 hint）+ E2E-022（无 active hint）

**Acceptance commands**：
```bash
go build ./... && go vet ./... && go test ./internal/cli/... -run TestAuthLogout -v
```

**AC-07-3 error paths**（SPK-M1）：
- profile 不存在 → stderr 含 "not found" + "Run `accounts list`"（E2E-021）
- 无 active profile → stderr 含 "no active profile" + "Run `accounts use`"（E2E-022）
- Revoke/cookie 失败 → stderr 含失败步骤报告（NFR-04）

**Completion criteria**：
- Spock task review pass
- 全部测试 exit 0
- logout 幂等性验证（已 logged-out → exit 0）
- 错误路径 stderr 含下一步建议

**Rollback note**：`auth logout` 删除 keychain entry + cookie jar 文件。Rollback = `auth login <email>` 刷新凭据（T4 已实现）。metadata 保留，无需恢复。

---

### T6 (Story) — `accounts remove`：全量删除 profile

**Depends on**：T1, T4（需要能 login 创建测试状态）

**Scope**：
- `internal/cli/account.go` — 填实 `accountsRemoveCmd(deps Deps)` 的 RunE（DD-08 + §3.7 flow）：
  1. `store.Get(id)` → 不存在 → ErrProfileNotFound（AC-04-5，快速失败，不弹确认）
  2. `ui.Confirm("Remove profile '<id>'?")` → "no" → exit 0（AC-04-4）
  3. cascade error collection:
     - `deps.AppStoreFactory(profile)` → Revoke
     - `os.Remove(CookieJarPath(id, root))`
     - `store.Remove(id)`（含 active-clearing 不变量）
     - `store.Save()`
  4. 若有 errors → exit 1 + 报告（NFR-04）；否则 exit 0
- `internal/cli/account_test.go` — 扩展 remove 测试：
  - 确认删除 happy path（AC-04-1）
  - 删除非 active 不影响 active（AC-04-2）
  - 删除 active 清空 active（AC-04-3）
  - 拒绝确认不删除（AC-04-4）
  - 删除不存在快速失败 + stderr 含 hint（AC-04-5, E2E-016）
  - 删除后 ID 行为如同从未存在（AC-04-6）
  - 删除后同 email 再 login 走全新流程（AC-04-7，需配合 T4 login 测试）
  - 级联失败报告（E2E-033）
- `README.md` — 更新（SPK-m3 修正）：R3 并发限制文档——明示"同 profile 并发操作行为未定义，不在测试覆盖"；简述本 mission 命令用法（login/list/use/remove/logout）
- 性能测试（E2E-031）：本任务完成后，所有命令已实现，可运行 NFR-01 性能基准

**US/AC**：US-04, US-07 / AC-04-1 through AC-04-7

**E2E**：E2E-012-018, E2E-033, E2E-016（AC-07-3 remove 不存在的 hint，SPK-M2 修正）

**Tests**：
- Integration：8 个 remove 场景（mock AppStore + mock keychain + temp dir）
- AC-07-3 验证：E2E-016（remove 不存在 → stderr 含 "not found" + "Run `accounts list`"）
- 性能：E2E-031（10 profiles wall clock < 500ms，本地命令）

**AC-07-3 error paths**（SPK-M1）：
- profile 不存在 → stderr 含 "not found" + "Run `accounts list`"（E2E-016）
- 级联失败 → stderr 含失败步骤报告（NFR-04，E2E-033）

**Acceptance commands**：
```bash
go build ./... && go vet ./... && go test ./internal/cli/... -run TestAccountsRemove -v
go test ./...     # 全量回归（所有包）
```

**Completion criteria**：
- Spock task review pass
- 全部测试 exit 0
- 级联失败不静默部分成功（NFR-04）
- 全量 `go test ./...` 回归通过（无前期任务破坏）

**Rollback note**：`accounts remove` 全量删除（keychain + cookie jar + metadata）。Rollback = `auth login <email>` 重建（T4）。若删除的是 active profile，active 被清空——需手动 `accounts use <other-id>` 恢复。

---

## 3. Dependency Graph

```
                    ┌──────────────────────────┐
                    │  T1 (Foundation)          │
                    │  Store + Deps + apperr    │
                    │  + DeriveProfileID        │
                    │  + NewProfileAppStore     │
                    └─────────┬────────────────┘
                              │ blocks all
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
        ┌──────────┐   ┌──────────┐   ┌──────────────┐
        │ T2 list  │   │ T3 use   │   │ T4 login     │
        │ (read)   │   │ (read+   │   │ (write+2FA)  │
        │          │   │  write)  │   │              │
        └──────────┘   └──────────┘   └──────┬───────┘
                                              │
                                 ┌────────────┴────────────┐
                                 ▼                         ▼
                           ┌──────────┐             ┌──────────────┐
                           │ T5 logout│             │ T6 remove    │
                           │ (needs   │             │ (needs       │
                           │  logged- │             │  profile to  │
                           │  in      │             │  remove)     │
                           │  state)  │             │              │
                           └──────────┘             └──────────────┘
```

**Dependency rationale**：
- T1 → T2/T3/T4：Store/Deps/apperr/NewProfileAppStore 是所有命令的前提
- T4 → T5：logout 测试需要 logged-in 状态（可用 mock 创建，但语义上依赖 login 概念）
- T4 → T6：remove 测试需要 profile 存在（同上）
- T2/T3 之间无依赖（都只需 Store 读路径 + T1 基础设施）
- **Recommended sequential order**：T1 → T2 → T3 → T4 → T5 → T6
- **Parallelizable tracks**（理论）：T2/T3 可与 T4 并行（不同文件，无冲突）。但单人开发建议顺序执行

---

## 4. Traceability Matrices

### 4.1 US/AC → Task

| US | AC | Task | Notes |
|----|-----|------|-------|
| US-01 | AC-01-1 | T4 | login 创建 profile |
| US-01 | AC-01-2 | T1, T4 | DeriveProfileID 在 T1；login 使用在 T4 |
| US-01 | AC-01-3 | T4 | 首个 profile 自动 active |
| US-01 | AC-01-4 | T4 | 第二个 profile 不顶替 active |
| US-02 | AC-02-1 | T3 | use 切换成功 |
| US-02 | AC-02-2 | T3 | use 不存在拒绝 |
| US-02 | AC-02-3 | T3 | use logged-out 拒绝 |
| US-02 | AC-02-4 | T3 | use 本地操作不构造 AppStore |
| US-03 | AC-03-1 | T2 | list 空列表 |
| US-03 | AC-03-2 | T2 | list 多 profile 状态 |
| US-03 | AC-03-3 | T2 | list 含 ID+email |
| US-04 | AC-04-1 | T6 | remove 确认删除 |
| US-04 | AC-04-2 | T6 | remove 非 active 不影响 active |
| US-04 | AC-04-3 | T6 | remove active 清空 active |
| US-04 | AC-04-4 | T6 | remove 拒绝确认 |
| US-04 | AC-04-5 | T6 | remove 不存在快速失败 |
| US-04 | AC-04-6 | T6 | remove 后 ID 行为如从未存在 |
| US-04 | AC-04-7 | T6 (+T4) | remove 后再 login 走全新流程 |
| US-05 | AC-05-1 | T5 | logout 默认作用于 active |
| US-05 | AC-05-2 | T5 | logout 显式指定 |
| US-05 | AC-05-3 | T5 | logout 不存在报错 |
| US-05 | AC-05-4 | T5 | logout 无 active 报错 |
| US-05 | AC-05-5 | T5 | logout 已 logged-out 幂等 |
| US-05 | AC-05-6 | T5 | logout 保留 metadata |
| US-05 | AC-05-7 | T5 | active→logged-out 契约 |
| US-06 | AC-06-1 | T4 | 2FA 提示与成功 |
| US-06 | AC-06-2 | T4 | 2FA 错误码失败 |
| US-06 | AC-06-3 | T4 | 无 2FA 直接成功 |
| US-07 | AC-07-1 | T4 | 错误密码不创建 profile |
| US-07 | AC-07-2 | T4 | Ctrl-C 中止 |
| US-07 | AC-07-3 | T3,T4,T5,T6 | 每个命令实现自己的错误路径 + 下一步建议（T2 list 标记 N/A——纯只读无 ipa-manager 命令错误）|

**Reverse check（每个 US 有对应 task）**：
- US-01 → T4 ✓
- US-02 → T3 ✓
- US-03 → T2 ✓
- US-04 → T6 ✓
- US-05 → T5 ✓
- US-06 → T4 ✓
- US-07 → T2-T6（分布式）✓

**无遗漏**：全部 31 AC 有 task 覆盖。

### 4.2 E2E → Task

| E2E | Task | E2E | Task | E2E | Task |
|-----|------|-----|------|-----|------|
| E2E-001 | T4 | E2E-013 | T6 | E2E-025 | T4 |
| E2E-002 | T1,T4 | E2E-014 | T6 | E2E-026 | T4 |
| E2E-003 | T4 | E2E-015 | T6 | E2E-027 | T4 |
| E2E-004 | T4 | E2E-016 | T6 | E2E-028 | T4 |
| E2E-005 | T3 | E2E-017 | T6 | E2E-029 | T4 |
| E2E-006 | T3 | E2E-018 | T6,T4 | E2E-030 | T3,T5 |
| E2E-007 | T3 | E2E-019 | T5 | E2E-031 | T6 |
| E2E-008 | T3 | E2E-020 | T5 | E2E-032 | T1 |
| E2E-009 | T2 | E2E-021 | T5 | E2E-033 | T6 |
| E2E-010 | T2 | E2E-022 | T5 | E2E-034 | T5 |
| E2E-011 | T2 | E2E-023 | T5 | E2E-035 | T1 |
| E2E-012 | T6 | E2E-024 | T5 | E2E-036 | T1 |

**无遗漏**：全部 36 个 E2E case 有 task 覆盖。

---

## 5. Risk Section

| Risk ID | Description | Likelihood | Impact | Mitigation |
|---------|-------------|-----------|--------|------------|
| **PR-1** | ipatool `AppStore` 接口与 mock 不兼容（mock 需实现全部 12 个方法）| 中 | 中 | 用 `mockgen` 或手写 mock struct 嵌入 `appstore.AppStore` 接口，只 override `Login`/`Bag`/`Revoke`；其余方法 panic 或 return zero |
| **PR-2** | huh 交互测试困难（huh 可能不支持非交互模式）| 中 | 低 | T4 的 UI 测试通过 mock `Prompter` 接口绕过 huh；huh 本身的集成留给手动验收 |
| **PR-3** | keyring.Open 在测试环境触发真实 Keychain 对话框 | 高 | 高 | T1 的 NewProfileAppStore 测试用 mock keyring.Keyring（不调 keyring.Open）；真实 keyring.Open 只在 production deps 构造时调 |
| **PR-4** | `go test` 覆盖率不足（mock 测试可能漏真实集成问题）| 中 | 中 | T6 完成后做一次手动真实 Apple ID login/logout/remove 验收；自动化测试到"正确调用 ipatool API + 正确处理返回值"为止 |
| **PR-5** | T4 (login) 复杂度高（Bag + Login + 2FA retry + Upsert + SetActive），可能需要拆分 | 低 | 中 | 若 T4 审查发现过大，可拆为 T4a（login without 2FA）+ T4b（2FA retry）。但当前评估为一个 task 可控 |
| **PR-6** | 并发问题（Store.mu 保护内存状态，但文件写仍可能竞争）| 低 | 低 | R3 已声明并发行为未定义；v1 不做互斥；文档明示 |

---

## 6. Pre-Execution Baseline

**验证时间**：2026-06-29（plan.md 编写时）

| Check | Command | Result |
|-------|---------|--------|
| Git clean | `git status --porcelain` | ✓ empty |
| Branch | `git symbolic-ref --short HEAD` | ✓ `feature/multi-account-login-switch` |
| Build | `go build ./...` | ✓ exit 0 |
| Vet | `go vet ./...` | ✓ exit 0 |
| Test | `go test ./...` | ✓ account 包 ProfileKeychain 测试通过（cached） |

**Baseline commit**：`6ff3111`（design milestone after user acceptance）

---

## 7. Decision-Complete Declaration

implementation 阶段能否不猜谜地推进？

- [x] **任务顺序确定**：T1 → T2 → T3 → T4 → T5 → T6（§3 依赖图）
- [x] **每个任务的文件清单确定**：§2 每个 task 列出 create/modify 的精确文件
- [x] **每个任务的测试集确定**：§2 每个 task 列出对应 E2E case + unit test 范围
- [x] **每个任务的验收命令确定**：§2 每个 task 给出可执行的 `go build` + `go test` 命令
- [x] **每个任务的回滚策略确定**：§2 每个 task 有 rollback note
- [x] **无 TBD / TODO / "similar to"**：全部 task 描述具体到文件名和函数名
- [x] **Foundation task 数 < story task 数**：1 Foundation < 5 Story ✓
- [x] **Foundation 声明阻塞关系**：T1 blocks T2-T6 ✓
- [x] **反向覆盖**：每个 US 有对应 task（§4.1 reverse check）✓
- [x] **全部 AC 有 task 覆盖**：31/31（§4.1）✓
- [x] **全部 E2E 有 task 覆盖**：36/36（§4.2）✓

→ Plan 阶段产出 decision-complete，execution 阶段可直接按 T1-T6 顺序实现，无需发明架构/接口/任务顺序/验证策略。

---

## 8. Plan Review History

### Round 1 — GO-WITH-FIXES（0 BLOCKER + 3 MAJOR + 3 MINOR）

| Finding | 修正 |
|---------|------|
| SPK-M1: AC-07-3 分布缺每个 task 的显式错误路径 | T2 标 N/A；T3/T4/T5/T6 各加 "AC-07-3 error paths" 子段，列出具体错误 + hint + E2E |
| SPK-M2: E2E-030 归属过宽（实际只测 use+logout）| §4.2 E2E-030 从 T2-T6 改为 T3,T5；T5/T6 加 E2E-021/022/016 显式引用 |
| SPK-M3: NFR-02/09 未排入 T4 | T4 Tests 加 NFR-02 timing + NFR-09 progress output 断言；completion criteria 同步 |
| SPK-m1: T5→T4 依赖理由不准确 | T5 dependency 改为"仅依赖 T1；T4 后排"为工作流连贯 |
| SPK-m2: prompt_test.go "可选" 违反 no-vague | 明确不创建 prompt_test.go；huh 交互归入手动验收 |
| SPK-m3: R3 README 文档无 task 归属 | T6 scope 加 README.md 更新（R3 并发限制 + 命令用法）|
