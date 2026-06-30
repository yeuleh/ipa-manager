# Requirements — download-ipa-by-account

## 1. Intent & Context

### Problem

前两个 mission 已交付：
- `multi-account-login-switch`：多账号 profile 管理（login / list / use / remove / logout），69 个测试全绿。
- `fix-ipatool-auth`：通过 fork `yeuleh/ipatool/v2@v2.3.1-fix-auth.5` 修复真实 Apple 登录。

但**下载 IPA 文件的核心功能尚未实现**——`appstore.Search` / `appstore.Download` 返回 `ErrNotImplemented`，`library.IPAStore` 全是 stub，CLI 的 `app search` / `app download` 只打印 "not yet implemented"。

用户需要：按 app 名字搜索 App Store，下载 IPA 到本地，且**按账号隔离存储**（每个 profile 有独立的本地 library）。

### Desired Outcome

用户能够完成完整的"搜索 → 下载 → 管理"闭环：
1. `ipa-manager app search <名字>` → 找到目标 app 的 bundle-id
2. `ipa-manager app download <bundle-id>` → IPA 下载到该账号的隔离 library
3. `ipa-manager library list` → 查看 active profile 已下载的 IPA
4. `ipa-manager library clean` → 清理磁盘空间

且所有命令支持 `--profile <id>` 指定非 active 账号操作，无需先切换。

### iPad / iPhone 说明

ipatool 的 search/lookup 已硬编码 `entity=software,iPadSoftware`，搜索结果**同时包含** iPhone 与 iPad app。现代 app 多为 universal（一个 IPA 通吃两种设备）；少数有独立 iPad 版本的 app 会以不同 bundle-id 作为独立搜索结果出现。Apple 按 bundle-id 服务 IPA，下载到的即是该 bundle-id 对应的完整包。**本次 mission 无需为 iPad 做任何特殊设计。**

## 2. Actors / Assumptions / Dependencies

### Actors

| Actor | Description |
|-------|-------------|
| ipa-manager user | 运行 `app search` / `app download` / `library list` / `library clean`，管理多账号下的 IPA |

### Assumptions

- **A-01**：fork `yeuleh/ipatool/v2@v2.3.1-fix-auth.5` 的 Search / Lookup / Download / Purchase / AccountInfo / ReplicateSinf API 与上游签名一致（已从 module cache 源码实证；但仅 Login 经 live Apple 验证，Search/Download 的 live 行为留待 validate 阶段手动验收）。
- **A-02**：Apple 经 download 端点服务的 IPA 是该 bundle-id 的完整通用包（universal），app thinning / 分片发生在设备安装时而非下载时。
- **A-03**：keychain 中的 Account JSON 含 `Password` 字段（前 mission 已确认 ipatool 固有行为），使 token 过期时可用存储的密码重登录。
- **A-04**：免费 app 的 license 获取（Purchase API）是同步且即时的——调用返回后即可重试 download。
- **A-05**：library 元数据索引按 profile 独立维护，不跨 profile 共享。
- **A-06**：下载的 IPA 在本机磁盘可明文存储（IPA 本身不含账号凭据；iTunesMetadata.plist 含 email，本机用户已可见，可接受）。
- **A-07**：个人小流量使用 Apple ID 自动化下载，风控风险低但非零（继承自 AGENTS.md 已知风险）。

### Dependencies

- **D-01**：`github.com/yeuleh/ipatool/v2@v2.3.1-fix-auth.5` —— 提供 Search / Lookup / Download / Purchase / AccountInfo / ReplicateSinf API。
- **D-02**：mission `multi-account-login-switch` 交付物 —— `account.Store`、`account.ProfileKeychain`、`appstore.NewProfileAppStore`、CLI `Deps` 注入框架、`ui.Prompter` 接口。
- **D-03**：mission `fix-ipatool-auth` 交付物 —— go.mod replace 指向 fork，真实 Apple login 可用。

## 3. Scope

### In Scope

- 扩展 `ProfileAppStore` 接口，暴露 Search / Lookup / Download / Purchase / AccountInfo / ReplicateSinf 方法（经 adapter 隔离 ipatool 类型）。
- 实现 `appstore.Search(query, profile)` 与 `appstore.Download(bundleID, profile, opts)` 业务函数。
- 实现 `library.IPAStore`：per-profile 目录布局 + 元数据索引（bundle-id / version / size / downloaded-at）+ CRUD（Add / List / Remove / Clean）。
- CLI 命令填实：
  - `app search <term>` —— 按名字模糊搜索，表格输出
  - `app download <bundle-id>` —— 下载 IPA 到 per-account library（`app` 命令组子命令，与 `app search` 对称）
  - `library list` —— 列出 active profile 的 IPA
  - `library clean [bundle-id]` —— 清理（全部 / 单 app）
- 全部上述命令支持 `--profile <id>` flag（缺省 active profile）。
- 下载流程编排：AccountInfo → Lookup → Download → ReplicateSinf，含 token 过期重登录 + 免费授权获取。
- 下载幂等性：已存在则跳过（`--force` 覆盖）。
- `--output <path>` / `--external-version-id <id>` / `--force` / `--limit N` flags。
- 非交互模式（stdin 非 TTY）行为定义：确认提示自动拒绝（safe default，F-06）。

### Out of Scope

- `app versions <bundle-id>`（列举历史版本）—— 未来 mission。
- `install push` / `install uninstall` / `install update`（设备侧操作）—— 未来 mission。
- 付费 app 购买与下载（Purchase API 仅支持 price=0）。
- 并发下载 / 下载队列 / 断点续传（ipatool 的 downloadFile 内部有 HTTP Range 支持单次断点，但不做多会话队列）。
- library 跨 profile 聚合视图（`library list --all-profiles`）—— v1 每个 profile 独立查看。
- IPA 完整性校验（签名验证 / checksum 比对）—— 信任 ipatool + Apple 端到端。
- 下载限速 / 代理配置 —— v1 不需要。

### Non-goals

- 不修改 ipatool fork 源码（隔离通过 adapter 依赖注入，ADR 0002）。
- 不自建 Apple API 客户端（复用 ipatool 全部网络层）。
- 不做 library 的自动清理策略（如 LRU 淘汰、容量上限）—— 由用户手动 clean。
- 不把 library 元数据塞进 `config.json`（library 与 config 关注点分离，见 Stage 4 设计）。
- 不为 iPad app 做特殊路径或逻辑。

## 4. User Stories

| ID | Priority | Story | Rationale |
|----|----------|-------|-----------|
| US-01 | P1 | As a user, I want to search the App Store by app name, so that I can discover the bundle-id needed for download. | 无法记住每个 app 的 bundle-id；搜索是下载的前置。 |
| US-02 | P1 | As a user, I want to download an app's IPA by bundle-id, so that I have it locally for later install. | mission 核心；没有下载，后续 install/push 都无 IPA 可用。 |
| US-03 | P1 | As a user, I want downloaded IPAs stored under per-account directories, so that each account's apps are isolated. | mission 名字 "by-account" 的核心约束；多账号场景下避免混淆。 |
| US-04 | P1 | As a user, I want to list downloaded IPAs for a profile, so that I can see what I have before cleaning or installing. | clean 的自然搭档；也是基本可见性。 |
| US-05 | P1 | As a user, I want to clean a profile's library (all or specific app), so that I can reclaim disk space. | 用户明确要求（Q3 新增）；IPA 文件大，必须可清理。 |
| US-06 | P2 | As a user, I want to skip re-downloading an existing IPA unless forced, so that I don't waste bandwidth. | 幂等性；避免重复下载同一版本。 |
| US-07 | P2 | As a user, I want to acquire a free license when required (with prompt), so that download succeeds for apps I haven't "purchased". | 许多免费 app 仍需先获取授权；ipatool 有 `--purchase` 机制。 |
| US-08 | P2 | As a user, I want to specify which profile to use via `--profile <id>` without switching active, so that I can operate on multiple accounts efficiently. | 多账号核心体验；"by-account" 主题的灵活面。 |
| US-09 | P3 | As a user, I want to download a specific app version via `--external-version-id`, so that I can get an older version when needed. | 高级需求；多数用户只需最新版。 |
| US-10 | P3 | As a user, I want to override the output path via `--output`, so that I can save an IPA to a custom location. | 灵活性；默认 library 路径已满足大多数场景。 |
| US-11 | P2 | As a user, I want to keep multiple versions of the same app in my library, so that I can install different versions on different devices. | 多设备场景（旧设备不支持最新版）；文件名含版本天然不冲突。 |

### Priority Rationale

- **P1（US-01~05）**：构成"搜索 → 下载 → 管理"最小可用闭环。缺任一则 mission 不可交付——无搜索则用户不知 bundle-id；无下载则核心功能缺失；无 per-account 隔离则违背 mission 主题；无 list/clean 则磁盘不可管理。
- **P2（US-06~08）**：重要但非首版阻塞。幂等性、授权获取、`--profile` flag 显著提升体验，但即使缺这些，"先 `accounts use` 切换、再下载、重复下载覆盖"也可用。
- **P3（US-09~10）**：高级 / 灵活性。默认路径 + 最新版本满足 90% 场景。

## 5. Acceptance Criteria

> Then 子句验证公开可观测行为（CLI stdout/stderr、exit code、文件系统副作用），不耦合内部实现。

### US-01 — Search（模糊搜索）

- **AC-01-1**：Given 存在已登录的 active profile，When 运行 `ipa-manager app search <term>`，Then 以表格输出搜索结果（列：Name / Bundle-ID / Version / Price），exit 0。
- **AC-01-2**：Given 无 active profile 或 active profile 未登录，When 运行 `app search`，Then 显示错误提示（指向 `auth login` 或 `accounts use`），exit 1。
- **AC-01-3**：Given 搜索返回零结果，When 运行 `app search <term>`，Then 显示 `"no results found for '<term>'"`，exit 0。
- **AC-01-4**：Given `--limit N` flag（N 为正整数），When 运行 `app search <term> --limit N`，Then 结果数 ≤ N。
- **AC-01-5**：Given `--profile <id>` 指向有效且已登录的 profile，When 运行 `app search <term> --profile <id>`，Then 使用该 profile 的 StoreFront（结果反映该账号的国家区域）。验证方法：用两个不同区域的 profile 搜索同一 term，观察结果集/可用性差异。
- **AC-01-6**：Given `--limit` 值为 0、负数或非整数，When 运行 `app search <term> --limit <invalid>`，Then 显示 `"invalid --limit value: must be a positive integer"` 错误，exit 1。

### US-02 — Download（下载 IPA）

- **AC-02-1**：Given 存在已登录的 active profile 且 bundle-id 有效，When 运行 `ipa-manager download <bundle-id>`，Then IPA 下载到默认 library 路径（`<configRoot>/library/<profileID>/` 下），显示含最终路径的成功消息，exit 0。
- **AC-02-2**：Given 下载成功，When 检查 library 目录，Then 对应 IPA 文件存在且大小 > 0。
- **AC-02-3**：Given 无 active profile 或未登录，When 运行 `app download`，Then 显示错误（指向 login/use），exit 1。
- **AC-02-4**：Given bundle-id 在 App Store 不存在，When 运行 `app download <bundle-id>`，Then 显示 `"app not found: <bundle-id>"` 错误，exit 1。
- **AC-02-5**：Given 目标路径已存在同版本 IPA 且未传 `--force`，When 运行 `app download <bundle-id>`，Then 显示 `"already exists: <path> (use --force to overwrite)"`，**不重新下载**，exit 0（幂等）。
- **AC-02-6**：Given 目标已存在且传 `--force`，When 运行 `app download <bundle-id> --force`，Then IPA 被重新下载并覆盖原文件，exit 0。
- **AC-02-7**：Given 下载过程返回 `ErrLicenseRequired` 且 app 价格 = 0，When 运行 `app download`，Then 提示 `"this app requires a free license. acquire? [Y/n]"`；选 yes → 获取授权后继续下载，exit 0；选 no → 取消，exit 0（非错误）。
- **AC-02-8**：Given 下载返回 `ErrLicenseRequired` 且 app 价格 > 0，When 运行 `app download`，Then 显示 `"paid apps are not supported"` 错误，exit 1。
- **AC-02-9**：Given `--profile <id>` flag，When 运行 `app download <bundle-id> --profile <id>`，Then 下载使用该 profile 的账号会话，IPA 存入该 profile 的 library 目录。
- **AC-02-10**：Given active profile 的 password token 已过期，When 运行 `app download`，Then 工具使用 keychain 中存储的凭据透明重登录并重试下载；成功则下载完成 exit 0，重登录失败则显示错误 exit 1。
- **AC-02-11**：Given 非交互模式（stdin 非 TTY）且下载遇到 `ErrLicenseRequired`（免费 app），When 运行 `app download`，Then 不显示提示（无法交互），显示 `"license acquisition requires interactive confirmation; cannot proceed non-interactively"` 错误，exit 1。

### US-03 — Per-account isolation（按账号隔离）

- **AC-03-1**：Given profile A 已下载 app X 的 IPA，When 切换到 profile B 并运行 `library list`，Then profile B 的列表**不含** app X（两个 profile 的 library 完全隔离）。
- **AC-03-2**：Given 两个不同 profile 各自下载同一 app，When 检查两个 profile 的 library 目录，Then 各自目录下均有该 IPA 文件（互不影响，互不引用）。

### US-11 — Multi-version（多版本共存）

- **AC-11-1**：Given 已下载 app X 版本 8.0.34，When App Store 更新到 8.0.35 且运行 `app download <bundle-id>`，Then 8.0.35 被下载，8.0.34 **保留**（不删除），`library list` 显示两个版本各占一行。
- **AC-11-2**：Given 已下载版本 8.0.35，When 再次运行 `app download <bundle-id>`（同版本），Then 跳过（`"already exists"`），exit 0（幂等，不重复下载）。

### US-04 — Library list

- **AC-04-1**：Given active profile 有已下载的 IPA（含同 app 多版本），When 运行 `ipa-manager library list`，Then 以表格输出（列：Bundle-ID / Version / Size / Downloaded-At / PATH），每个 (bundle-id, version) 一行，exit 0。
- **AC-04-2**：Given active profile 的 library 为空，When 运行 `library list`，Then 显示 `"no IPAs in library for profile '<id>'"`，exit 0。
- **AC-04-3**：Given `--profile <id>` flag，When 运行 `library list --profile <id>`，Then 列出该 profile 的 IPA。

### US-05 — Library clean

- **AC-05-1**：Given active profile 的 library 有 N > 0 个 IPA，When 运行 `library clean`（无参数），Then 显示确认提示 `"remove all N IPA(s) for profile '<id>'? [y/N]"`；**若其中 M > 0 个 IPA 位于自定义 `--output` 路径（非默认 library 目录），提示额外逐行列出这些自定义路径**（如 `"  - /Users/x/custom/app.ipa"`），使破坏性范围完全可见；选 yes → 全部删除（含自定义路径文件）+ 成功消息（含删除数量）；选 no → `"cancelled"`，exit 0。
- **AC-05-2**：Given `library clean` 无参数且 library 为空，When 运行 `library clean`，Then 显示 `"library is already empty"`，exit 0（无需确认）。
- **AC-05-3**：Given 指定 bundle-id 的 IPA 存在，When 运行 `library clean <bundle-id>`，Then 显示确认 `"remove '<bundle-id>' (version <v>, <size>)? [y/N]"`；选 yes → 删除该文件 + 成功消息；选 no → cancelled，exit 0。
- **AC-05-4**：Given `library clean <bundle-id>` 但该 bundle-id 无 IPA，When 运行，Then 显示 `"no IPA for '<bundle-id>' in profile '<id>'"`，exit 0（幂等）。
- **AC-05-5**：Given `--profile <id>` flag，When 运行 `library clean --profile <id>`，Then 清理该 profile 的 library（不影响 active profile）。
- **AC-05-6**：Given clean 删除文件后，When 运行 `library list`，Then 被删除的版本不再出现在列表中。
- **AC-05-7**：Given 某 IPA 是通过 `--output <path>` 下载到自定义路径（非默认 library 目录）且仍被 library 索引跟踪，When 运行 `library clean <bundle-id>`，Then 确认提示显示该 IPA 的**完整自定义路径**（如 `"remove '<bundle-id>' at '/Users/x/custom/app.ipa'? [y/N]"`）；选 yes → 删除该路径下的文件；选 no → cancelled，exit 0。
- **AC-05-8**：Given `library clean <bundle-id>` 但该 bundle-id 对应的物理文件已不存在（文件已被外部删除），When 运行，Then 显示 `"file already absent for '<bundle-id>'"`，exit 0（幂等）；再次运行 `library list` 时该版本不再出现。
- **AC-05-10**：Given 某 bundle-id 有多个版本（如 8.0.34 和 8.0.35），When 运行 `library clean <bundle-id>`（无 --version），Then 确认提示列出**全部版本**（如 `"remove all 2 version(s) of '<bundle-id>'? [y/N]"`）；选 yes → 删除全部版本的文件 + 索引；选 no → cancelled。
- **AC-05-11**：Given 某 bundle-id 有多个版本，When 运行 `library clean <bundle-id> --version 8.0.34`，Then 仅确认并删除该指定版本（其他版本保留）；exit 0。
- **AC-05-12**：Given `library clean <bundle-id> --version <v>` 但该版本不存在（其他版本存在），When 运行，Then 显示 `"no IPA for '<bundle-id>' version '<v>'"`，exit 0（幂等）。
- **AC-05-9**：Given 非交互模式（stdin 非 TTY）且 clean **将删除一个或多个现存文件**（即需要确认的破坏性操作），When 运行 `library clean`（无论有无 bundle-id 参数），Then 不显示确认提示，显示 `"confirmation required in non-interactive mode; cannot proceed"` 错误，exit 1（安全默认）。注：no-op 场景（AC-05-2 空库 / AC-05-4 未找到 / AC-05-8 文件已不存在）不受此约束——无文件可删时仍 exit 0。

### US-06 — Skip existing（幂等，已在 AC-02-5 / AC-02-6 覆盖）

> 无独立 AC；行为由 AC-02-5（跳过）与 AC-02-6（`--force` 覆盖）完整规定。

### US-07 — Free license prompt（已在 AC-02-7 / AC-02-8 / AC-02-11 覆盖）

> 无独立 AC；行为由 AC-02-7（交互式免费授权提示）、AC-02-8（付费拒绝）、AC-02-11（非交互模式报错）完整规定。

### US-08 — `--profile` flag（跨命令）

- **AC-08-1**：Given `--profile <id>` 指向不存在的 profile，When 运行任一命令（search / download / library list / library clean）带 `--profile <id>`，Then 显示 `"profile '<id>' not found"`，exit 1。
- **AC-08-2**：Given `--profile <id>` 指向已存在但未登录的 profile，When 运行 `app search` 或 `app download` 带 `--profile <id>`，Then 显示 `"profile '<id>' has no credentials"`，exit 1。
- **AC-08-3**：Given 未传 `--profile` 且存在 active profile，When 运行任一命令，Then 使用 active profile（缺省行为，已被各命令的 AC-XX-1 覆盖）。

### US-09 — Version selection

- **AC-09-1**：Given `--external-version-id <id>` flag 指向有效版本，When 运行 `app download <bundle-id> --external-version-id <id>`，Then 下载该指定版本（非最新），成功消息显示该版本号，且 `library list` 中该条目的 Version 列对应该指定版本，exit 0。
- **AC-09-2**：Given `--external-version-id <id>` 指向无效版本，When 运行，Then 显示 Apple 返回的错误消息，exit 1。

### US-10 — Custom output path

> **`--output` 语义定义**（F-02/F-05 决议）：`--output <path>` 将 IPA 保存到自定义路径，**同时仍记录到该 profile 的 library 索引**（用户 Q3 明确要求）。因此 `library list` 会显示该条目（含自定义路径），`library clean` 可删除它（确认提示显示完整路径，见 AC-05-7）。

- **AC-10-1**：Given `--output <path>` flag，When 运行 `app download <bundle-id> --output <path>`，Then IPA 保存到 `<path>`（而非默认 library 路径），exit 0。
- **AC-10-2**：Given `--output <path>` 下载成功，When 运行 `library list`，Then 该 IPA 出现在列表中（Bundle-ID / Version / Size / Downloaded-At 可见），且文件确实存在于 `<path>`。
- **AC-10-3**：Given `--output <path>` 指向已存在的文件且未传 `--force`，When 运行，Then 显示 `"already exists: <path> (use --force to overwrite)"`，不覆盖，exit 0。
- **AC-10-4**：Given `--output <path>` 的父目录不存在，When 运行，Then 显示 `"output directory does not exist: <dir>"` 错误（含父目录路径），exit 1。
- **AC-10-5**：Given `--output <path>` 指向一个已存在的目录（而非文件路径），When 运行，Then 显示 `"output path is a directory: <path>"` 错误，exit 1。
- **AC-10-6**：Given `--output <path>` 父目录存在但无写权限，When 运行，Then 显示 `"cannot write to output path: <path> (permission denied)"` 错误，exit 1。

## 6. Non-Functional Requirements

| ID | Category | Requirement | Measurement |
|----|----------|-------------|-------------|
| NFR-01 | Reliability — atomic download | 下载先写 `.tmp` 临时文件，成功后原子 rename；中断不留下"半写"的可用 IPA。 | 下载中断后，目标路径要么不存在，要么是完整的上一次版本；绝不出现损坏 IPA。 |
| NFR-02 | Reliability — DRM | 下载后必须调用 `ReplicateSinf` 写入 DRM 解密密钥；未执行此步的 IPA 无法安装。 | 下载成功的 IPA 经 device install 流程可被识别（validate 阶段手动验收）。 |
| NFR-03 | Isolation | 不同 profile 的 library 目录严格隔离；任一命令不得读写非目标 profile 的目录。 | `library list --profile A` 永不显示 profile B 的 IPA；文件系统审计两目录无交叉引用。 |
| NFR-04 | Security — no credential leak | 搜索词、下载日志、library 输出中不得出现 password / passwordToken / directoryServicesID。 | `grep -r` 命令输出无凭据字段；仅 email 可出现在 iTunesMetadata（Apple 固有行为，A-06）。 |
| NFR-05 | Usability — progress | 下载大文件（> 5MB）时显示进度指示（百分比或字节进度条）。 | 交互式终端下可见进度更新；非交互模式（CI）优雅降级（无进度条，不报错）。 |
| NFR-06 | Usability — errors | 所有 Apple 返回的失败（网络 / 授权 / license / 不存在）附人类可读消息 + 下一步建议。 | 错误消息含明确原因 + 建议动作（如 `run auth login` / `verify bundle-id`）。 |
| NFR-07 | Compatibility | 仅支持 macOS（与 v1 一致）；依赖 macOS Keychain。 | `go build` 产出 macOS 二进制；非 macOS 平台编译不保证。 |
| NFR-08 | Maintainability — ipatool isolation | ipatool 类型（App / Account / DownloadInput 等）仅出现在 `internal/appstore/adapter.go`；CLI / library 层只见我们的接口。 | `grep -r "majd/ipatool" internal/cli internal/library` 无结果。 |
| NFR-09 | No regression | 现有全部测试（前两 mission 的 69+ 测试）继续通过。 | `go test ./... -count=1` exit 0。 |
| NFR-10 | Performance — search | 搜索响应以 Apple iTunes API 为上界（通常 < 3s）；本工具额外开销 < 200ms（profile 解析 + keychain 读）。 | 手动计时：`app search` 端到端 < 5s（正常网络）。 |

## 7. Key Domain Concepts

| Concept | Description |
|---------|-------------|
| **Profile** | 命名的 Apple 账号配置（id / name / email / store_front）。本 mission 复用现有 `account.Profile`。 |
| **Active profile** | 当前默认操作的 profile；`--profile <id>` flag 可临时覆盖。 |
| **StoreFront** | Apple 商店区域代码（如 `143441-1,29`），决定搜索/下载的区域内容。来自 profile，不可手动指定。 |
| **Bundle-ID** | App 的唯一标识（如 `com.tencent.xin`）。下载的主键；search 的产出。 |
| **App-ID (trackId)** | Apple 数字 App 标识。与 bundle-id 一一对应；search/lookup 返回，download 可使用任一。 |
| **Library** | 按 profile 隔离的本地 IPA 存储区。物理布局：`<configRoot>/library/<profileID>/`。每个 profile 独立。 |
| **Library index** | per-profile 的元数据索引（记录 bundle-id / app-id / version / size / downloaded-at / 文件路径）。供 `library list` / `clean` / 幂等检查使用。存储机制由 design 决定。 |
| **AccountInfo** | 从 keychain 读取的完整 Account JSON（含 passwordToken / DSID / StoreFront / Password）。Search/Lookup/Download 均需此对象作为 Apple API 参数。 |
| **Lookup** | 按 bundle-id 查询单个 app 元数据的 Apple iTunes API（比 Search 轻，返回精确的 App 对象）。Download 编排必经。 |
| **License (Purchase)** | Apple 要求账号先"获取授权"才能下载（即使免费）。ipatool 的 Purchase API 仅支持 price=0；付费 app 不支持。 |
| **Sinf / ReplicateSinf** | Apple 的 DRM 解密密钥片段。下载的原始 IPA 不含 sinf，必须调用 `ReplicateSinf` 写入后才能在设备上安装。 |
| **External Version ID** | Apple 的版本标识符（非人类可读的 version string）。用于下载指定版本；缺省下载最新。 |

## 8. Success Criteria

1. **搜索可用**：用户能按名字搜到目标 app 并获得 bundle-id（live Apple 验证）。
2. **下载可用**：用户能下载 IPA 到 per-account library，下载后的 IPA 文件完整且可被设备识别（手动 install 验证）。
3. **隔离正确**：两个 profile 的 library 互不干扰（AC-03-1 / AC-03-2 通过）。
4. **管理可用**：用户能列出和清理已下载的 IPA（含单 app 与全部两种粒度）。
5. **多账号灵活**：`--profile <id>` flag 在所有命令上工作，无需切换 active 即可操作其他账号。
6. **无回归**：现有 69+ 测试全绿。
7. **代码隔离**：ipatool 类型不泄露到 CLI / library 层（NFR-08）。

## 9. Clarification Notes

- **所有高影响歧义已解决**（Q1~Q7 全部用户确认）。无 NEEDS CLARIFICATION 项。
- **Spike 不需要**：ipatool 的 Search / Lookup / Download / Purchase / AccountInfo / ReplicateSinf API 签名已从 module cache 源码实证（`pkg/appstore/*.go` + `cmd/download.go` + `cmd/search.go`）。唯一未验证的是 live Apple 端到端下载行为——这是 validate 阶段的手动验收项，不是 requirements 的阻塞。
- **iPad 处理**：无需特殊设计（§1 已说明）。ipatool search 硬编码 `entity=software,iPadSoftware`，覆盖两种设备类型。
- **library 元数据索引存储机制**：requirements 只规定"需维护 per-profile 元数据索引"，具体用 JSON 文件 / SQLite / 嵌入文件名解析，留给 design 决策。
- **授权获取方式**（F-01 决议）：本 mission 采用**交互式提示**（AC-02-7）获取免费授权，不提供显式 `--purchase` flag。非交互模式下 license 提示无法交互，直接报错 exit 1（AC-02-11）。若未来需要自动化场景，可在后续 mission 加 `--yes` flag 跳过提示。
- **命令结构决议**（用户确认）：`app` 命令组归集所有 App Store 操作（`app search` / `app download` / `app versions`），保持 noun+verb 一致性。理由：search（发现）+ download（获取）是同一概念域的两步操作，归组使心理模型连贯；与 `library list/clean`、`accounts list/use/remove` 等组结构一致。`install` 组净化为纯设备操作（`push` / `uninstall` / `update`，未来可改名为 `device`）。重构后命令树：
  ```
  app search / download / versions   ← App Store 域（发现 + 获取 + 版本查询）
  library list / clean               ← 本地文件管理域
  install push / uninstall / update  ← 设备操作域（未来改 device）
  ```
- **password token 过期重登录**（AC-02-10）：依赖 keychain 中存储的 Password（A-03）。若用户登出后 Password 被清除，重登录会失败——此时显示错误，不静默失败。
