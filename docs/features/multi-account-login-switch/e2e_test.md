# E2E Test Specification — multi-account-login-switch

> 本文档从 `requirements.md`（31 个 AC）和 `design.md`（8 个处理流）**单向派生**测试用例。每个 AC 至少被一个 E2E 用例覆盖。用例不验证实现细节，只验证**用户可观察行为**（CLI 输出 / 退出码 / 跨命令可见的状态）。

---

## 1. Test Scope

### 1.1 In Scope（自动化覆盖）

| 范围 | 覆盖方式 |
|------|---------|
| `auth login`（新 profile / refresh / 2FA / 错误密码 / 错误 2FA / Ctrl-C）| Mock Apple API（拦截 `appstore.AppStore` 接口）|
| `auth logout`（默认 active / 显式 / 幂等 / 不存在 / 无 active）| Mock keychain + temp dir |
| `accounts list`（空 / 多 profile 状态 / 含 ID 与 email）| Temp dir config + mock keychain |
| `accounts use`（成功 / 不存在 / 无凭据 / 本地操作不依赖网络）| Temp dir + mock keychain |
| `accounts remove`（确认 / 拒绝 / 不存在 / 级联 / active 清空 / 再 login）| Temp dir + mock keychain |
| Profile ID 派生算法 | 表驱动单元测试 |
| config.json 原子写 | Store 单元测试（temp dir）|
| 错误消息含下一步建议 | CLI 输出断言 |

### 1.2 Out of Scope（手动验收 / 不可自动化）

| 范围 | 原因 | 验收方式 |
|------|------|---------|
| 真实 Apple ID 登录 | 需真实账号 + 2FA，无法 CI | 手动：开发者用自己的 Apple ID 跑一次 `auth login` |
| macOS Keychain 解锁对话框 | OS 级 UI 交互 | 手动：首次运行观察授权弹窗 |
| ipatool 内部 Apple API 通信 | 外部依赖 | 手动：真实 login 成功即视为通过 |
| 并发操作（R3）| 行为未定义，不测 | N/A |

### 1.3 测试架构

```
┌─────────────────────────────────────────────────┐
│            E2E Test Runner (go test)             │
├─────────────────────────────────────────────────┤
│  Temp dir (~/.ipa-manager 替身)                  │
│  ├─ config.json (由测试 setup 创建/留空)          │
│  └─ profiles/<id>/cookies (由测试 setup 模拟)     │
├─────────────────────────────────────────────────┤
│  Mock Keychain (in-memory, 实现 keychain.Keychain)│
│  └─ map[string][]byte                            │
├─────────────────────────────────────────────────┤
│  Mock AppStore (实现 appstore.AppStore 接口)      │
│  ├─ Login: 按 email/password/authCode 编程返回    │
│  ├─ Bag: 返回固定 AuthEndpoint                    │
│  └─ Revoke: 删除 mock keychain entry             │
├─────────────────────────────────────────────────┤
│  CLI 执行 (call RunE directly 或 exec.Command)   │
│  → 捕获 stdout / stderr / exit code              │
└─────────────────────────────────────────────────┘
```

**Mock 注入点**：`appstore.NewProfileAppStore` 在测试中被替换为返回 mock AppStore 的版本（通过函数变量或接口注入）。

---

## 2. Environment Prerequisites

| 项 | 要求 |
|----|------|
| OS | macOS（Keychain 集成测试）；单元测试可在任意 OS |
| Go | ≥ 1.26（与 go.mod 一致）|
| ipatool | v2.3.0（已在 go.mod）|
| 网络 | 自动化测试**不需要**（全 mock）；真实 Apple 测试需联网 |
| 账号 | 自动化测试不需要；真实测试需用户自己的 Apple ID |

---

## 3. Validation Oracles

| 维度 | 断言方式 |
|------|---------|
| **退出码** | `exit code == 0` 或 `!= 0`，按 AC 指定 |
| **stdout** | 包含特定子串（如 profile ID、email、"Logged in"）|
| **stderr** | 包含错误描述 + 下一步建议（AC-07-3 范围内）|
| **config.json 状态** | 反序列化后检查 `active_profile_id` 和 `profiles[]` 字段 |
| **keychain 状态** | mock keychain 的 map 中是否存在 key `profiles/<id>/account` |
| **跨命令一致性** | 操作后运行 `accounts list` / `accounts use` 验证状态 |

---

## 4. E2E Test Cases

### E2E-001 — 首次 login 创建 profile 并自动 active（AC-01-1, AC-01-3）

- **Type**: happy
- **Given**: temp dir 为空（config.json 不存在）；mock keychain 为空。
- **When**: 运行 `auth login`，输入 `email=alice@example.com`、`password=correct`；mock AppStore.Login 第一次返回 `ErrAuthCodeRequired`，第二次（带 authCode）返回成功 + `Account{Name:"Alice",Email:"alice@example.com",StoreFront:"143441"}`。
- **Then**:
  - exit 0
  - stdout 含 "Logged in" 和 "Alice"
  - config.json 解析后：`active_profile_id == "alice_example_com"`，`profiles[0].id == "alice_example_com"`，`profiles[0].email == "alice@example.com"`
  - mock keychain 含 key `profiles/alice_example_com/account`
- **Pass/Fail**: 全部 Then 断言通过 = pass

### E2E-002 — 派生 ID 算法（AC-01-2）

- **Type**: happy + edge
- **Given**: temp dir 为空。
- **When**: 分别用以下 email 完成 mock login：`alice@example.com`、`Bob@Example.Com`、`a.b@c.d.e`。
- **Then**:
  - `accounts list` 分别显示 ID：`alice_example_com`、`bob_example_com`、`a_b_c_d_e`
- **Pass/Fail**: 三个 ID 全部匹配 = pass

### E2E-003 — 第二个 profile 不顶替 active（AC-01-4）

- **Type**: happy
- **Given**: config.json 已有 `alice_example_com`（active, logged-in）。
- **When**: 运行 `auth login`，输入 `email=bob@example.com`，mock login 成功。
- **Then**:
  - exit 0
  - `accounts list` 显示 `alice_example_com` 为 active，`bob_example_com` 为 logged-in 但非 active
  - config.json `active_profile_id == "alice_example_com"`（未变）
- **Pass/Fail**: active 未改变 = pass

### E2E-004 — refresh 已有 profile（派生 ID 冲突 = 刷新）

- **Type**: happy
- **Given**: config.json 已有 `alice_example_com`（logged-in）。
- **When**: 运行 `auth login`，输入 `email=alice@example.com`（同 email），mock login 成功返回更新后的 `Account{Name:"Alice Smith"}`。
- **Then**:
  - exit 0
  - config.json `profiles[]` 长度仍为 1（未新增）
  - `profiles[0].name == "Alice Smith"`（已更新）
  - active 未变（仍是 `alice_example_com`）
- **Pass/Fail**: profile 数量不变 + name 更新 = pass

### E2E-005 — `accounts use` 切换成功（AC-02-1）

- **Type**: happy
- **Given**: `alice_example_com` 与 `bob_example_com` 均 logged-in，active 为 `alice_example_com`。
- **When**: 运行 `accounts use bob_example_com`。
- **Then**:
  - exit 0
  - config.json `active_profile_id == "bob_example_com"`
  - `accounts list` 显示 `bob_example_com` 为 active
- **Pass/Fail**: active 已切换 = pass

### E2E-006 — `accounts use` 不存在的 profile（AC-02-2）

- **Type**: failure
- **Given**: profiles 中无 `ghost_example_com`。
- **When**: 运行 `accounts use ghost_example_com`。
- **Then**:
  - exit ≠ 0
  - stderr 含 "not found" 和 "accounts list"
  - config.json `active_profile_id` 未变
- **Pass/Fail**: 拒绝 + active 不变 = pass

### E2E-007 — `accounts use` 切到 logged-out profile（AC-02-3）

- **Type**: failure
- **Given**: `alice_example_com` 存在于 config.json，但 mock keychain 无 `profiles/alice_example_com/account`。
- **When**: 运行 `accounts use alice_example_com`。
- **Then**:
  - exit ≠ 0
  - stderr 含 "no credentials" 或 "auth login"
  - active 未变
- **Pass/Fail**: 拒绝 + 错误提示 = pass

### E2E-008 — `accounts use` 是本地操作（AC-02-4, NFR-01）

- **Type**: NFR
- **Given**: `alice_example_com` logged-in；mock AppStore 的 `Bag()` 被配置为返回 error（模拟 Apple API 不可达）。
- **When**: 运行 `accounts use alice_example_com`。
- **Then**:
  - exit 0（不受 Apple API 不可达影响）
  - active 已切换
- **Pass/Fail**: `use` 不依赖网络 = pass

### E2E-009 — `accounts list` 空列表（AC-03-1）

- **Type**: edge
- **Given**: temp dir 为空（config.json 不存在）。
- **When**: 运行 `accounts list`。
- **Then**:
  - exit 0
  - stdout 含 "No profiles" 或等义空状态提示
- **Pass/Fail**: 不报错 + 空提示 = pass

### E2E-010 — `accounts list` 多 profile 状态正确（AC-03-2）

- **Type**: happy
- **Given**: 三个 profile：alice（active, logged-in）、bob（logged-in）、charlie（logged-out）。
- **When**: 运行 `accounts list`。
- **Then**:
  - exit 0
  - 输出含全部三个 profile
  - 能区分：alice 有 active 标记 + logged-in；bob 有 logged-in；charlie 有 logged-out
- **Pass/Fail**: 三种状态均可区分 = pass

### E2E-011 — `accounts list` 含 ID 与 email（AC-03-3）

- **Type**: happy
- **Given**: `alice_example_com`（email `alice@example.com`）。
- **When**: 运行 `accounts list`。
- **Then**: 该行同时显示 `alice_example_com`（ID）和 `alice@example.com`（email）。
- **Pass/Fail**: ID 和 email 均可见 = pass

### E2E-012 — `accounts remove` 确认删除（AC-04-1）

- **Type**: happy
- **Given**: `bob_example_com` 存在且 logged-in。
- **When**: 运行 `accounts remove bob_example_com`，在确认提示选 "yes"。
- **Then**:
  - exit 0
  - `accounts list` 不再显示 `bob_example_com`
  - `accounts use bob_example_com` 以 "not found" 失败
  - mock keychain 无 `profiles/bob_example_com/account`
- **Pass/Fail**: 全部痕迹清除 = pass

### E2E-013 — `accounts remove` 非 active 不影响 active（AC-04-2）

- **Type**: happy
- **Given**: active 为 `alice_example_com`；`bob_example_com` 也存在。
- **When**: `accounts remove bob_example_com` 并确认。
- **Then**: config.json `active_profile_id == "alice_example_com"`（未变）。
- **Pass/Fail**: active 不变 = pass

### E2E-014 — `accounts remove` active 后清空 active（AC-04-3）

- **Type**: happy
- **Given**: active 为 `alice_example_com`。
- **When**: `accounts remove alice_example_com` 并确认。
- **Then**:
  - exit 0
  - config.json `active_profile_id == ""`
  - `auth logout`（无参数）以 "no active profile" 失败
- **Pass/Fail**: active 已清空 = pass

### E2E-015 — `accounts remove` 拒绝确认（AC-04-4）

- **Type**: edge
- **Given**: `alice_example_com` 存在。
- **When**: `accounts remove alice_example_com`，确认提示选 "no"。
- **Then**:
  - exit 0
  - `accounts list` 仍显示该 profile
  - config.json profiles[] 长度不变
- **Pass/Fail**: 不删除 = pass

### E2E-016 — `accounts remove` 不存在（AC-04-5）

- **Type**: failure
- **Given**: `ghost_example_com` 不存在。
- **When**: `accounts remove ghost_example_com`。
- **Then**:
  - exit ≠ 0
  - stderr 含 "not found"
  - 不弹确认提示（可通过确认 prompt 未被调用验证）
- **Pass/Fail**: 快速失败 = pass

### E2E-017 — 删除后 ID 行为如同从未存在（AC-04-6）

- **Type**: regression
- **Given**: `bob_example_com` 已被 remove。
- **When**: 分别运行 `accounts list`、`accounts use bob_example_com`、`auth logout bob_example_com`、`accounts remove bob_example_com`。
- **Then**:
  - `list`：不显示 bob
  - `use`：exit ≠ 0，"not found"
  - `logout`：exit ≠ 0，"not found"
  - `remove`：exit ≠ 0，"not found"
- **Pass/Fail**: 四个命令均表现为从未存在 = pass

### E2E-018 — 删除后同 email 再 login 走全新流程（AC-04-7）

- **Type**: regression
- **Given**: `bob_example_com` 已删除；无其他 profile。
- **When**: 运行 `auth login`，输入 `email=bob@example.com` + 完整凭据，mock login 成功。
- **Then**:
  - exit 0
  - config.json 出现 `bob_example_com`（fresh）
  - `active_profile_id == "bob_example_com"`（首个 profile 自动 active，AC-01-3）
- **Pass/Fail**: 再 login 成功 + active 行为正确 = pass

### E2E-019 — `auth logout` 默认作用于 active（AC-05-1）

- **Type**: happy
- **Given**: active 为 `alice_example_com`（logged-in）。
- **When**: 运行 `auth logout`（无参数）。
- **Then**:
  - exit 0
  - `accounts list` 显示 `alice_example_com` 为 logged-out
  - config.json `active_profile_id == "alice_example_com"`（未变）
  - profiles[] 仍含 alice（metadata 保留）
- **Pass/Fail**: 凭据清除 + metadata + active 保留 = pass

### E2E-020 — `auth logout` 显式指定（AC-05-2）

- **Type**: happy
- **Given**: `alice_example_com`（active, logged-in）、`bob_example_com`（logged-in）。
- **When**: 运行 `auth logout bob_example_com`。
- **Then**:
  - exit 0
  - `accounts list`：bob 为 logged-out，alice 仍 logged-in 且仍 active
- **Pass/Fail**: 仅目标被登出 = pass

### E2E-021 — `auth logout` 不存在（AC-05-3）

- **Type**: failure
- **Given**: `ghost_example_com` 不存在。
- **When**: `auth logout ghost_example_com`。
- **Then**: exit ≠ 0；stderr 含 "not found"。
- **Pass/Fail**: 拒绝 = pass

### E2E-022 — `auth logout` 无 active（AC-05-4）

- **Type**: failure
- **Given**: config.json `active_profile_id == ""`。
- **When**: `auth logout`（无参数）。
- **Then**: exit ≠ 0；stderr 含 "no active profile"。
- **Pass/Fail**: 拒绝 = pass

### E2E-023 — `auth logout` 已 logged-out 幂等（AC-05-5）

- **Type**: edge
- **Given**: `alice_example_com` 已 logged-out（mock keychain 无 entry）。
- **When**: `auth logout alice_example_com`。
- **Then**: exit 0（不报错）；状态仍 logged-out。
- **Pass/Fail**: 幂等成功 = pass

### E2E-024 — `auth logout` 保留 metadata（AC-05-6）

- **Type**: happy
- **Given**: `alice_example_com`（name="Alice", email="alice@example.com"）。
- **When**: `auth logout alice_example_com`。
- **Then**: `accounts list` 仍显示该 profile；name/email 字段不变。
- **Pass/Fail**: metadata 完整 = pass

### E2E-025 — 2FA 提示与成功（AC-06-1）

- **Type**: happy
- **Given**: mock AppStore.Login 第一次（AuthCode=""）返回 `ErrAuthCodeRequired`，第二次（AuthCode 非空）返回成功。
- **When**: 运行 `auth login`，输入 email、password，看到 2FA 提示后输入正确 code。
- **Then**:
  - exit 0
  - stdout 含 2FA 提示阶段（"2FA" 或 "verification code"）
  - 最终 stdout 含 "Logged in"
  - profile 为 logged-in
- **Pass/Fail**: 2FA 流程完整 = pass

### E2E-026 — 2FA 错误码失败（AC-06-2）

- **Type**: failure
- **Given**: mock AppStore.Login 第一次返回 `ErrAuthCodeRequired`，第二次（带错误 AuthCode）返回 Apple 错误。
- **When**: 运行 `auth login`，输入 2FA code "000000"（错误）。
- **Then**:
  - exit ≠ 0
  - stderr 含 Apple 的错误信息
  - mock keychain 无新 entry
  - config.json 无新 profile
- **Pass/Fail**: 失败 + 无副作用 = pass

### E2E-027 — 无 2FA 直接成功（AC-06-3）

- **Type**: happy
- **Given**: mock AppStore.Login 第一次（AuthCode=""）直接返回成功（不返回 ErrAuthCodeRequired）。
- **When**: 运行 `auth login`，输入 email、password。
- **Then**:
  - exit 0
  - stdout 不含 2FA 提示
  - profile 为 logged-in
- **Pass/Fail**: 跳过 2FA = pass

### E2E-028 — 错误密码不创建 profile（AC-07-1）

- **Type**: failure
- **Given**: mock AppStore.Login 返回非 `ErrAuthCodeRequired` 的认证失败。
- **When**: 运行 `auth login`，输入 email + 错误 password。
- **Then**:
  - exit ≠ 0
  - stderr 含 Apple 返回的失败信息
  - config.json 无新 profile
  - mock keychain 无新 entry
- **Pass/Fail**: 失败 + 无副作用 = pass

### E2E-029 — Ctrl-C 中止（AC-07-2）

- **Type**: edge
- **Given**: 任意状态。
- **When**: 运行 `auth login`，在 email 提示阶段发送中断信号（mock huh Input 返回 error 模拟 Ctrl-C）。
- **Then**:
  - exit ≠ 0
  - config.json 无新 profile
  - mock keychain 无新 entry
- **Pass/Fail**: 无副作用 = pass

### E2E-030 — 自身命令错误含下一步建议（AC-07-3）

- **Type**: NFR
- **Given**: N/A（参数化多场景）。
- **When**: 分别触发：
  - `accounts use ghost_example_com`（profile 不存在）
  - `accounts use <logged-out-id>`（无凭据）
  - `auth logout`（无 active）
- **Then**: 每个场景的 stderr 除错误描述外，还含明确的下一步建议（如 "Run `accounts list`"、"Run `auth login`"）。
- **Pass/Fail**: 三个场景均含建议 = pass

### E2E-031 — 性能：本地命令 < 500ms（NFR-01）

- **Type**: NFR
- **Given**: config.json 有 10 个 profile（5 logged-in + 5 logged-out）。
- **When**: 分别运行 `accounts list`、`accounts use <id>`、`accounts remove <id>`（mock keychain，无真实 keychain 开销）。
- **Then**: 每个命令的 wall clock 时间 < 500ms。
- **Pass/Fail**: 三个命令均在预算内 = pass
- **注**: 真实 keychain 的耗时无法在 mock 中精确模拟；此用例验证的是 ipa-manager 自身逻辑的开销，真实 keychain 延迟由 macOS 保证。

### E2E-032 — config.json 原子写（NFR-04, DD-10）

- **Type**: NFR
- **Given**: temp dir 监控文件系统操作。
- **When**: 触发 `store.Save()`（任何修改 config 的命令）。
- **Then**: 观察到 `config.json.tmp` 创建后 `rename` 为 `config.json`（而非直接覆写）；中途崩溃不会留下损坏的 `config.json`。
- **Pass/Fail**: 写入路径走 rename = pass

### E2E-033 — 级联失败报告（NFR-04）

- **Type**: failure
- **Given**: `alice_example_com` 存在；mock AppStore.Revoke 被配置为返回 error（模拟 keychain 故障）。
- **When**: `accounts remove alice_example_com` 并确认。
- **Then**:
  - exit ≠ 0
  - stderr 报告哪个级联步骤失败（含 "revoke" 或 "keychain"）
  - metadata 仍被移除（best-effort）或报告 metadata 移除也失败
- **Pass/Fail**: 不静默部分成功 = pass

### E2E-034 — active 指向 logged-out profile 的契约（AC-05-7）

- **Type**: regression
- **Given**: active 为 `alice_example_com`；该 profile 已 logged-out（通过先 `auth logout` 达成）。
- **When**: 再次运行 `auth logout`（无参数）。
- **Then**:
  - exit 0（幂等，AC-05-5）
  - `accounts list` 显示 alice 为 logged-out，仍为 active
- **Pass/Fail**: 幂等 + active 不变 = pass
- **注**: 本 mission 范围内仅 `auth logout` 消费 active；后续 mission 的 active 依赖命令不在测试范围（契约已在 AC-05-7 声明）。

### E2E-035 — Profile ID 派生单元测试（§4.1 算法）

- **Type**: unit（表驱动）
- **Given**: 一组 email → expected ID 映射。
- **When**: 调用 `account.DeriveProfileID(email)`。
- **Then**: 返回值 == expected ID。

**测试数据**：

| Email | Expected ID |
|-------|-------------|
| `alice@example.com` | `alice_example_com` |
| `Bob@Example.Com` | `bob_example_com` |
| `a.b@c.d.e` | `a_b_c_d_e` |
| `user+tag@domain.org` | `user_tag_domain_org` |
| `already_id-styled@x.io` | `already_id-styled_x_io` |
| `UPPER@CASE.COM` | `upper_case_com` |
| `multi..dot@x.com` | `multi__dot_x_com` |

- **Pass/Fail**: 全部 case 通过 = pass

### E2E-036 — Store 状态机单元测试

- **Type**: unit
- **Given**: temp dir + 空 config。
- **When**: 按序执行：
  1. `store.Upsert(p1)` → `store.Upsert(p2)`
  2. `store.SetActive(p1.ID)`
  3. `store.GetActiveID()` == p1.ID
  4. `store.Remove(p1.ID)`
  5. `store.GetActiveID()` == ""（p1 是 active，remove 后清空）
  6. `store.Get(p1.ID)` → error（不存在）
  7. `store.List()` == [p2]
- **Then**: 每步断言通过。
- **Pass/Fail**: 状态机全路径通过 = pass

---

## 5. Traceability Matrix

| E2E Case | Type | US | AC | NFR |
|----------|------|----|----|-----|
| E2E-001 | happy | US-01, US-06 | AC-01-1, AC-01-3, AC-06-1 | — |
| E2E-002 | happy+edge | US-01 | AC-01-2 | — |
| E2E-003 | happy | US-01 | AC-01-4 | — |
| E2E-004 | happy | US-01 | (§4.1 冲突=refresh) | — |
| E2E-005 | happy | US-02 | AC-02-1 | — |
| E2E-006 | failure | US-02, US-07 | AC-02-2, AC-07-3 | — |
| E2E-007 | failure | US-02, US-07 | AC-02-3, AC-07-3 | — |
| E2E-008 | NFR | US-02 | AC-02-4 | NFR-01 |
| E2E-009 | edge | US-03 | AC-03-1 | — |
| E2E-010 | happy | US-03 | AC-03-2 | — |
| E2E-011 | happy | US-03 | AC-03-3 | — |
| E2E-012 | happy | US-04 | AC-04-1 | — |
| E2E-013 | happy | US-04 | AC-04-2 | — |
| E2E-014 | happy | US-04 | AC-04-3 | — |
| E2E-015 | edge | US-04 | AC-04-4 | — |
| E2E-016 | failure | US-04, US-07 | AC-04-5, AC-07-3 | — |
| E2E-017 | regression | US-04 | AC-04-6 | — |
| E2E-018 | regression | US-01, US-04 | AC-04-7, AC-01-3 | — |
| E2E-019 | happy | US-05 | AC-05-1 | — |
| E2E-020 | happy | US-05 | AC-05-2 | — |
| E2E-021 | failure | US-05, US-07 | AC-05-3, AC-07-3 | — |
| E2E-022 | failure | US-05, US-07 | AC-05-4, AC-07-3 | — |
| E2E-023 | edge | US-05 | AC-05-5 | — |
| E2E-024 | happy | US-05 | AC-05-6 | — |
| E2E-025 | happy | US-06 | AC-06-1 | — |
| E2E-026 | failure | US-06 | AC-06-2 | — |
| E2E-027 | happy | US-06 | AC-06-3 | — |
| E2E-028 | failure | US-07 | AC-07-1 | — |
| E2E-029 | edge | US-07 | AC-07-2 | — |
| E2E-030 | NFR | US-07 | AC-07-3 | NFR-07 |
| E2E-031 | NFR | — | — | NFR-01 |
| E2E-032 | NFR | — | — | NFR-04, DD-10 |
| E2E-033 | failure | US-04 | — | NFR-04 |
| E2E-034 | regression | US-05 | AC-05-7 | — |
| E2E-035 | unit | US-01 | AC-01-2 | — |
| E2E-036 | unit | — | — | — |

### Reverse Coverage Check（每个 AC 至少被 1 个 E2E 覆盖）

| AC | 覆盖用例 |
|----|---------|
| AC-01-1 | E2E-001 |
| AC-01-2 | E2E-002, E2E-035 |
| AC-01-3 | E2E-001, E2E-018 |
| AC-01-4 | E2E-003 |
| AC-02-1 | E2E-005 |
| AC-02-2 | E2E-006, E2E-017 |
| AC-02-3 | E2E-007, E2E-017 |
| AC-02-4 | E2E-008 |
| AC-03-1 | E2E-009 |
| AC-03-2 | E2E-010 |
| AC-03-3 | E2E-011 |
| AC-04-1 | E2E-012 |
| AC-04-2 | E2E-013 |
| AC-04-3 | E2E-014 |
| AC-04-4 | E2E-015 |
| AC-04-5 | E2E-016, E2E-017 |
| AC-04-6 | E2E-017 |
| AC-04-7 | E2E-018 |
| AC-05-1 | E2E-019 |
| AC-05-2 | E2E-020 |
| AC-05-3 | E2E-021, E2E-017 |
| AC-05-4 | E2E-022 |
| AC-05-5 | E2E-023, E2E-034 |
| AC-05-6 | E2E-024 |
| AC-05-7 | E2E-034 |
| AC-06-1 | E2E-001, E2E-025 |
| AC-06-2 | E2E-026 |
| AC-06-3 | E2E-027 |
| AC-07-1 | E2E-028 |
| AC-07-2 | E2E-029 |
| AC-07-3 | E2E-006, E2E-007, E2E-016, E2E-021, E2E-022, E2E-030 |

**无遗漏**：全部 31 个 AC 至少被一个 E2E 用例覆盖。

### Reverse Coverage Check（每个 US）

| US | 覆盖用例 |
|----|---------|
| US-01 | E2E-001, 002, 003, 004, 018, 025-028, 035 |
| US-02 | E2E-005-008 |
| US-03 | E2E-009-011 |
| US-04 | E2E-012-018, 033 |
| US-05 | E2E-019-024, 034 |
| US-06 | E2E-001, 025-027 |
| US-07 | E2E-006, 007, 016, 021, 022, 028, 029, 030 |

**无遗漏**：全部 7 个 US 均有 E2E 覆盖。

---

## 6. Test Suite Summary

| 维度 | 数量 |
|------|------|
| 总 E2E + 单元用例 | 36 |
| happy | 13 |
| failure | 9 |
| edge | 6 |
| regression | 3 |
| NFR | 5 |
| unit | 2（含表驱动 7 个子 case）|
| 自动化可覆盖 | 34（E2E-001..034 + 单元）|
| 需手动验收 | 2（真实 Apple login + Keychain 授权对话框，见 §1.2）|
