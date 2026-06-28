# ADR 0002 — 多账号凭据隔离架构

- 状态：Accepted
- 日期：2026-06-28
- 决策者：Leon
- 相关：ADR 0001（技术栈）

## 背景 (Context)

ipa-manager 的核心价值是"多 Apple 账号切换 + 按账号隔离"。但 ipatool 的 `AppStore` 实现把账号凭据**固定**写入 keychain key `"account"`，并把登录 cookie 存在单一 `persistent-cookiejar` 文件里、由所有 HTTP client 共享。直接复用默认行为意味着：登录第二个账号会**覆盖**第一个的凭据与 session。

因此需要一个隔离层，让每个 profile 拥有独立的 keychain 状态与 cookie jar，且对 ipatool 的 `AppStore` 透明（不修改 ipatool 源码）。

## 决策 (Decision)

采用 **key namespace 包装 + 每 profile 独立 cookie jar**：

1. `account.ProfileKeychain` 实现 ipatool 的 `keychain.Keychain` 接口，把固定 key `"account"` 映射为 `profiles/<profileID>/account`。通过 `appstore.NewAppStore(appstore.Args{Keychain: ProfileKeychain{...}})` 注入，对 ipatool 完全透明。
2. 每个 profile 拥有独立 cookie jar 文件（`account.CookieJarPath` → `<configRoot>/profiles/<id>/cookies`），构造 AppStore 时一并注入。
3. `appstore.NewProfileAppStore(profile)` 作为单一构造入口，同时注入上述两者。
4. 删除 profile 时必须同步 revoke keychain namespace + 删除 cookie jar（一致性）。

接口契约由编译期断言锁定：`var _ ipakeychain.Keychain = ProfileKeychain{}`（已在 `internal/account/keychain.go` 中，build 验证通过）。

## 结果 (Consequences)

- **正向**：多账号共存且互不污染；不修改 ipatool 源码（库升级安全）；隔离逻辑集中可测（`keychain_test.go` 已验证 alice/bob 不交叉）。
- **负向**：profile 元数据须与 keychain 数据保持一致（删除要级联）；同 profile 并发登录可能竞争写 keychain/cookie jar。
- **缓和**：v1 用文件锁或简单拒绝同 profile 并发登录；删除 profile 走统一入口确保级联。

## 备选方案 (Alternatives Considered)

1. **每 profile 一个 keyring ServiceName**（`ipa-manager.auth.<id>`）：ipatool 仍写 `"account"` 但落在不同 Keychain service。被否——Keychain 项增多、迁移/清理复杂，且不如 namespace 包装直观可测。
2. **直接复用默认 `"account"` key**：被否——**不支持多账号**，会互相覆盖，直接违背项目核心目标。
3. **fork ipatool 加 profile 支持**：被否——放弃库升级红利（Apple API 跟进靠上游），维护成本高。
