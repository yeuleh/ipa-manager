# internal/account — 多账号 profile + keychain namespace 隔离

多 Apple 账号的 profile 管理（CRUD + active + 凭据状态）+ **核心设计 ProfileKeychain namespace 隔离**（ADR 0002）。

## ProfileKeychain（核心）

ipatool 的 keychain 抽象固定读写 key `"account"`（`AccountInfo` 读 `keychain.Get("account")`，`Login` 写 `"account"`）——**默认只支持一个活跃账号**。多账号由我们的 wrapper 提供：

```go
type ProfileKeychain struct {
    Base      ipakeychain.Keychain
    ProfileID string
}
// mapKey("account") → "profiles/<ProfileID>/account"
```

每个 profile 实例化独立 AppStore，keychain key namespace 化 + 独立 cookie jar 文件，实现多账号隔离（互不覆盖、互不污染）。

备选方案对比见 `docs/bootstrap/research.md` §1.6（v1 决策：key namespace + per-profile cookie jar）。

## Store 接口

`Store`（CLI 经 `Deps.Store` 注入）：profile CRUD（list/get/add/remove）、active profile（get/set activeID）、凭据状态（`HasCredentials(id)`）。配置存 `<configRoot>/config.json`；凭据存 macOS Keychain（经 `appstore.NewBaseKeychain`）。

## 关键约定

- **Password 明文在 keychain**：ipatool 的 `Account` JSON 含 `Password` 字段（虽存 Keychain），UI 与日志必须屏蔽（NFR-04）。
- **cookie jar 隔离**：多账号不仅隔离 keychain entry，还要隔离 cookie jar 文件（避免登录 cookie 跨账号污染）。
- `ProfileKeychain` 包裹的是 ipatool 的 `pkg/keychain.Keychain` 接口，与底层 keyring 实现无关（`99designs/keyring v1.2.1`）。
