# E2E Test — fix-purchase-token-expired

> 本文档从 `requirements.md`(US-01,3 ACs)+ `design.md`(DD-01/02)派生。遵循 spec → cases → code 单向流:测试用例不反向从实现推导。

---

## 1. Test Scope

### 1.1 测试类型

| 类型 | 覆盖范围 | 说明 |
|------|----------|------|
| **单元测试**(Go test) | adapter 层 Purchase 方法的错误转换(`internal/appstore/client_test.go`) | mock `ipaappstore.AppStore` interface,白盒构造 `profileAppStoreAdapter` |
| **E2E 测试**(Go test) | CLI 层 `handleLicenseRequired` 中 Purchase token-expired 重试路径 | mock `ProfileAppStore`,cobra RunE 入口,断言 mock 调用次数 + exit code |
| **回归测试**(Go test) | 全项目现有测试 | `go test ./... -count=1` exit 0 |
| **手动验收**(validate 阶段) | 真实 Apple session token 过期 + `device install` | 等待 token 自然过期 或 手动 revoke 触发 |

### 1.2 测试环境前置

- **自动化**:Go ≥ 1.26;`go test ./... -count=1`;无网络依赖(全 mock)。
- **手动**:macOS;已登录 Apple 账号;等待 token 自然过期(数小时~数天)或 Apple ID 后台 revoke session。

### 1.3 Validation Oracles(断言层次)

| 层次 | 断言对象 | 示例 |
|------|----------|------|
| **L1 — exit code** | RunE 返回 nil(exit 0)或 error(exit 1) | `require.NoError(t, err)` / `require.Error(t, err)` |
| **L2 — error 类型** | returned error 是 apperr sentinel | `assert.True(t, errors.Is(err, apperr.ErrPasswordTokenExpired))` |
| **L3 — mock 调用次数** | mock 的方法被调用 / 调用次数 | `assert.Equal(t, 2, mockAS.purchaseCalls)` |
| **L4 — mock 调用顺序** | RefreshSession 在第二次 Purchase 之前 | `assert.Before(t, mockAS.refreshAt, mockAS.secondPurchaseAt)` |
| **L5 — 手动**(validate) | 真实 install 成功 + stderr 无敏感术语 | `./bin/ipa-manager device install <bundle>` exit 0 |

> 自动化覆盖 L1-L4。L5 在 validate 阶段手动执行。

---

## 2. E2E Test Cases

### US-01 — Purchase token-expired auto-refresh + retry

#### E2E-001 / AC-01-1 — Happy path: Purchase token-expired → refresh → retry → 成功

- **Type**: happy
- **Layer**: CLI(端到端编排)
- **Given**:
  - active profile 已登录;keychain 有缓存凭据
  - mockLibraryStore 不包含目标 app(强制触发 download+purchase 路径)
  - mockAppStore.AppInfo / Lookup 正常返回(免费 app, price=0)
  - mockAppStore.Search 路径不触发(直接用 bundle-id)
  - **mockAppStore.Download 第一次返回 `apperr.ErrLicenseRequired`**(触发 license acquire)
  - **mockAppStore.Purchase 第一次返回 `apperr.ErrPasswordTokenExpired`**(模拟 fix 后的 adapter 行为)
  - mockAppStore.RefreshSession 返回 nil(模拟 refresh 成功)
  - mockAppStore.Purchase 第二次返回 nil(模拟重试成功)
  - mockAppStore.Download 第二次返回成功 + Sinf 数据
  - mockUI.Confirm 返回 true(用户确认 acquire)
- **When**: 运行 `app download <bundle-id>` 或 `device install <bundle-id>`(任意触发 handleLicenseRequired 的命令)
- **Then**:
  - exit 0
  - mockAppStore.purchaseCalls == 2
  - mockAppStore.refreshSessionCalls == 1
  - mockLibraryStore.addCalled == true(IPA 入库)
  - stderr 不包含 `STDQ` 或 `password token is expired`
- **Pass**: 全部 Then 断言通过
- **Maps to**: AC-01-1, NFR-04

#### E2E-002 / AC-01-2 — RefreshSession 也失败:友好错误

- **Type**: failure
- **Layer**: CLI
- **Given**:
  - 同 E2E-001 setup
  - mockAppStore.Purchase 第一次返回 `apperr.ErrPasswordTokenExpired`
  - **mockAppStore.RefreshSession 返回 errors.New("simulated keychain password invalid")**(模拟密码已变)
- **When**: 运行 `app download <bundle-id>` 并确认 acquire
- **Then**:
  - exit 非 0
  - stderr **以** `Error: re-login failed:` 开头
  - stderr **包含** `simulated keychain password invalid`(底层错误经 `%w` 包装,可追溯)
  - stderr **不包含** `STDQ`
  - stderr **不包含** `password token is expired`
  - mockAppStore.purchaseCalls == 1(refresh 失败后未重试)
  - mockAppStore.refreshSessionCalls == 1
- **Pass**: 全部 Then 断言通过
- **Maps to**: AC-01-2, NFR-05

#### E2E-003 / AC-01-3 — 非 token 错误:行为不变

- **Type**: regression
- **Layer**: CLI
- **Given**:
  - 同 E2E-001 setup
  - **mockAppStore.Purchase 第一次返回 errors.New("network timeout")**(非 sentinel)
- **When**: 运行 `app download <bundle-id>` 并确认 acquire
- **Then**:
  - exit 非 0
  - stderr **以** `Error: license acquisition failed:` 开头
  - stderr **包含** `network timeout`(原始错误)
  - stderr **不包含** `session expired, re-authenticating`(证明未触发 refresh)
  - mockAppStore.refreshSessionCalls == 0
  - mockAppStore.purchaseCalls == 1
- **Pass**: 全部 Then 断言通过
- **Maps to**: AC-01-3, NFR-04

#### E2E-004 / NFR-06 — Adapter 层 sentinel 转换契约

- **Type**: unit
- **Layer**: adapter(白盒)
- **Given**:
  - 白盒构造 `profileAppStoreAdapter{inner: mockInner, account: &testAccount}`
  - mockInner.Purchase 返回 `ipaappstore.ErrPasswordTokenExpired`(ipatool 原始 sentinel)
- **When**: 调用 `adapter.Purchase("com.test", 123, 0)`
- **Then**:
  - returned error != nil
  - `errors.Is(returnedErr, apperr.ErrPasswordTokenExpired)` == **true**(已转换)
  - `errors.Is(returnedErr, ipaappstore.ErrPasswordTokenExpired)` == false(不再泄露原始 sentinel —— 注:由于 mapAppStoreError 不 wrap,这是新 sentinel,不是 wrapper)
- **Pass**: 全部 Then 断言通过
- **Maps to**: NFR-06(隐式 — 验证 mapAppStoreError 转换契约), DD-01

#### E2E-005 / NFR-06 — Adapter 层非 token 错误透传

- **Type**: unit (edge)
- **Layer**: adapter(白盒)
- **Given**:
  - 白盒构造 `profileAppStoreAdapter`
  - mockInner.Purchase 返回 `errors.New("apple 500 error")`(非 sentinel)
- **When**: 调用 `adapter.Purchase(...)`
- **Then**:
  - returned error != nil
  - `errors.Is(returnedErr, apperr.ErrPasswordTokenExpired)` == false
  - returned error.Error() == "apple 500 error"(原样透传)
- **Pass**: 全部 Then 断言通过
- **Maps to**: NFR-06, AC-01-3(adapter 层对应契约)

#### E2E-006 / NFR-01 — 全项目无回归

- **Type**: regression
- **Layer**: 全项目
- **Given**: 本 mission 所有 fix 已 apply
- **When**: 运行 `go test ./... -count=1`
- **Then**:
  - exit 0
  - 无 FAIL
  - 测试数 ≥ 修复前 baseline(不引入新失败)
- **Pass**: 全部 Then 断言通过
- **Maps to**: NFR-01, NFR-04

#### E2E-007 / NFR-02 — go.mod / go.sum 无变更

- **Type**: artifact
- **Given**: 本 mission 所有 fix 已 apply
- **When**: `git diff go.mod go.sum`
- **Then**: 输出为空
- **Pass**: `git diff --exit-code go.mod go.sum` exit 0
- **Maps to**: NFR-02

---

## 3. Traceability Matrix

| E2E Case | US    | AC      | NFR        | Type       | Layer   |
| -------- | ----- | ------- | ---------- | ---------- | ------- |
| E2E-001  | US-01 | AC-01-1 | NFR-04     | happy      | CLI     |
| E2E-002  | US-01 | AC-01-2 | NFR-05     | failure    | CLI     |
| E2E-003  | US-01 | AC-01-3 | NFR-04     | regression | CLI     |
| E2E-004  | —     | —       | NFR-06     | unit       | adapter |
| E2E-005  | —     | AC-01-3 | NFR-06     | unit       | adapter |
| E2E-006  | —     | —       | NFR-01, 04 | regression | 全项目  |
| E2E-007  | —     | —       | NFR-02     | artifact   | 全项目  |

**Reverse coverage check**:
- ✅ US-01 covered by E2E-001 / 002 / 003
- ✅ AC-01-1 covered by E2E-001
- ✅ AC-01-2 covered by E2E-002
- ✅ AC-01-3 covered by E2E-003(CLI)+ E2E-005(adapter)
- ✅ AC-01-4(自动化测试覆盖元 AC)covered by E2E-001 ~ 005 本身的存在
- ✅ NFR-01 covered by E2E-006
- ✅ NFR-02 covered by E2E-007
- ✅ NFR-03(无 secret leak)由 code review + `rg` 在 implementation phase 验证
- ✅ NFR-04(boundary)covered by E2E-006(回归)
- ✅ NFR-05(observability)covered by E2E-002(stderr 断言)
- ✅ NFR-06(maintainability)covered by E2E-004 + E2E-005 + code review(确认 errors.go 共享 helper)

---

## 4. Manual Validation(validate 阶段)

由于本 mission 修复的核心场景是"真实 Apple token 过期",自动化测试无法完全替代真实环境验证。validate 阶段需执行:

### M-1 — 真实 token 过期 → install 成功

**前置**:
- 已登录 Apple 账号(`accounts list` 显示 logged-in)
- 等待 password token 自然过期(数小时~数天)**或** 在 Apple ID 后台(appleid.apple.com)手动 revoke 该 session

**步骤**:
1. `./bin/ipa-manager device install <某个之前未下载过的免费 app bundle-id>`
2. 观察输出

**预期**:
- 看到 `session expired, re-authenticating...` 提示
- 看到 `license acquired, retrying download...` 提示(或类似)
- install 最终成功
- stderr 无 `STDQ` / `password token is expired`

**回滚预案**:如 fix 失效(罕见),fallback 是先 `auth login` 再 install(老 workaround,见 requirements §1)。

### M-2 — Code review checklist(validate 阶段)

- [ ] `git diff go.mod go.sum` 为空(NFR-02)
- [ ] `internal/appstore/errors.go` 只 rename,转换逻辑不变
- [ ] `internal/appstore/client_impl.go` Purchase 调用 mapAppStoreError,Download 调用同名
- [ ] `internal/cli/app_download.go` 无任何改动
- [ ] 新增测试无真实 Apple ID / password / token(NFR-03)
- [ ] `grep -rn "danielpaulus/go-ios\|majd/ipatool" internal/cli` 仍无结果(AGENTS.md 边界约束)
