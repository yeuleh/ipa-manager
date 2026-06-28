# Requirements — multi-account-login-switch

> Feature mission: 实现多 Apple 账号的添加（登录）及切换。
>
> 本文档是 design / plan / execution / validate 阶段的唯一需求源头。所有验收标准验证的是**用户可观察行为**（CLI 输出、退出码、通过其他 CLI 命令可见的副作用），不验证内部实现状态。

---

## 1. 意图与上下文

### 1.1 解决什么问题

ipa-manager 的核心价值主张是「多 Apple 账号切换 + 按账号隔离」。脚手架阶段（Stage 5）已实现隔离机制（`ProfileKeychain` namespace 包装、per-profile cookie jar 路径，ADR 0002），但**账号生命周期本身**的全部命令仍是 `ErrNotImplemented` stub——用户当前无法：

- 登录一个 Apple ID 并把它保存为 profile
- 在多个 profile 之间切换 active 指针
- 列出 / 删除 / 登出已配置的 profile

本 mission 填实这一空白：把账号的**添加（登录）/ 切换 / 列举 / 删除 / 登出**全链路打通，使 ipa-manager 具备「多账号管理」这一最小可用闭环。后续 mission（应用下载、设备安装）将以本 mission 建立的「active profile」契约为前提。

### 1.2 使用者

- **主要演员**：ipa-manager 的唯一用户（个人开发者，单机 macOS）。该用户拥有 1 个或多个 Apple ID，希望在它们之间切换以分别管理各自购买/拥有的 IPA。

### 1.3 当前状态 / 痛点

- 脚手架能编译、能跑 `--help`，但所有账号命令返回 `not yet implemented`。
- `ProfileKeychain` 隔离机制已就绪并有测试覆盖，但**没有任何命令会触发它**——隔离能力处于「有架构、无使用」状态。
- 用户当前只能直接用 raw `ipatool`，而它原生**不支持多账号**（固定写 keychain key `"account"`，登录第二个覆盖第一个）。

### 1.4 期望结果

用户能完成如下端到端流程，无需手动编辑任何配置文件或直接操作 Keychain：

```
ipa-manager auth login          # 登录 alice@example.com → 自动创建 profile
ipa-manager auth login          # 登录 bob@example.com   → 自动创建第二个 profile
ipa-manager accounts list       # 看到 alice + bob，alice 是 active
ipa-manager accounts use bob_example_com   # 切到 bob
ipa-manager auth logout         # 登出 bob（保留 metadata，可再次 login 刷新）
ipa-manager accounts remove alice_example_com  # 彻底删除 alice
```

---

## 2. Actors / Assumptions / Dependencies

### 2.1 Actors

| Actor | 描述 |
|-------|------|
| User | 单用户，本机操作 ipa-manager CLI 的开发者 |
| Apple IDP | Apple 的身份认证服务（通过 ipatool 间接交互），处理 email/password/2FA 校验，返回 account token JSON |
| macOS Keychain | 凭据持久化后端（通过 ipatool 的 keyring 接口） |

### 2.2 Assumptions

- **A1**：ipatool v2.3.0 的 `AppStore.Login` API 稳定；登录成功后 ipatool 自动把 account JSON 写入注入的 `Keychain`（即我们的 `ProfileKeychain`），本 mission 不需要手动管理 token 持久化。
- **A2**：macOS Keychain 可访问（系统授权、未锁屏、无权限问题）。若不可访问，`auth login` 在调用 ipatool 时会失败——本 mission 把这个失败**透传**为可读错误，不做特殊恢复。
- **A3**：单用户单机，**无并发访问**。同一 profile 的并发登录可能导致 keychain/cookie jar 写竞争——作为已知限制记录（见 §8 风险），v1 不做互斥。
- **A4**：用户的 Apple ID 普遍启用了 2FA。`auth login` 必须支持 2FA 重试流；不支持仅密码账号的特判（ipatool 自己决定是否需要 2FA）。
- **A5**：Profile 元数据（ID/Name/Email/StoreFront）非敏感——明文存于 `~/.ipa-manager/config.json`。真正的凭据（token JSON）只在 Keychain。

### 2.3 Dependencies

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/majd/ipatool/v2` | v2.3.0 | Apple API 调用、Keychain 接口契约、`ErrAuthCodeRequired` sentinel |
| `github.com/danielpaulus/go-ios` | v1.2.0 | 不直接使用，但需共存编译（脚手架已验证） |
| `charm.land/huh/v2` | v2.0.3 | 交互提示（email/password/2FA/选择/确认） |
| `charm.land/lipgloss/v2` | v2.0.4 | `accounts list` 表格渲染 |
| `github.com/spf13/cobra` | v1.10.2 | CLI 框架 |
| macOS Keychain | — | 凭据存储（通过 ipatool 的 `99designs/keyring`） |

---

## 3. Scope

### 3.1 In Scope

1. **`auth login`** — 统一入口：交互式收集 email/password（+ 2FA），完成 Apple 认证，自动创建或刷新 profile。
2. **`auth logout [<profile-id>]`** — 默认登出 active profile；显式指定则登出该 profile。Revoke keychain 凭据 + 清 cookie jar，保留 profile 元数据。
3. **`accounts list`** — 列出所有 profile 及状态（active / logged-in / logged-out）。
4. **`accounts use <profile-id>`** — 切换 active profile（严格：profile 必须存在且已登录）。
5. **`accounts remove <profile-id>`** — 全量删除 profile（元数据 + keychain namespace + cookie jar），需确认。
6. **Profile store 实现** — `account.Store` 支持 `List/Get/Add/Remove`，持久化到 `~/.ipa-manager/config.json`。
7. **Config 实现** — `config.Load/Save` 持久化 `ActiveProfileID`。
8. **`appstore.NewProfileAppStore`** — 工厂接线，注入 `ProfileKeychain` + per-profile cookie jar，调 ipatool 的 `appstore.NewAppStore`。
9. **UI 提示器** — `ui.SelectProfile/Confirm/InputAuthCode` + email/password 输入实现。
10. **2FA 重试流** — `ErrAuthCodeRequired` → 提示 → 同 email/password + AuthCode 重试一次。

### 3.2 Out of Scope（留给后续 mission）

- `apps search` / `apps versions` — 应用搜索与版本列举。
- `devices list` — iOS 设备列举。
- `install download/push/uninstall/update` — IPA 下载与设备安装。
- `doctor` — 环境健康检查（独立 mission）。
- `library` — 本地 IPA 隔离存储管理。
- 并发登录互斥（见 §8 已知限制）。
- Token 过期主动检测与自动刷新（依赖 ipatool 在调用时抛错；被动刷新由用户运行 `auth login` 触发）。
- Profile metadata 加密。
- Profile import/export 与跨机器同步。

### 3.3 Non-goals

- **不持久化 Apple ID 密码**：password 仅传给 Apple API，任何时刻都不写盘。
- **不绕过 2FA**：所有 2FA 流程遵守 Apple 规则。
- **不做密码强度校验 / email 格式校验**：交给 Apple 判定（错误密码/格式由 Apple 返回，本 mission 透传）。
- **不提供 `--id` 手动覆盖**：profile ID 严格由 email 派生（见 §4.1 算法）。
- **不修改 ipatool 源码**：隔离完全通过 `ProfileKeychain` 注入完成（ADR 0002）。

---

## 4. 关键设计决策（用户已确认）

### 4.1 Profile ID 派生算法

输入 email → 输出 profile ID：

1. lowercase
2. 把每个**非** `[a-z0-9_-]` 字符替换为 `_`

示例：

| Email | Profile ID |
|-------|-----------|
| `alice@example.com` | `alice_example_com` |
| `Bob@Example.Com` | `bob_example_com` |
| `a.b@c.d.e` | `a_b_c_d_e` |
| `alice+work@example.com` | `alice_work_example_com` |

**冲突处理**：若 `auth login` 输入的 email 派生出的 ID 已存在，**视为刷新**（更新该 profile 的凭据），不创建副本，不报错。

**已知限制**：不同 email 可能映射到同一 ID（如 `alice+work@x.com` 与 `alice.work@x.com` 都 → `alice_work_x_com`）。此场景下第二次 login 会刷新第一个 profile。作为 v1 已知限制接受（见 §8）。

### 4.2 命令树（最终版）

```
ipa-manager
├── auth
│   ├── login              # 统一入口：add + login + refresh
│   └── logout [<id>]      # 默认 active；可显式指定
└── accounts
    ├── list
    ├── use <id>
    └── remove <id>
```

**`accounts add` 命令移除**（其职责完全被 `auth login` 吸收）。

### 4.3 状态模型

每个 profile 在任意时刻处于以下两种凭据状态之一：

| 状态 | 判定 | 含义 |
|------|------|------|
| **logged-in** | `ProfileKeychain.Get("account")` 成功返回非空 | 有有效或待验证的 token |
| **logged-out** | `ProfileKeychain.Get("account")` 失败/为空 | 从未登录，或被 `auth logout` 显式登出，或被 `accounts remove` 删除（删除后不存在） |

外加上「active」标记（独立于凭据状态，仅由 `config.ActiveProfileID` 决定）。

---

## 5. User Stories

### US-01 (P1) — 首次添加账号
> As a user，我想要 `auth login` 用我的 Apple ID 登录并自动保存为新 profile，这样我之后能切回这个账号使用。

**优先级理由**：核心闭环起点；无此能力则多账号管理不存在。

### US-02 (P1) — 切换账号
> As a user，我想要 `accounts use <id>` 把 active profile 切到另一个已登录的 profile，这样后续操作针对所切账号。

**优先级理由**：「多账号」的「多」就靠切换体现。

### US-03 (P1) — 列举账号与状态
> As a user，我想要 `accounts list` 看到所有 profile（active / logged-in / logged-out），这样我知道当前有哪些账号可用。

**优先级理由**：无列举则用户无法知道 profile ID，无法完成 `use` / `remove`。

### US-04 (P2) — 删除账号
> As a user，我想要 `accounts remove <id>` 彻底删除一个 profile 及其凭据，这样清理不再使用的账号。

**优先级理由**：完整生命周期需要，但非闭环必需。

### US-05 (P2) — 登出但保留元数据
> As a user，我想要 `auth logout` 撤销 active profile 的凭据但保留 profile 元数据，这样之后能通过再次 `auth login` 刷新而非重建。

**优先级理由**：与 remove 形成完整语义对偶；支持 token 过期场景。

### US-06 (P2) — 2FA 登录
> As a user，我的 Apple ID 启用了 2FA，我希望 `auth login` 在需要时提示我输入 2FA 验证码，这样登录能成功完成。

**优先级理由**：现代 Apple ID 几乎都有 2FA，无此能力则大部分登录失败。

### US-07 (P3) — 错误可读性
> As a user，当我切换到不存在的 profile 或凭据缺失的 profile 时，我想要看到清晰的错误和下一步建议，这样我知道怎么修复。

**优先级理由**：影响体验但非功能缺失；P3 因为 strict-fail 本身已提供基本反馈。

---

## 6. Acceptance Criteria

> **可观察性约定**：所有 Then 子句验证的是 CLI 退出码 + stdout/stderr 输出 + 通过其他 CLI 命令可观察的状态。**不**直接窥探 keychain / 文件系统内部结构（这些是 implementation detail，可能随实现演进）。

### US-01 — 首次添加账号

**AC-01-1 — 首次登录创建 profile**
- **Given**：尚无 email 为 `alice@example.com` 的 profile。
- **When**：运行 `auth login`，交互输入 `email=alice@example.com`、有效 password、需要的 2FA code。
- **Then**：命令以 exit 0 退出；输出确认登录成功；随后 `accounts list` 显示一个 profile，其 ID 为 `alice_example_com`，状态为 logged-in；它不是 active（除非是首个 profile，见 AC-01-3）。

**AC-01-2 — 派生 ID 算法**
- **Given**：任意 email 输入。
- **When**：完成 `auth login`。
- **Then**：`accounts list` 显示的 ID 与 §4.1 算法派生的结果一致（如 `Bob@Example.Com` → `bob_example_com`）。

**AC-01-3 — 首个 profile 自动成为 active**
- **Given**：当前无任何 profile 存在（`accounts list` 显示空）。
- **When**：完成首次 `auth login`。
- **Then**：`accounts list` 标记该 profile 为 active；运行 `ipa-manager auth logout`（无参数）时影响的就是这个 profile（可通过再次 `accounts list` 看到它变为 logged-out 验证）。

**AC-01-4 — 第二个 profile 不自动顶替 active**
- **Given**：已有 1 个 active profile。
- **When**：登录第二个不同 email 的 profile。
- **Then**：`accounts list` 显示 active 仍为第一个；新 profile 状态为 logged-in 但非 active。

### US-02 — 切换账号

**AC-02-1 — 切到已登录 profile 成功**
- **Given**：profiles `alice_example_com` 与 `bob_example_com` 均 logged-in，active 为 `alice_example_com`。
- **When**：运行 `accounts use bob_example_com`。
- **Then**：exit 0；`accounts list` 显示 active 为 `bob_example_com`。

**AC-02-2 — 切到不存在的 profile 拒绝**
- **Given**：profile `ghost_example_com` 不存在。
- **When**：运行 `accounts use ghost_example_com`。
- **Then**：exit 非零；stderr 包含「profile 不存在」（或等义中文）的提示；`accounts list` 显示的 active 未改变。

**AC-02-3 — 切到 logged-out profile 拒绝**
- **Given**：profile `alice_example_com` 存在但处于 logged-out 状态。
- **When**：运行 `accounts use alice_example_com`。
- **Then**：exit 非零；stderr 包含「无凭据/未登录，请先 `auth login`」（或等义）的提示；active 未改变。

**AC-02-4 — `use` 不发起网络请求**
- **Given**：profile 已 logged-in。
- **When**：运行 `accounts use <id>`。
- **Then**：命令在 < 500ms 内完成（验证 `use` 是纯本地指针操作，不验证 Apple token 有效性）。

### US-03 — 列举账号

**AC-03-1 — 空列表**
- **Given**：无 profile。
- **When**：运行 `accounts list`。
- **Then**：exit 0；输出明确指示「无 profile」（不报错）。

**AC-03-2 — 多 profile 状态正确**
- **Given**：三个 profile：alice（active + logged-in）、bob（logged-in）、charlie（logged-out）。
- **When**：运行 `accounts list`。
- **Then**：exit 0；输出包含全部三个；每行能区分 active 标记、logged-in、logged-out 三种状态。

**AC-03-3 — 输出包含派生 ID 与 email**
- **Given**：profile `alice_example_com`（email `alice@example.com`）。
- **When**：运行 `accounts list`。
- **Then**：该行同时显示 ID（`alice_example_com`）与 email（`alice@example.com`）——用户能据此知道 `use`/`remove` 该传哪个 ID。

### US-04 — 删除账号

**AC-04-1 — 删除已存在 profile（确认）**
- **Given**：profile `bob_example_com` 存在。
- **When**：运行 `accounts remove bob_example_com` 并在确认提示选「yes」。
- **Then**：exit 0；`accounts list` 不再显示 `bob_example_com`；运行 `accounts use bob_example_com` 失败（AC-02-2 行为）。

**AC-04-2 — 删除非 active profile 不影响 active**
- **Given**：active 为 `alice_example_com`；另存在 `bob_example_com`。
- **When**：`accounts remove bob_example_com` 并确认。
- **Then**：`accounts list` 显示 active 仍为 `alice_example_com`。

**AC-04-3 — 删除 active profile 清空 active**
- **Given**：active 为 `alice_example_com`。
- **When**：`accounts remove alice_example_com` 并确认。
- **Then**：exit 0；`accounts list` 显示无 active（或显示 active 为空）；任何需要 active 的后续命令（如 `auth logout` 无参数）会以「无 active profile」错误退出。

**AC-04-4 — 拒绝确认则不删除**
- **Given**：profile `alice_example_com` 存在。
- **When**：`accounts remove alice_example_com` 并在确认提示选「no」。
- **Then**：exit 0；`accounts list` 仍显示该 profile，状态不变。

**AC-04-5 — 删除不存在的 profile 报错**
- **Given**：profile `ghost_example_com` 不存在。
- **When**：`accounts remove ghost_example_com`。
- **Then**：exit 非零；stderr 提示「profile 不存在」；不弹出确认提示（快速失败）。

**AC-04-6 — 删除后凭据不可用**
- **Given**：profile `bob_example_com` 已通过 `accounts remove` 删除。
- **When**：用相同 email 再次 `auth login`。
- **Then**：视为全新 profile（AC-01-3 行为：若此时无其他 profile，自动成为 active；若有，则非 active），即旧凭据确实被清除而非复用。

### US-05 — 登出

**AC-05-1 — logout 默认作用于 active**
- **Given**：active 为 `alice_example_com`（logged-in）。
- **When**：运行 `auth logout`（无参数）。
- **Then**：exit 0；`accounts list` 显示 `alice_example_com` 仍存在但状态为 logged-out；active 指针未变（仍是 `alice_example_com`）。

**AC-05-2 — logout 显式指定 profile**
- **Given**：profiles `alice_example_com`（active, logged-in）与 `bob_example_com`（logged-in）。
- **When**：运行 `auth logout bob_example_com`。
- **Then**：exit 0；`accounts list` 显示 `bob_example_com` 为 logged-out；`alice_example_com` 仍 logged-in 且仍 active。

**AC-05-3 — logout 不存在的 profile 报错**
- **Given**：profile `ghost_example_com` 不存在。
- **When**：运行 `auth logout ghost_example_com`。
- **Then**：exit 非零；stderr 提示「profile 不存在」。

**AC-05-4 — logout 无 active 且未指定 profile 报错**
- **Given**：无 active profile（如 active profile 被 remove 后）。
- **When**：运行 `auth logout`（无参数）。
- **Then**：exit 非零；stderr 提示「无 active profile」或等义。

**AC-05-5 — logout 已 logged-out profile 的行为**
- **Given**：profile `alice_example_com` 处于 logged-out。
- **When**：运行 `auth logout alice_example_com`。
- **Then**：exit 0（幂等）；状态仍为 logged-out。不报错（避免幂等操作失败困扰用户）。

**AC-05-6 — logout 后 profile 元数据保留**
- **Given**：profile `alice_example_com`（含 email=`alice@example.com`, name 等元数据）。
- **When**：运行 `auth logout alice_example_com`。
- **Then**：`accounts list` 仍显示该 profile，email/name 字段不变。

### US-06 — 2FA 登录

**AC-06-1 — 2FA 提示与重试**
- **Given**：email/password 正确，但 Apple 要求 2FA。
- **When**：运行 `auth login`，输入 email、password。
- **Then**：命令在 password 提交后**主动提示**输入 2FA 验证码；输入正确 code 后，命令 exit 0 且 `accounts list` 显示对应 profile 为 logged-in。

**AC-06-2 — 2FA 错误码失败**
- **Given**：email/password 正确，Apple 要求 2FA。
- **When**：运行 `auth login`，输入 email、password、**错误**的 2FA code。
- **Then**：exit 非零；stderr 提示登录失败（含 Apple 返回的错误信息）；**不**创建/更新 profile（`accounts list` 不出现新条目，已有同 ID profile 的状态不变）。

**AC-06-3 — 无 2FA 直接成功**
- **Given**：email/password 正确，Apple 不要求 2FA（理论场景，ipatool 决定）。
- **When**：运行 `auth login`，输入 email、password。
- **Then**：命令**不**提示 2FA；直接 exit 0；`accounts list` 显示 logged-in。

### US-07 — 错误可读性

**AC-07-1 — 错误密码不创建 profile**
- **Given**：email 有效但 password 错误。
- **When**：运行 `auth login`，输入该 email 与错误 password。
- **Then**：exit 非零；stderr 包含 Apple 返回的认证失败信息；`accounts list` 不出现新 profile（已存在的同 ID profile 状态不变）。

**AC-07-2 — Ctrl-C 中止不产生副作用**
- **Given**：运行 `auth login`。
- **When**：在任意提示阶段按 Ctrl-C。
- **Then**：exit 非零（标准信号）；`accounts list` 不出现新 profile；已有 profile 状态不变。

**AC-07-3 — 所有错误附下一步建议**
- **Given**：任何命令失败。
- **When**：捕获到错误。
- **Then**：stderr 在错误描述后附一句简短「下一步建议」（如 profile 不存在 → 「运行 `accounts list` 查看可用 profile」；logged-out → 「运行 `auth login` 重新登录」）。

---

## 7. Non-functional Requirements

| ID | 维度 | 度量 |
|----|------|------|
| **NFR-01** | Performance（本地命令）| `accounts list` / `use` / `remove` 在 profile 数 ≤ 10 时 < 500ms 完成（纯本地文件 + keychain 读，无网络）。|
| **NFR-02** | Performance（login）| `auth login` 端到端延迟主要由 Apple API 响应决定；CLI 自身开销（提示、keychain 写、config 写）< 1s。|
| **NFR-03** | Reliability（幂等）| `auth logout` 对已 logged-out profile 幂等（AC-05-5）；`accounts remove` 对已删除 profile 不幂等——第二次以「不存在」错误退出（AC-04-5）。|
| **NFR-04** | Reliability（级联）| `accounts remove` 必须级联清理 keychain namespace + cookie jar + 元数据；任一级联步骤失败 → 命令 exit 非零并报告失败部分（不静默部分成功）。|
| **NFR-05** | Security | Apple ID password **绝不**写盘（不进 config 文件、不进 keychain、不进日志）。只有 ipatool 返回的 account token JSON 进 keychain。 |
| **NFR-06** | Privacy | Profile 元数据（含 email）明文存于 `~/.ipa-manager/config.json`——这是可接受的（email 对本机用户已可见）。无遥测、无上报。|
| **NFR-07** | Usability | 所有交互提示用 `huh`（统一 TUI 风格）。所有错误信息人可读 + 含下一步建议（AC-07-3）。退出码：0 = 成功，非零 = 失败。|
| **NFR-08** | Compatibility | 仅支持 macOS（依赖 Keychain）。Go ≥ 1.26（依赖 go-ios v1.2.0 要求）。|
| **NFR-09** | Observability | `auth login` 在关键阶段输出进度（如「正在联系 Apple...」「2FA 已发送」「登录成功」）；不输出 password、不输出 token 内容。|
| **NFR-10** | Maintainability | 维持 ADR 0002 的 `ProfileKeychain` 编译期接口断言；不修改 ipatool 源码；新增逻辑放 `internal/`，第三方类型不泄漏到 CLI 层。|

---

## 8. Known Risks & Limitations

| ID | 风险 / 限制 | 影响 | 缓解 |
|----|------------|------|------|
| **R1** | ipatool 依赖 Apple 私有 API；Apple 改服务端会临时失效 | `auth login` 全部失败 | 项目级风险（AGENTS.md 已记录）；等 ipatool 跟进，通常数周内修复。|
| **R2** | Apple ID 自动化登录有理论风控风险 | 账号可能被 Apple 标记 | 个人小流量使用风险低（AGENTS.md 已记录）；文档建议只用可接受风险的账号。|
| **R3** | 同 profile 并发登录（多终端窗口同时操作同一 ID）| keychain/cookie jar 写竞争，状态损坏 | **v1 不做互斥**，作为已知限制；文档建议用户避免。|
| **R4** | Profile ID 派生冲突（不同 email → 同 ID）| 第二次 login 刷新第一个 profile 而非新建 | §4.1 已限制为 lowercase + 非 `[a-z0-9_-]` → `_`；接受此限制，文档明示。|
| **R5** | macOS Keychain 被锁 / 权限拒绝 | `auth login` 失败 | 错误透传 + 可读提示；不做自动解锁。|
| **R6** | 2FA code 输错 | login 失败 | AC-06-2：直接失败退出；用户重新运行 `auth login`。v1 不做交互式 retry 循环（保持简单）。|

---

## 9. Key Domain Concepts

| 概念 | 定义 |
|------|------|
| **Profile** | 一个 Apple 账号的命名配置。字段：`ID`（派生 slug）、`Name`（人读标签）、`Email`（Apple ID）、`StoreFront`（登录后由 ipatool 填充）。持久化于 `~/.ipa-manager/config.json`。|
| **Profile ID** | email 经 §4.1 算法派生的稳定 slug。用作 keychain namespace、cookie jar 路径组件、CLI 参数。同一 email 多次 login 派生相同 ID。|
| **Active profile** | `config.ActiveProfileID` 指向的 profile。独立于「logged-in」状态。后续 mission 的所有账号相关操作都以 active profile 为隐式目标。|
| **Profile credentials** | ipatool 登录成功后写入 keychain 的 account token JSON，命名空间为 `profiles/<id>/account`（通过 `ProfileKeychain`）。存在 = logged-in，不存在 = logged-out。|
| **Profile isolation** | 每个 profile 拥有独立的 keychain namespace + 独立 cookie jar（ADR 0002），保证多账号互不污染。本 mission 不改此机制，只在其上构建生命周期命令。|
| **Logged-in / Logged-out** | 见 §4.3 状态模型。|

---

## 10. Success Criteria

| ID | 度量（用户视角，技术无关）|
|----|--------------------------|
| **S1** | 用户能在 < 5 分钟内完成「登录 2 个账号 + 在它们之间切换 1 次」的端到端流程，无需查阅文档中除命令名以外的任何内部细节（profile ID、keychain 路径、config 格式等）。|
| **S2** | `accounts remove` 后，通过任何 ipa-manager 命令都无法再访问到该 profile 的任何痕迹（不出现在 `list`，`use` 拒绝，`auth logout` 拒绝）。|
| **S3** | `auth logout` 后再 `auth login` 同 email，profile 元数据（name 等）保留不变，凭据刷新成功。|
| **S4** | 三个本地命令（`list` / `use` / `remove`）体感瞬时（< 500ms）。|
| **S5** | 所有失败路径返回非零退出码 + 人可读错误 + 下一步建议（覆盖 AC-07-3）。|

---

## 11. Clarification Notes（决策溯源）

| 问题 | 用户决策 | 影响 |
|------|---------|------|
| Q1: `auth login` vs `accounts add` | login = add + login + refresh；`accounts add` 移除 | 命令树简化（§4.2）；刷新流程统一为再次 login |
| Q2: 切换到无凭据 profile | A — 严格拒绝 | AC-02-3 |
| Q3: 切换到 logged-out profile | 与 Q2 合并（同状态）| AC-02-3 覆盖；刷新走 `auth login` |
| Q4: profile ID 派生 | A — email 派生；含 domain；`alice@example.com` → `alice_example_com` | §4.1 算法 |
| Q5: logout vs remove 边界 | 接受默认（logout 保留元数据 + active；remove 全删 + 清 active）| AC-04-3, AC-05-1 |
| Q6: 范围 | 不调整 | §3 |
| Logout 目标 | A — 默认 active，可显式覆盖 | AC-05-1, AC-05-2 |

---

## 12. Sufficiency Check（design 阶段能否不猜谜地推进？）

- [x] **意图清晰**：多账号添加/切换闭环，§1 已述。
- [x] **歧义已解**：所有 7 个高影响问题已闭环（§11）。
- [x] **每个 US 至少 1 个 AC**：US-01→4 AC，US-02→4 AC，US-03→3 AC，US-04→6 AC，US-05→6 AC，US-06→3 AC，US-07→3 AC。
- [x] **AC 可观察**：全部 Then 子句验证 CLI 输出/退出码/通过其他命令可见的状态，无内部实现耦合。
- [x] **NFR 可度量**：每个 NFR 有具体度量（毫秒、是否写盘、退出码等）。
- [x] **范围明确**：In/Out/Non-goals 三段清晰。
- [x] **依赖完整**：所有外部库版本与用途列出（§2.3）。
- [x] **假设显式**：A1–A5 明示，标记已知限制（§8）。

→ Design 阶段可基于此文档无需再问用户「意图」级问题。
