# Requirements — fix-purchase-token-expired

## 1. Intent & Context

### Problem

`device install <bundle-id>` 在 Apple session token 过期时**直接报错给用户**,而不是像 Download 路径那样自动 refresh + 重试。

实测错误:

```
$ ./bin/ipa-manager device install com.hikvision.videogo
Error: license acquisition failed: failed to purchase item with param 'STDQ': password token is expired
```

### Root Cause(已诊断)

`internal/appstore/client_impl.go:145-153` 的 `Purchase` 方法直接返回 ipatool 的原始错误,**未经过 sentinel 转换**:

```go
func (a *profileAppStoreAdapter) Purchase(...) error {
    ...
    return a.inner.Purchase(...)  // ← ipatool 的 ErrPasswordTokenExpired,不是 apperr 的
}
```

对比 `Download` 方法的处理(经 `mapDownloadError` 把 ipatool sentinel 转成 apperr sentinel)。

CLI 层 `internal/cli/app_download.go:245` 检查 `errors.Is(err, apperr.ErrPasswordTokenExpired)`,**因为传入的是 ipatool sentinel 而非 apperr sentinel,匹配失败**,走 else 分支把错误冒泡给用户。

代码注释 `app_download.go:244` 甚至明确写了风险,但只写了注释没在 adapter 层兜住:

```go
// Purchase may also fail with token-expired (data-flow audit finding)
if errors.Is(err, apperr.ErrPasswordTokenExpired) { ... }  // ← 永远不进这个分支
```

### Desired Outcome

`device install`(以及任何走 Purchase 路径的命令)在 token 过期时**自动 RefreshSession + 重试**,与 Download 路径行为一致。用户无感知,无需手动 `auth login`。

### Workaround(用户当前可用)

```bash
./bin/ipa-manager auth login  # 手动刷新 session
./bin/ipa-manager device install <bundle-id>
```

本 mission 把 workaround 变成自动行为。

## 2. Actors / Assumptions / Dependencies

### Actors

| Actor           | Description                                          |
| --------------- | ---------------------------------------------------- |
| ipa-manager user | 运行 `device install` / `app download`,期望 token 过期时无感重试 |

### Assumptions

- **A-01**:ipatool 的 `Purchase` 失败时,若原因是 token 过期,返回的 sentinel 是 `ipaappstore.ErrPasswordTokenExpired`(与 Download 同一个 sentinel)。**已从 module cache 源码确认**(同一个包级 var)。
- **A-02**:`RefreshSession()`(adapter 已实现,`client_impl.go:158+`)能成功用 keychain 缓存的 password 重换 token —— 已在用户的实测中验证(`auth login` workaround 跑通即证明)。
- **A-03**:Apple 的 password token 过期是**常见且预期**的情况(几小时到几天);session token 自动刷新是产品基本要求,不是 nice-to-have。
- **A-04**:本修复**不动 ipatool fork** —— bug 在 ipa-manager 自己的 adapter 层。
- **A-05**:除 Purchase 外,其他 adapter 方法(ReplicateSinf / Search / Lookup / AccountInfo)理论上也可能返回 ipatool sentinel,但**本 mission 只修 Purchase**(YAGNI;其他方法在实测中未触发该问题,出现时再单开 mission)。

### Dependencies

- **D-01**:mission `download-ipa-by-account` 交付的 `ProfileAppStore` adapter 与 `mapDownloadError` 模式。
- **D-02**:mission `ios-device-manage` 交付的 `device install` 命令(走 `downloadToLibrary` → 复用 `app_download.go` 的 error-recovery)。

## 3. Scope

### In Scope

- 在 `internal/appstore/client_impl.go` 的 `Purchase` 方法中,把 ipatool 的 `ErrPasswordTokenExpired` 转换为 `apperr.ErrPasswordTokenExpired`。
- 复用或新增一个 sentinel 转换 helper(决策见 design.md)。
- 补单测覆盖 Purchase 失败 → RefreshSession → 重试成功 的完整路径。

### Out of Scope

- 修改 `RefreshSession` 本身(它在 `auth login` 路径已实证可用)。
- 修改其他 adapter 方法的错误转换(见 A-05;YAGNI)。
- 修改 ipatool fork(fork 的 Purchase 行为正确,只是返回自己的 sentinel)。
- 引入"preemptive token refresh"(登录时主动刷新 token 避免过期)——这是另一个独立改进,本 mission 不做。

### Non-goals

- 把 ipatool 的所有 sentinel(几十个)在 adapter 全部转换 —— 只转 Purchase 路径需要的那个。
- 重构 CLI 层的 `handleLicenseRequired` / `handleTokenExpired` —— 它们设计正确,只是输入的 sentinel 不对。

## 4. User Stories

| ID    | Priority | Story                                                                                                                | Rationale                                                                                       |
| ----- | -------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| US-01 | P1       | As an ipa-manager user, I want `device install` to auto-refresh my expired Apple session and retry, so that I don't have to manually `auth login` every time my session expires. | 直接阻塞核心使用流程;token 过期是高频常态。                                                       |
| US-02 | P2       | As a maintainer, I want the sentinel-conversion logic shared between Download and Purchase paths, so that future ipatool sentinel additions propagate to both paths automatically. | 防止同类 bug 再次发生(DRY)。                                                                      |

### Priority Rationale

- US-01 是 P1:用户实测遇到的阻塞,fix 后用户体验显著改善。
- US-02 是 P2:是工程优化(DRY),不影响功能正确性。如果与 US-01 实现路径冲突,优先 US-01 的最小修复。

## 5. Acceptance Criteria

### US-01 — Purchase 失败时自动 refresh + 重试

- **AC-01-1**: Given 当前 profile 的 Apple password token 已过期,When 用户运行 `ipa-manager device install <bundle-id>`(或任何触发 Purchase 的命令),Then CLI **自动**调用 `RefreshSession()` 刷新 token、重试 Purchase、继续后续流程,**用户看到的最终结果是 install 成功**(无错误输出到 stderr)。
- **AC-01-2**: Given AC-01-1 中 RefreshSession 也失败(例如 keychain 中缓存的密码已被用户在 Apple ID 后台修改),Then CLI 输出清晰的错误 `re-login failed: <原因>`,**不输出**含"STDQ"或"password token is expired"的底层 Apple 错误信息(那些是技术细节,不应暴露)。
- **AC-01-3**: Given Purchase 失败但**原因不是 token 过期**(例如付费 app、网络故障),Then 错误处理路径与现状一致(返回 `license acquisition failed: <原因>`),**不触发** RefreshSession。
- **AC-01-4**: Given 单元测试 `TestPurchase_TokenExpired_AutoRefresh` 运行,When mock 的第一次 Purchase 返回 `ipaappstore.ErrPasswordTokenExpired`、RefreshSession 成功、第二次 Purchase 成功,Then 测试断言 RefreshSession 被调用一次、Purchase 被调用两次、最终返回 nil。

### US-02 — 错误转换 DRY

- **AC-02-1**: Given 代码审查 `internal/appstore/errors.go` 或等价位置,Then Download 和 Purchase 路径共用同一个错误转换函数(而非各自维护一份 if-else 列表)。
- **AC-02-2**: Given 未来 ipatool 新增一个 sentinel(假设叫 `ErrFoo`),When 维护者在共享转换函数中加一行 `if errors.Is(err, ipaappstore.ErrFoo) { return apperr.ErrFoo }`,Then Download 和 Purchase 路径**都**自动获得这个转换,无需修改两处。

## 6. Non-Functional Requirements

| ID     | Category        | Requirement                                                                  | Measurement                                                |
| ------ | --------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------- |
| NFR-01 | No regression   | 现有 201 个自动化测试全部通过(无新增、无修改 except 新增的 Purchase 路径测试)。 | `go test ./... -count=1` 退出码 0,测试数 ≥ 201。            |
| NFR-02 | No fork change  | 本 mission 不修改 ipatool fork(`yeuleh/ipatool/v2@v2.3.1-fix-auth.5`)。       | `git diff go.mod go.sum` 无变更。                            |
| NFR-03 | No secret leak  | 新增代码与测试不包含真实 Apple ID / password / token 字面值。                    | 代码审查 + `rg 'ghp_|appleid|password:\s*"[^"]{8,}"'`。       |
| NFR-04 | Boundary        | 修复仅影响 Purchase 失败路径;Download / RefreshSession / Login 路径行为不变。    | 现有相关测试(`TestDownload_*`、`TestAuthLogin_*`)全绿。     |
| NFR-05 | Observability   | 修复后用户可见的错误信息更友好(无"STDQ"等 Apple 内部术语)。                       | 代码审查 `app_download.go:250` 的 fmt.Errorf 格式。          |

## 7. Key Domain Concepts

| Concept                       | Description                                                                                                                       |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| ipatool sentinel error        | ipatool 的 `pkg/appstore` 包定义的哨兵错误(如 `ErrPasswordTokenExpired`、`ErrLicenseRequired`),用于标识特定失败类型。              |
| apperr sentinel error         | ipa-manager 自己的 `internal/apperr` 包定义的对应哨兵。**adapter 层负责把 ipatool sentinel 转成 apperr sentinel**,CLI 层只见 apperr。 |
| `mapDownloadError` 模式        | 现有的错误转换 helper(`internal/appstore/errors.go:16`),Download 方法用它做 sentinel 转换。本 mission 决定是否复用 / 泛化(见 design.md DD-01)。 |
| `RefreshSession()`            | adapter 方法,用 keychain 缓存的 password 重新换 token。已实现且实证可用。                                                              |
| `handleLicenseRequired` 流程  | CLI 层(`app_download.go:215+`)的免费 license 获取 + token 过期重试流程。本 mission 让它对 Purchase 失败也生效。                              |

## 8. Success Criteria

1. **实测**:`./bin/ipa-manager device install <bundle-id>` 在 token 自然过期后(等待 N 小时或手动 revoke session 触发)运行,**无需任何手动干预**就 install 成功。
2. **自动化**:新增单测 `TestPurchase_TokenExpired_AutoRefresh`(或等价名)覆盖该路径,且所有现有测试无回归。
3. **代码质量**:错误转换在 Download 和 Purchase 路径共享(US-02)。
4. **可发布**:CHANGELOG 或 commit message 清楚说明 fix 内容(个人项目,无需正式 release notes)。

## 9. Clarification Notes

- **为什么不扩展到所有 adapter 方法**:YAGNI。当前实测只有 Purchase 路径有这个问题。其他方法(ReplicateSinf / Search / Lookup / AccountInfo)若以后出现类似问题,各自单开 mission(每次 1-2 行修复 + 1 个测试,成本低)。预先全转是 over-engineering。
- **为什么不预先 refresh token**:在 `device install` 命令开头主动调 `RefreshSession()` 可以"预防"token 过期,但代价是每次 install 都多一次 Apple API 调用(慢、增加风控风险)。**Reactive refresh**(出错才 refresh + retry)是更优策略,本 mission 走这条路。
- **为什么不在 CLI 层用 `strings.Contains(err.Error(), "password token is expired")` 兜底**:虽然能修 bug,但破坏了 sentinel error 的设计模式,让代码脆弱(任何 ipatool 改 error message 都会破)。**正确的修复在 adapter 层**,与 Download 路径保持一致。
