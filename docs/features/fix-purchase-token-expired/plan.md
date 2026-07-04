# Plan — fix-purchase-token-expired

> 本 plan 从 `requirements.md`(US-01,3 ACs,6 NFRs)+ `design.md`(DD-01/02)+ `e2e_test.md`(7 cases)派生。**决策完备**:执行阶段不需要发明架构 / 接口 / 任务顺序 / 验证策略。

---

## 1. Implementation Context

- **Runtime/Language**: Go ≥ 1.26(macOS,cgo for Keychain backend)
- **Key dependencies**: `github.com/yeuleh/ipatool/v2@v2.3.1-fix-auth.5`(fork,本 mission **不动**)
- **Testing framework**: Go 标准 `testing` + `github.com/stretchr/testify`(assert/require)
- **Build commands**:
  - `go build ./...` — 编译
  - `go vet ./...` — 静态检查
  - `go test ./... -count=1` — 全量测试
- **Constraints**:
  - adapter 隔离(AGENTS.md):ipatool 类型止步 `internal/appstore/`
  - `internal/cli/app_download.go` 是 **non-goal**(本 mission 不修改)
  - go.mod / go.sum **不动**(NFR-02)
- **Non-goals**(本 mission 明确不做):preemptive token refresh、修改其他 adapter 方法、修改 ipatool fork

---

## 2. Dependency Graph

```
┌─────────────────────────────────────────────────────┐
│  T1: adapter 层 fix + 契约测试                         │
│  (errors.go rename + client_impl.go Purchase route) │
│  + E2E-004/005 adapter unit tests                   │
└─────────────────┬───────────────────────────────────┘
                  │
                  │ T2 不依赖 T1 的代码改动
                  │ (CLI 测试 mock 接口,不触达 adapter 实际转换)
                  │ 但**逻辑顺序**建议 T1 → T2
                  │ (fix 先生效,CLI 测试覆盖端到端路径)
                  ▼
┌─────────────────────────────────────────────────────┐
│  T2: CLI 层 mock 扩展 + Purchase token-expired 测试   │
│  (mockAppStore 字段扩展 + E2E-001/002/003 CLI tests) │
└─────────────────────────────────────────────────────┘
```

**并行性**:T1 和 T2 在技术上可并行(CLI 测试 mock 接口)。但本 mission 推荐串行 T1 → T2,因为:
- T1 是真正的 fix(用户价值);T2 是回归保护(工程价值)。
- T1 完成后可立即用 `auth login + device install` 实测验证(虽然完整 validate 在 validate 阶段)。
- T2 的 mock 扩展若失败,不阻塞 T1 的 fix 价值。

**Foundation tasks**:无。本 mission 无基础设施改动需求(现有 mock 框架支持序列模式,只需扩展字段)。

---

## 3. Tasks(Vertical Slices)

### T1 — adapter 层 fix + 契约测试

**Title**: Purchase 错误转换 fix + adapter 层 sentinel 契约测试

**User Story IDs**: US-01(间接支持 — fix 让 AC-01-1/2/3 可达)
**Acceptance Criteria IDs**: NFR-06(direct), AC-01-1/2/3(间接 — 让 CLI 层的 token-expired 分支可达)
**E2E Case IDs**: E2E-004, E2E-005

**Files to modify**:
- `internal/appstore/errors.go` — rename `mapDownloadError` → `mapAppStoreError`,更新注释
- `internal/appstore/client_impl.go` — 2 处:
  - Download 方法的 `mapDownloadError(err)` → `mapAppStoreError(err)`(当前 `client_impl.go:125` 附近)
  - Purchase 方法(`client_impl.go:145-153`)的 `return a.inner.Purchase(...)` → `return mapAppStoreError(err)`(用临时变量承接)

**Files to add/modify for tests**:
- `internal/appstore/client_test.go` — 新增 2 个测试 case:
  - `TestPurchase_TokenExpired_ConvertsToApperrSentinel`(E2E-004)
  - `TestPurchase_NonSentinelError_Passthrough`(E2E-005)
- 可能需要在 `client_test.go` 新增 mock `ipaappstore.AppStore` interface 的 minimal 实现(只 mock Purchase + AccountInfo 方法,其他 panic / return zero)

**Acceptance command**:
```bash
go build ./... && \
go vet ./... && \
go test ./internal/appstore/ -count=1 -run 'TestPurchase|TestDownload|TestMapAppStoreError' -v
```

**Completion criteria**:
- Spock review pass(implementation matches design DD-01)
- `go build ./...` exit 0
- `go vet ./...` exit 0
- 新增 2 个 adapter 测试全绿
- 现有 `TestDownload_*` 测试全绿(rename 不影响行为)
- `errors.go` 中函数名为 `mapAppStoreError`(非 `mapDownloadError`)
- `client_impl.go` Purchase 方法以 `return mapAppStoreError(...)` 结尾

**Rollback note**:纯代码改动,无 persisted state / config / migration / public interface 变化。`git revert` 即可完整回滚。

---

### T2 — CLI 层 mock 扩展 + Purchase token-expired 端到端测试

**Title**: handleLicenseRequired 中 Purchase token-expired 路径的端到端测试覆盖

**User Story IDs**: US-01
**Acceptance Criteria IDs**: AC-01-1, AC-01-2, AC-01-3, NFR-05
**E2E Case IDs**: E2E-001, E2E-002, E2E-003

**Files to modify**:
- `internal/cli/auth_test.go` — 扩展 `mockAppStore` 结构(`auth_test.go:80-84` 附近):
  ```go
  // 现有字段(保留)
  purchaseErr          error  // 保留(向后兼容现有测试)
  purchaseCalled       bool   // 保留
  refreshSessionErr    error  // 保留
  refreshSessionCalled bool   // 保留
  
  // 新增字段
  purchaseErrors       []error  // 序列:第一次失败、第二次成功(模仿 downloadErrors 模式)
  purchaseCalls        int      // 调用计数
  refreshSessionCalls  int      // 调用计数
  ```
  - Purchase mock 方法逻辑:优先用 `purchaseErrors` 序列(逐次 pop),fallback 到 `purchaseErr`(兼容);每次调用 `purchaseCalls++`
  - RefreshSession mock 方法逻辑:`refreshSessionCalls++`,return `refreshSessionErr`
- **兼容性约束**:现有 6 个引用 `purchaseCalled` / `refreshSessionCalled` 的测试(`app_download_edge_test.go:64,90,133`、`device_test.go:666,691`、`auth_test.go`)必须继续工作 —— `bool` 字段保留(在 mock 方法内同时设 `purchaseCalled = true` 和 `purchaseCalls++`)

**Files to add for tests**:
- `internal/cli/app_download_edge_test.go`(推荐)或 `app_download_test.go` — 新增 3 个测试 case:
  - `TestHandleLicenseRequired_PurchaseTokenExpired_Retries`(E2E-001 happy)
  - `TestHandleLicenseRequired_PurchaseTokenExpired_RefreshFails`(E2E-002 refresh 失败 + stderr 断言)
  - `TestHandleLicenseRequired_PurchaseNonTokenError_NoRefresh`(E2E-003 非 token 错误)
- 复用现有 helper:`helperMakeDownloadDeps` / `helperRunDownloadCmd` / `mockLibraryStore` / `mockPrompter`

**Acceptance command**:
```bash
go build ./... && \
go vet ./... && \
go test ./internal/cli/ -count=1 -run 'TestHandleLicenseRequired_Purchase' -v && \
go test ./internal/cli/ -count=1 -v  # 确保现有测试无回归(包括 device_test.go)
```

**Completion criteria**:
- Spock review pass(测试覆盖 design DD-02 + e2e_test.md E2E-001/002/003)
- `go build ./...` exit 0
- `go vet ./...` exit 0
- 新增 3 个 CLI 测试全绿
- 现有 CLI 测试全绿(尤其 `app_download_edge_test.go` / `device_test.go` / `auth_test.go` 中引用 `purchaseCalled` 的测试)
- mock 扩展字段同时设 `purchaseCalled = true` + `purchaseCalls++`(向后兼容)

**Rollback note**:测试代码 + mock 扩展,无生产代码改动,无 persisted state 变化。`git revert` 即可。

---

## 4. Traceability Matrices

### US/AC → Task

| ID       | Description                                      | T1    | T2    | Notes                                              |
| -------- | ------------------------------------------------ | ----- | ----- | -------------------------------------------------- |
| US-01    | device install token 过期自动 refresh + retry    | ✅(间接) | ✅(直接) | T1 提供 fix,T2 提供端到端验证                       |
| AC-01-1  | happy path:refresh 成功 + retry 成功             | indirect | ✅(E2E-001) | T2 是 direct 验证                                |
| AC-01-2  | refresh 也失败 → 友好错误                        | indirect | ✅(E2E-002) | T2 是 direct 验证(NFR-05 stderr 断言)            |
| AC-01-3  | 非 token 错误 → 行为不变                         | ✅(E2E-005 adapter) | ✅(E2E-003 CLI) | T1 验证 adapter passthrough,T2 验证 CLI 端到端 |
| AC-01-4  | 自动化测试覆盖(元 AC)                          | ✅     | ✅     | 由 T1+T2 测试本身的存在满足                          |
| NFR-01   | 无回归                                           | ✅     | ✅     | 两个 task 都跑 `go test ./...`                     |
| NFR-02   | go.mod/go.sum 不变                               | ✅     | ✅     | 两个 task 都不修改依赖                              |
| NFR-03   | 无 secret leak                                   | ✅     | ✅     | 测试 fixture 用 `test@example.com` 等占位值          |
| NFR-04   | boundary(其他路径不变)                          | ✅     | ✅     | T1 rename 不影响行为;T2 不修改现有测试              |
| NFR-05   | observability(stderr 友好)                      | —     | ✅(E2E-002) | 仅 T2 含 stderr 断言                              |
| NFR-06   | maintainability(share helper)                   | ✅     | —     | T1 实现 share,T2 不涉及                            |

### E2E → Task

| E2E Case | US/AC/NFR                | Task | Layer   |
| -------- | ------------------------ | ---- | ------- |
| E2E-001  | US-01 / AC-01-1 / NFR-04 | T2   | CLI     |
| E2E-002  | US-01 / AC-01-2 / NFR-05 | T2   | CLI     |
| E2E-003  | US-01 / AC-01-3 / NFR-04 | T2   | CLI     |
| E2E-004  | NFR-06                   | T1   | adapter |
| E2E-005  | NFR-06 / AC-01-3         | T1   | adapter |
| E2E-006  | NFR-01 / NFR-04          | T1+T2 | 全项目 |
| E2E-007  | NFR-02                   | T1+T2 | artifact |

### Reverse Coverage Verification

- ✅ **US-01** has corresponding task:**T1 + T2**
- ✅ **AC-01-1** has corresponding task:**T2 (E2E-001)**
- ✅ **AC-01-2** has corresponding task:**T2 (E2E-002)**
- ✅ **AC-01-3** has corresponding task:**T1 (E2E-005) + T2 (E2E-003)**
- ✅ **AC-01-4**(meta):**T1 + T2 测试存在性**
- ✅ All 6 NFRs covered

**No orphan user stories or ACs.**

---

## 5. Risk Section

| Risk ID | Risk                                                                  | Likelihood | Impact | Mitigation                                                                                                |
| ------- | --------------------------------------------------------------------- | ---------- | ------ | --------------------------------------------------------------------------------------------------------- |
| R1      | Rename `mapDownloadError` 漏改 call site                              | LOW        | LOW    | Go compiler 强制:`go build ./...` 会立即失败。当前仅 1 处调用(`client_impl.go:125`)。                       |
| R2      | mock `ipaappstore.AppStore` interface 过于复杂(12 方法)                | LOW        | LOW    | 只需实现 Purchase + AccountInfo + 必要的 lookup 方法;其他方法 panic / return zero(测试不会触发)。              |
| R3      | 扩展 `mockAppStore` 字段破坏现有 6 个测试                               | LOW        | MEDIUM | 保留 `purchaseCalled bool` + `refreshSessionCalled bool` 字段,mock 方法内同时更新 bool 和 int。`go test ./internal/cli/` 即可发现回归。 |
| R4      | E2E-001 中 stdout 断言 `license acquired, retrying download...` 字面变化 | LOW        | LOW    | 字符串硬编码在 `app_download.go:256`,本 mission 不修改该文件,字面稳定。                                       |
| R5      | E2E-002 中 stderr 断言依赖错误包装链格式                                | LOW        | LOW    | `app_download.go:247` 的 `fmt.Errorf("re-login failed: %w", err)` 格式稳定;本 mission 不修改。                   |
| R6      | 真实 token 过期无法在 validate 阶段触发(M-1 opportunistic)            | MEDIUM     | LOW    | 自动化测试(E2E-001..005)是 required validation,已覆盖核心契约。M-1 是 smoke,不阻塞。fallback:`auth login`。 |

**Compatibility / migration / regression-sensitive areas**:
- 现有 6 个测试引用 `purchaseCalled` / `refreshSessionCalled`(`app_download_edge_test.go` / `device_test.go` / `auth_test.go`)— **必须**保留 bool 字段
- `TestDownload_TokenExpired_AutoRelogin`(`app_download_test.go:118`)— 验证 Download 路径不受 rename 影响

**Security / privacy / performance concerns**: 无(纯错误处理路径变化,无凭据/IO/算法变化)。

---

## 6. Pre-execution Baseline(2026-07-04)

```bash
$ git status --short
(empty — clean working tree)

$ git branch --show-current
feature/fix-purchase-token-expired

$ go build ./...
(exit 0,无输出)

$ go vet ./...
(exit 0,假设 — 验证步骤)

$ go test ./... -count=1
(全绿,201+ tests,基线)
```

**Branch**: `feature/fix-purchase-token-expired`(mission 创建时已切)
**Last commit**: `44da0e4` — design + e2e_test Spock PASS 修正
**Working tree**: clean

---

## 7. Decision-Complete Declaration

✅ 本 plan 是 decision-complete:

- **架构 / 接口**:沿用既有(`ProfileAppStore` interface 不变,`mapAppStoreError` helper 已定义)
- **任务顺序**:T1 → T2(串行,可并行)
- **验证策略**:每 task 自带 acceptance command(具体可执行);validate 阶段 opportunistic smoke
- **Mock 扩展**:字段名、类型、向后兼容策略已明确
- **文件级改动**:每个 task 的修改 / 新增文件清单完整

执行阶段(`mission_advance_stage` 到 execution)不需要发明任何决策,只需按本 plan 实施。
