# Design — fix-purchase-token-expired

## 1. Goals & Non-Goals

### Goals

满足 requirements.md 的 **US-01**:

- **AC-01-1**: Purchase 失败(token expired)→ 自动 `RefreshSession()` + 重试 → install 成功,用户无错误可见
- **AC-01-2**: refresh 也失败 → 友好错误(`re-login failed:`,不暴露 `STDQ`/`password token is expired`)
- **AC-01-3**: 非 token 错误 → 行为不变(`license acquisition failed:`)

### Non-Goals(design constraints)

- 不修改 CLI 层 `handleLicenseRequired` 代码(`app_download.go:215-258`)—— 它的设计正确,只是输入的 sentinel 不对
- 不修改 ipatool fork(fork 的 Purchase 行为正确,返回自己的 sentinel 是合理的)
- 不动其他 adapter 方法(Lookup/ReplicateSinf 等的同类潜在问题,见 requirements §3 Out of Scope)
- 不引入 preemptive token refresh(见 requirements §9)

## 2. Architecture & Key Decisions

### Component Overview(改动范围)

```
┌────────────────────────────────────────────────────────────┐
│  CLI 层 internal/cli/app_download.go (UNCHANGED)            │
│  └─ handleLicenseRequired                                    │
│      └─ if errors.Is(err, apperr.ErrPasswordTokenExpired) { │
│             appStore.RefreshSession()                       │
│             appStore.Purchase(...)  ← retry                 │
│         }                                                    │
└──────────────────────┬──────────────────────────────────────┘
                       │ ProfileAppStore interface
                       ▼
┌────────────────────────────────────────────────────────────┐
│  adapter 层 internal/appstore/  (CHANGED,本 mission 范围)    │
│  ├─ errors.go  (CHANGED)                                     │
│  │   └─ mapAppStoreError(err)  ← 重命名自 mapDownloadError  │
│  └─ client_impl.go  (CHANGED)                                │
│      ├─ Purchase(...) error                                  │
│      │   └─ return mapAppStoreError(a.inner.Purchase(...))  │ ← FIX
│      └─ Download(...)                                        │
│          └─ return ..., mapAppStoreError(err)  (调用名变)    │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
              ipatool pkg/appstore
              (UNCHANGED — fork 行为正确)
```

**唯一行为变化**:Purchase 失败时,返回的 error 从 ipatool sentinel 变成对应的 apperr sentinel(仅 `ErrPasswordTokenExpired` 一个);其他 sentinel 原样透传(向后兼容)。

### Decision Record

#### DD-01: 错误转换策略 — 重命名 `mapDownloadError` → `mapAppStoreError`,Download/Purchase 共用

**Decision**: 把 `internal/appstore/errors.go:16` 的 `mapDownloadError` 重命名为 `mapAppStoreError`(更通用名),Download 和 Purchase 路径**共用**这一个 helper。

**Rationale**:
- 满足 **NFR-06 Maintainability**(requirements.md):避免两份独立 if-else 列表,防止同类 bug 再次发生。
- `mapDownloadError` 当前转换 `ErrLicenseRequired` + `ErrPasswordTokenExpired` 两个 sentinel。Purchase 路径只会返回 `ErrPasswordTokenExpired`(LicenseRequired 是 Download 特有的);**超集转换对 Purchase 无副作用**(Purchase 不会返回 LicenseRequired,即使返回,转成 apperr.ErrLicenseRequired 也合理且向后兼容)。
- 重命名是机械 refactor,零行为风险。

**Implementation**:
```go
// errors.go — 重命名 + 注释更新
// mapAppStoreError translates ipatool sentinel errors to our apperr sentinels.
// Used by the adapter's Download AND Purchase methods (NFR-06: shared conversion).
func mapAppStoreError(err error) error {
    if errors.Is(err, ipaappstore.ErrLicenseRequired) {
        return apperr.ErrLicenseRequired
    }
    if errors.Is(err, ipaappstore.ErrPasswordTokenExpired) {
        return apperr.ErrPasswordTokenExpired
    }
    return err
}
```

**Alternatives considered**:
- **(a) Inline 在 Purchase 方法内加 `if errors.Is(...)`**:违反 DRY,新增 sentinel 要改两处。被 NFR-06 拒绝。
- **(b) 新建 `mapPurchaseError` helper**:同样违反 DRY(两个 helper 内容会重复)。
- **(c) 完全泛化(mapAllErrors 处理所有 ipatool sentinel)**:over-engineering,YAGNI(见 requirements §3 Out of Scope)。

#### DD-02: 测试策略 — 双层覆盖

**Decision**: 加两个测试:

1. **adapter 层单元测试**(必加)— `internal/appstore/client_test.go` 新增 `TestPurchase_TokenExpired_ConvertsToApperrSentinel`:
   - **目的**: 验证 Purchase 失败时正确转换 sentinel(fix 的直接契约)。
   - **方法**: 构造 `profileAppStoreAdapter` 实例,注入 mock `ipaappstore.AppStore` interface,让 mock 的 `Purchase` 返回 `ipaappstore.ErrPasswordTokenExpired`。
   - **断言**: `errors.Is(returnedErr, apperr.ErrPasswordTokenExpired)` 为 true。

2. **CLI 层端到端测试**(加)— `internal/cli/app_download_test.go`(或 `app_download_edge_test.go`)新增 `TestHandleLicenseRequired_PurchaseTokenExpired_Retries`:
   - **目的**: 验证 AC-01-1 的完整路径(Purchase 失败 → refresh → retry → 成功)在 CLI 编排层正确串联。
   - **方法**: mock `ProfileAppStore`,让第一次 `Purchase` 返回 `apperr.ErrPasswordTokenExpired`、`RefreshSession` 返回 nil、第二次 `Purchase` 返回 nil;后续 `Download` 成功。
   - **断言**: `RefreshSession` 被调用 1 次、`Purchase` 被调用 2 次、命令 exit 0、`library.Add` 被调用(IPA 进入库)。

**为什么 CLI 层测试也要加**:虽然既有测试覆盖了 Download 路径的 token-expired(`app_download_test.go:115 TestDownload_TokenExpired_AutoRelogin`),但 **Purchase 路径** 的 token-expired 重试(`handleLicenseRequired` 内的 `if errors.Is(err, apperr.ErrPasswordTokenExpired)` 分支)目前**无专门测试**。补一个测试锁定该路径的契约。

**Alternatives considered**:
- **只加 adapter 层测试**:不够,因为 CLI 层的 `handleLicenseRequired` token-expired 分支(`app_download.go:244-254`)虽然有代码,但从未被任何测试执行过(代码审查发现)。补测试锁定。
- **加 stderr 字符串断言测试**(NFR-05):**改为依赖 adapter 测试 + CLI 测试中的 error wrapping 链**:`re-login failed: <reason>` 的格式由 `app_download.go:247` 的 `fmt.Errorf("re-login failed: %w", err)` 决定。NFR-05 的"不暴露 STDQ / password token is expired"在 AC-01-2 路径自动满足 —— 因为 token-expired 已被转换成 apperr sentinel,`%w` 包装的是 RefreshSession 的错误(不会包含 Apple 的内部术语)。**无需额外 stderr 字符串断言测试**,现有 wrapping 逻辑足够保证。

## 3. Data Models / State / Interfaces

**N/A — 无变化**。

- `ProfileAppStore` interface 不变(`Purchase(bundleID, appID, price) error` 签名不变)
- `apperr.ErrPasswordTokenExpired` sentinel 不变
- `ipaappstore.ErrPasswordTokenExpired` 不变(fork)
- 仅 Purchase 方法的**返回错误类型**变化(从 ipatool sentinel 变为 apperr sentinel)

## 4. Code Structure

### Files Modified

| File                              | Change                                                                                              |
| --------------------------------- | --------------------------------------------------------------------------------------------------- |
| `internal/appstore/errors.go`     | `mapDownloadError` → `mapAppStoreError`(重命名 + 注释更新)。**纯 rename,转换逻辑不变**。              |
| `internal/appstore/client_impl.go` | 2 处:(a) `Download` 方法的 `mapDownloadError(err)` 调用改为 `mapAppStoreError(err)`;(b) `Purchase` 方法把 `return a.inner.Purchase(...)` 改为 `return mapAppStoreError(<same-call>)`。 |

### Files Added(测试)

| File                                       | Change                                                                              |
| ------------------------------------------ | ----------------------------------------------------------------------------------- |
| `internal/appstore/client_test.go`         | 新增 `TestPurchase_TokenExpired_ConvertsToApperrSentinel`(及可能的 mock helper 扩展) |
| `internal/cli/app_download_edge_test.go`(或 `app_download_test.go`) | 新增 `TestHandleLicenseRequired_PurchaseTokenExpired_Retries`                        |

### Files NOT Modified(重要)

| File / Package                      | Reason                                                                        |
| ----------------------------------- | ----------------------------------------------------------------------------- |
| `internal/cli/app_download.go`      | `handleLicenseRequired` 设计正确,只是输入 sentinel 不对;fix 在 adapter 层     |
| `internal/apperr/errors.go`         | sentinel 定义不变                                                              |
| `go.mod` / `go.sum`                 | NFR-02:不动 fork                                                              |
| `internal/appstore/adapter.go`      | interface 定义不变                                                            |
| `internal/account/*`                | 账号管理无关                                                                  |

## 5. Processing Flows

### Happy Path(AC-01-1):token 过期 + refresh 成功 + 重试成功

```
┌─ device install <bundle-id> ────────────────────────────────────────┐
│                                                                       │
│  1. resolveProfile → AppStore                                         │
│  2. AccountInfo(缓存 account)                                          │
│  3. Lookup(bundle-id) → AppInfo                                       │
│  4. library skip-check(无该 app)                                       │
│  5. Download → ErrLicenseRequired(首次下载)                            │
│  6. handleLicenseRequired:                                            │
│     a. price=0 ✅                                                     │
│     b. interactive ✅                                                 │
│     c. UI.Confirm("acquire?") → 用户确认                              │
│     d. appStore.Purchase(bundleID, appID, 0)  ─────────┐              │
│        │ ipatool Apple API 调用,token 已过期            │              │
│        ▼ ipatool 返回 ErrPasswordTokenExpired          │              │
│     e. ✅ FIX: mapAppStoreError(err)                   │              │
│        → apperr.ErrPasswordTokenExpired                │              │
│     f. ✅ errors.Is(apperr.ErrPasswordTokenExpired)    │              │
│        → enter token-expired branch                    │              │
│     g. RefreshSession()                                │              │
│        │ keychain 缓存的 password + Bag.AuthEndpoint   │              │
│        ▼ Login → fresh passwordToken                   │              │
│        → account cache updated                         │              │
│     h. Purchase 第二次(用 fresh token)→ 成功           │              │
│  7. retry Download → 成功                                              │
│  8. ReplicateSinf(DRM 密钥)                                           │
│  9. library.Add(IPA 元数据)                                           │
│ 10. install 到设备                                                     │
│ 11. exit 0 ✅                                                          │
│                                                                       │
│  用户可见:"session expired, re-authenticating..." + install 进度      │
│           (无 "STDQ" / "password token is expired")                   │
└───────────────────────────────────────────────────────────────────────┘
```

**Fix 关键点**:步骤 6e 是本 mission 的核心 —— 没有 mapAppStoreError 转换,6f 的 `errors.Is` 匹配失败,流程会跳到 else 分支(`app_download.go:253`)直接返回 "license acquisition failed"。

### Failure Path 1(AC-01-2):token 过期 + refresh 也失败

```
6.d. Purchase → ErrPasswordTokenExpired → mapAppStoreError → apperr sentinel
6.f. enter token-expired branch
6.g. RefreshSession() → error(如 password 已变)
     ↓
6.g.path2: return fmt.Errorf("re-login failed: %w", err)
     ↓
CLI root.go RunE: 输出 "Error: re-login failed: <RefreshSession 错误描述>"
     ↓
exit 1 ✅

用户可见 stderr: "Error: re-login failed: ..." (不含 STDQ / password token is expired)
```

### Failure Path 2(AC-01-3):Purchase 失败但不是 token expired

```
6.d. Purchase → 其他错误(如 ErrPaidAppNotSupported / 网络错误 / Apple 500)
     ↓
6.e. mapAppStoreError(err) → 不匹配 ErrLicenseRequired / ErrPasswordTokenExpired → 原样返回
     ↓
6.f. errors.Is(apperr.ErrPasswordTokenExpired) → false
     ↓
else branch (app_download.go:253):
     return fmt.Errorf("license acquisition failed: %w", err)
     ↓
exit 1 ✅

用户可见 stderr: "Error: license acquisition failed: <原始错误>" (与 fix 前一致)
```

## 6. Impact Analysis

引用 requirements.md §6 NFR + 补充:

| Concern             | Impact Assessment                                                                                                          |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| **Compatibility**   | LOW RISK。Purchase 方法签名不变,只改返回错误的 sentinel 类型。CLI 层已 expected apperr sentinel,fix 后才匹配预期。 |
| **Migration**       | N/A。无 persisted state 变化。                                                                                              |
| **Security**        | NONE。错误处理路径变化,无凭据处理变化。                                                                                     |
| **Performance**     | NONE。每次 Purchase 失败多一次 errors.Is 调用(纳秒级,可忽略)。                                                            |
| **Reliability**     | IMPROVED。token 过期不再阻塞用户,自动 refresh + retry。                                                                      |
| **Observability**   | IMPROVED。错误信息更友好(AC-01-2 不暴露 STDQ)。                                                                            |
| **Rollout**         | N/A。个人工具,无 phased rollout。                                                                                            |
| **Rollback**        | TRIVIAL。还原 errors.go 和 client_impl.go 即可。                                                                             |
| **Maintainability** | IMPROVED (NFR-06)。Download/Purchase 共享 sentinel 转换 helper,新增 sentinel 只改一处。                                      |
| **Test coverage**   | IMPROVED。新增 2 个测试(adapter 层 + CLI 层),覆盖此前未测试的 handleLicenseRequired token-expired branch。                 |

## 7. Validation Strategy

见 `e2e_test.md` 完整测试矩阵。

### Test Pyramid

| Level      | Scope                                                                                | Count |
| ---------- | ------------------------------------------------------------------------------------ | ----- |
| **Unit**   | adapter Purchase 错误转换契约(`client_test.go` 新增)                                  | 1     |
| **E2E**    | handleLicenseRequired 中 Purchase token-expired 路径(`app_download_*test.go` 新增) | 1     |
| **Regression** | 现有所有测试无回归(`go test ./... -count=1` 全绿)                                 | 201+  |
| **Manual** | 真实 token 过期后 `device install`(validate 阶段手动)                                | 1     |

### Key Validation Principles

1. **Adapter 测试是 fix 的直接契约**:验证 sentinel 类型转换正确。
2. **CLI 测试是端到端契约**:验证 CLI 编排层的 retry 逻辑按预期触发。
3. **无 mock 需求变化**:CLI 层现有 mock 已支持 `purchaseErr` 字段,无需扩展 mock 接口。

## 8. Risk Register

| Risk ID | Risk                                                 | Likelihood | Impact | Mitigation                                                            |
| ------- | ---------------------------------------------------- | ---------- | ------ | --------------------------------------------------------------------- |
| R1      | ipatool Purchase 实际返回非 sentinel 的 wrapped error | LOW        | MEDIUM | 实证:用户实测错误信息含 "password token is expired",与 sentinel.Error() 一致。Adapter 测试用 sentinel 直接验证。 |
| R2      | 重命名 `mapDownloadError` 漏改某处调用                  | LOW        | LOW    | Go compiler 强制:rename 后未更新的调用处会编译失败。`go build ./...` 即可发现。 |
| R3      | mock ipaappstore.AppStore interface 过于复杂           | LOW        | LOW    | interface 是 ipatool 公共 API,方法签名稳定;mock 只需实现 Purchase + AccountInfo 两个方法。 |

## 9. Open Questions

None。所有未知项在 requirements.md §9 已澄清,bug 已实测确认,fix 路径明确。
