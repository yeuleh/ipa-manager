# Requirements — ios-device-manage

## 1. Intent & Context

### Problem

前三个 mission 已交付 ipa-manager 的账号侧与 IPA 获取侧：
- `multi-account-login-switch`：多账号 profile 管理（login / list / use / remove / logout）。
- `fix-ipatool-auth`：fork `yeuleh/ipatool/v2@v2.3.1-fix-auth.5` 修复真实 Apple 登录。
- `download-ipa-by-account`：搜索 / 下载 IPA、按账号隔离 library、library list / clean。

但**设备侧（连接 iOS 设备 + 推送安装 + 设备应用管理）尚未实现**——`internal/device/` 全是 stub（`ListConnectedDevices` / `ListInstalledApps` / `Install` / `Uninstall` 返回 `ErrNotImplemented`），CLI 的 `devices` 命令与 `install push/uninstall/update` 组只打印 "not yet implemented"。`download-ipa-by-account` 的 Out of Scope 明确把 `install push / uninstall / update` 推迟到"未来 mission"——即本 mission。

用户需要：把已下载到 library 的 IPA 推到 iOS 设备安装，并能列举/卸载设备上的 app，形成 **App Store → library → 设备** 的完整闭环。且 `install` 应自给自足——library 缺该 IPA 时自动下载再推送，无需用户手动两步操作。

### Desired Outcome

用户能够完成完整的"搜索 → 下载 → 安装 → 管理"闭环：

1. `ipa-manager device list` → 看到已连接的设备（UDID / 名称 / iOS 版本）
2. `ipa-manager device install <bundle-id>` → 该 profile library 有 IPA 就推、没有就自动下载再推（自给自足）
3. `ipa-manager device apps` → 看到设备上已安装的 user app
4. `ipa-manager device uninstall <bundle-id>` → 卸载（带二次确认）
5. 多设备 / 多账号场景通过 `--udid` / `--profile` flag 灵活指定，无需先切换

且 iOS 17+ 设备在缺 tunnel 时给出**可操作的错误**（提示运行 `sudo ios tunnel start`），绝不静默失败、绝不自动 sudo 提权。

### 关键设计决议（来自需求讨论）

- **命令树统一为 `device` 组**：砍掉顶层 `devices` 单命令 + `install push/uninstall/update` 组（均为 stub，重构零风险），归并为 noun+verb 一致的 `device list/apps/install/uninstall`，与 `app search/download`、`library list/clean`、`accounts list/use` 模式对齐。
- **砍掉 `device update`**：install 足够智能（library 有→推 / 无→自动下载→推），`update` 语义（拿最新版）收编进 `--latest` flag。一个命令覆盖 fresh-install 与 update 两个用例。
- **install 的 IPA 来源 = library by bundle-id**（Q2=A）：library 有则用、没有则自动下载。不支持任意文件路径（留给未来）。
- **多设备 = 单命令操作单台设备**：`--udid <id>` flag + 缺省单台自动选中 / 多台交互选择 / 0 台报错；批量 `--all` 留给未来。

## 2. Actors / Assumptions / Dependencies

### Actors

| Actor | Description |
|-------|-------------|
| ipa-manager user | 运行 `device list/apps/install/uninstall`，把 IPA 推到设备并管理设备上的 app |

### Assumptions

- **A-01**：`go-ios v1.2.0` 的设备 API（`ios.ListDevices` / `ios.GetDevice` / `zipconduit.New().SendFile` / `installationproxy.New().BrowseUserApps/Uninstall`）签名与 `research.md` §2 一致（已从源码实证）。live 设备端到端行为留待 validate 阶段手动验收。
- **A-02**：设备已与 Mac 配对并信任（trust 对话框由 macOS Finder 一次性处理）；本工具不做 pairing/trust。
- **A-03**：iOS 17+ 设备的服务操作（install / apps / uninstall）需先 `sudo ios tunnel start`；`ListDevices` 经 usbmuxd 仍可列举。本工具**绝不自动 sudo**，检测到缺 tunnel 时返回 `ErrTunnelRequired`（已定义于 `internal/device/errors.go`）。
- **A-04**：install 在 library 缺 IPA 时复用 `download-ipa-by-account` 的完整下载编排（AccountInfo → Lookup → Download → ReplicateSinf），含 token 过期重登录与免费授权获取。
- **A-05**：library 中的 IPA 已 DRM 完整（ReplicateSinf 已在下载时写入），install 时无需再做 DRM 步骤，直接推送即可被设备识别。
- **A-06**：go-ios zipconduit 在设备安装时处理 universal/thinning；library 中存的就是该 bundle-id 的完整通用包。
- **A-07**：个人小流量使用 Apple ID 自动化下载，风控风险低但非零（继承自 AGENTS.md 已知风险）。

### Dependencies

- **D-01**：`github.com/danielpaulus/go-ios v1.2.0` —— 设备列举 / 安装 / 卸载 / 列举 app。
- **D-02**：mission `multi-account-login-switch` 交付物 —— `account.Store`、`account.Profile`、`account.ProfileKeychain`、CLI `Deps` 注入框架、`ui.Prompter` 接口。
- **D-03**：mission `download-ipa-by-account` 交付物 —— `appstore` 下载编排（Search / Lookup / Download / Purchase / AccountInfo / ReplicateSinf）、`library.Store`（per-profile IPA library + 元数据索引 + 多版本支持）。
- **D-04**：mission `fix-ipatool-auth` 交付物 —— go.mod replace 指向 fork，真实 Apple login 可用。

## 3. Scope

### In Scope

- 实现 `internal/device` 包：`ListConnectedDevices` / `ListInstalledApps` / `Install` / `Uninstall`（替换 stub）。
- CLI 命令树重构：统一 `device` 命令组（`device list` / `device apps` / `device install` / `device uninstall`），移除旧 `devices` 单命令与 `install push/uninstall/update` 组。
- **install 编排**：library 有 → 推；library 无 → 自动下载（复用 download 流程）→ 推；`--latest` → 强制刷新 App Store 最新版再推。
- **设备选择**：`--udid <id>` flag；缺省单台自动选中、多台交互提示（huh）选择、0 台报错。
- **账号选择**：`--profile <id>` flag（缺省 active profile），与 download/library 命令一致。
- **iOS 17+ tunnel 检测**：服务操作缺 tunnel 时返回 `ErrTunnelRequired` + 可操作消息；list 经 usbmuxd 仍可用。
- **uninstall 二次确认**：交互式 `[y/N]`；非交互模式（非 TTY）拒绝（safe default）。
- `device apps` 仅列举 user-installed app（不含系统 app）。

### Out of Scope

- **批量装多设备**（`--all`）—— 未来增强。
- **install 自任意文件路径**（Q2=A：仅 library by bundle-id）—— 未来增强。
- **设备配对 / 信任**（A-02：假设已配对）。
- **`device update` 独立命令**（已合并进 `install --latest`）。
- **`doctor` 命令的 tunnel 健康检查**（doctor 是独立关注点；本 mission 只做运行时 tunnel 检测，不增强 doctor）。
- **设备 app 数据备份 / 恢复**、**屏幕镜像 / 截图**、**文件系统浏览**（go-ios 支持但超出个人工具范围）。
- **无线 / Wi-Fi 设备同步配置**。
- **iOS 版本与 app 兼容性预检**（App Store / 设备端自行处理）。

### Non-goals

- **绝不自动 sudo 提权**（安全红线；tunnel 由用户手动启动）。
- **不绕过 Apple 的 trust / pairing 机制**。
- **不修改 go-ios 源码**（代码级 import，adapter 隔离类型）。
- **不做 install 后的设备端完整性校验**（信任 go-ios zipconduit）。

## 4. User Stories

| ID | Priority | Story | Rationale |
|----|----------|-------|-----------|
| US-01 | P1 | As a user, I want to list connected iOS devices, so that I see what's available and get the UDID for `--udid`. | 一切设备操作的前提；无可见性则无法选择目标设备。 |
| US-02 | P1 | As a user, I want to install an app to a device from my library by bundle-id, so that I can put IPAs onto devices. | mission 核心；闭环"library → 设备"的关键一步。 |
| US-03 | P1 | As a user, I want install to auto-download the IPA when my library lacks it, so that I don't have to manually run `app download` first. | 自给自足是核心体验；用户在需求讨论中明确要求。 |
| US-04 | P1 | As a user, I want to uninstall an app from a device, so that I can remove apps I no longer want. | 设备管理的基本能力。 |
| US-05 | P1 | As a user, I want to list apps installed on a device, so that I can verify installs and find bundle-ids to uninstall. | install 的可见性搭档；也是发现可卸载 app 的入口。 |
| US-06 | P1 | As a user, I want to select which device to operate on when multiple are connected, so that I target the right device. | 多设备场景常见（家庭多台 iPhone/iPad）；必须可控。 |
| US-07 | P1 | As a user with an iOS 17+ device, I want a clear error telling me to start the tunnel, so that I can fix it myself rather than hitting a silent failure. | iOS 17+ 设备普及；silent failure 不可接受；安全红线决定不自动 sudo。 |
| US-08 | P2 | As a user, I want to refresh to the App Store's latest version before pushing via `--latest`, so that I can update an already-installed app. | "update" 用例；合并进 install 的 flag，覆盖拿新版场景。 |
| US-09 | P2 | As a user, I want to specify which profile's library/credentials to use via `--profile <id>`, so that I can install from a non-active account without switching. | 多账号一致性体验；与 download/library 命令对齐。 |
| US-10 | P3 | As a user, I want to install a specific version via `--version <v>` when my library holds multiple versions, so that I can put an older version on a legacy device. | 多版本库场景；少数派需求（旧设备不支持最新版）。 |

### Priority Rationale

- **P1（US-01~07）**：构成"列设备 → 安装 → 管理设备 app"最小可用闭环 + iOS 17+ 正确性。缺任一则 mission 不可交付——无 list 则无可见性；无 install 则核心缺失；无 auto-download 则不自给自足；无 uninstall/apps 则设备不可管理；无多设备选择则多设备场景误操作；无 tunnel 处理则在主流 iOS 17+ 设备上静默失败。
- **P2（US-08~09）**：重要增强。`--latest` 提供 update 能力但 install auto-download 已覆盖新 app 的鲜度；`--profile` 是一致性 flag，缺它则多账号需先 `accounts use` 切换。两者缺失时"先切换、用现有版本"也可用。
- **P3（US-10）**：niche 多版本选择；多数用户只装最新版。

## 5. Acceptance Criteria

> Then 子句验证公开可观测行为（CLI stdout/stderr、exit code、设备上 app 增删、library 文件系统副作用），不耦合内部实现。

### US-01 — device list

- **AC-01-1**：Given 1 台或多台设备已连接，When 运行 `ipa-manager device list`，Then 以表格输出（列：UDID / Name / iOS Version / Connection Type），exit 0。
- **AC-01-2**：Given 0 台设备连接，When 运行 `device list`，Then 显示 `"no connected device"`，exit 0（list 命令空结果非错误，与 `library list` 空库一致）。
- **AC-01-3**：Given 多台设备连接，When 运行 `device list`，Then 全部列出，每台含完整 UDID（可供 `--udid` 复制使用）。

### US-02 — install（library 有 IPA 时推送）

- **AC-02-1**：Given active profile library 含 bundle-id 的 IPA + 恰好 1 台设备连接，When 运行 `ipa-manager device install <bundle-id>`，Then IPA 推送到设备，显示成功消息（含设备名 + app 版本），exit 0。
- **AC-02-2**：Given install 成功，When 运行 `device apps`，Then 该 app 出现在设备已装列表中（Bundle-ID / Name / Version 可见）。
- **AC-02-3**：Given 0 台设备连接，When 运行 `device install <bundle-id>`，Then 显示 `"no connected device; connect a device and trust this Mac"` 错误，exit 1。
- **AC-02-4**：Given `--udid <id>` 指向已连接设备，When 运行 `device install <bundle-id> --udid <id>`，Then 推送到该指定设备，exit 0。
- **AC-02-5**：Given `--udid <id>` 指向未连接设备，When 运行 `device install <bundle-id> --udid <id>`，Then 显示 `"device '<id>' not connected"` 错误，exit 1。
- **AC-02-6**：Given 多台设备连接且未传 `--udid`，When 运行 `device install <bundle-id>`，Then 交互提示选择设备（显示 UDID/Name 选项）；选某台 → 推送该台 exit 0；选取消 → `"cancelled"` exit 0。
- **AC-02-7**：Given 目标设备未信任 / 未配对（go-ios 返回 trust 错误），When 运行 `device install`，Then 显示 go-ios 错误消息 + 提示在设备上信任此 Mac，exit 1。
- **AC-02-8**：Given active profile 不存在或未设置，When 运行 `device install <bundle-id>`，Then 显示错误（指向 `auth login` 或 `accounts use`），exit 1。

### US-03 — install 自动下载（library 缺 IPA 时）

- **AC-03-1**：Given bundle-id 不在 active profile library 但存在于 App Store，When 运行 `device install <bundle-id>`，Then 自动下载到该 profile library 后推送，成功消息同时体现下载与安装两步，exit 0。
- **AC-03-2**：Given 自动下载被触发且完成，When 运行 `library list`，Then 该 IPA 出现在该 profile 的 library 列表中（即 install 副作用丰富了 library）。
- **AC-03-3**：Given 自动下载被触发但 active profile 未登录，When 运行 `device install`，Then 复用 download 流程返回 `"profile '<id>' has no credentials"` 错误，exit 1。
- **AC-03-4**：Given 自动下载被触发但 bundle-id 在 App Store 不存在，When 运行 `device install`，Then 显示 `"app not found: <bundle-id>"` 错误，exit 1。
- **AC-03-5**：Given 自动下载遇到免费 app 需授权（ErrLicenseRequired, price=0），When 交互模式运行，Then 复用 download 的授权提示（`acquire? [Y/n]`）；选 yes → 获取授权后继续下载+安装 exit 0；选 no → 取消 exit 0。
- **AC-03-6**：Given 自动下载需授权但处于非交互模式（非 TTY），When 运行 `device install`，Then 显示 `"license acquisition requires interactive confirmation; cannot proceed non-interactively"` 错误，exit 1。

### US-04 — uninstall

- **AC-04-1**：Given 设备上已装某 app（1 台设备），When 运行 `device uninstall <bundle-id>`，Then 显示确认 `"uninstall '<bundle-id>' from device '<name>'? [y/N]"`；选 yes → 卸载 + 成功消息 exit 0；选 no → `"cancelled"` exit 0。
- **AC-04-2**：Given uninstall 成功，When 运行 `device apps`，Then 该 app 不再出现在设备已装列表中。
- **AC-04-3**：Given 设备上未装该 bundle-id，When 运行 `device uninstall <bundle-id>`，Then 显示 `"app '<bundle-id>' not installed on device"` 错误，exit 1。
- **AC-04-4**：Given 非交互模式（stdin 非 TTY），When 运行 `device uninstall <bundle-id>`，Then 不显示确认提示，显示 `"confirmation required in non-interactive mode; cannot proceed"` 错误，exit 1（安全默认）。
- **AC-04-5**：Given `--udid <id>` 指定设备，When 运行 `device uninstall <bundle-id> --udid <id>`，Then 卸载该指定设备上的 app。
- **AC-04-6**：Given 0 台设备连接，When 运行 `device uninstall <bundle-id>`，Then 显示 `"no connected device..."` 错误，exit 1。

### US-05 — device apps

- **AC-05-1**：Given 设备上有 user-installed app（1 台设备），When 运行 `device apps`，Then 以表格输出（列：Bundle-ID / Name / Version），仅含 user app（不含系统 app），exit 0。
- **AC-05-2**：Given 设备上无任何 user app，When 运行 `device apps`，Then 显示 `"no user apps installed on device '<name>'"`，exit 0。
- **AC-05-3**：Given 多台设备连接且未传 `--udid`，When 运行 `device apps`，Then 交互提示选择设备（同 AC-02-6 模式）。
- **AC-05-4**：Given `--udid <id>` 指定设备，When 运行 `device apps --udid <id>`，Then 列举该设备的 user app。

### US-06 — 多设备选择（行为由 AC-02-4/5/6、AC-04-5、AC-05-3/4 共同覆盖）

> 无独立 AC；设备选择行为（`--udid` 选中 / 未连报错 / 多台交互 / 0 台报错）由 install、uninstall、apps 各组 AC 完整规定。

### US-07 — iOS 17+ tunnel 处理

- **AC-07-1**：Given iOS 17+ 设备已连接且 tunnel 未运行，When 运行 `device list`，Then 该设备**仍被列出**（usbmuxd 可列举），exit 0。
- **AC-07-2**：Given iOS 17+ 设备无 tunnel，When 运行 `device install <bundle-id>`（library 有 IPA），Then 返回 `ErrTunnelRequired`，显示 `"iOS 17+ tunnel required; run: sudo ios tunnel start"`，exit 1。
- **AC-07-3**：Given iOS 17+ 设备无 tunnel，When 运行 `device apps` 或 `device uninstall <bundle-id>`，Then 返回 `ErrTunnelRequired` + 同样提示消息，exit 1。
- **AC-07-4**：Given 任意场景，When 工具执行设备操作，Then **绝不**自动运行 `sudo` / `ios tunnel start`（用户必须手动启动 tunnel）；验证方法：源码审计执行路径无 `sudo` / 无 exec 提权调用。

### US-08 — `--latest`（update 用例）

- **AC-08-1**：Given library 最高版本为 v1.0 且 App Store 最新为 v2.0，When 运行 `device install <bundle-id> --latest`，Then 先下载 v2.0 到 library（覆盖/新增），再推送 v2.0 到设备，成功消息显示 v2.0，exit 0。
- **AC-08-2**：Given library 最高版本已等于 App Store 最新版本，When 运行 `device install <bundle-id> --latest`，Then 不重复下载，推送 library 现有 IPA，显示 `"already up to date (vX)"`，exit 0。
- **AC-08-3**：Given `--latest` 触发下载但 active profile 未登录，When 运行，Then 同 AC-03-3 报 credentials 错误，exit 1。

### US-09 — `--profile` flag

- **AC-09-1**：Given `--profile <id>` 指向有效且已登录的 profile 且其 library 含该 IPA，When 运行 `device install <bundle-id> --profile <id>`，Then 推送该 profile library 的 IPA（不触及 active profile）。
- **AC-09-2**：Given `--profile <id>` 指向有效但**未登录**的 profile 且其 library 含该 IPA，When 运行 `device install <bundle-id> --profile <id>`，Then 推送成功（cached 推送无需凭据），exit 0。
- **AC-09-3**：Given `--profile <id>` 指向有效但未登录的 profile 且 library **缺**该 IPA（需自动下载），When 运行 `device install <bundle-id> --profile <id>`，Then 复用 download 流程返回 `"profile '<id>' has no credentials"` 错误，exit 1。
- **AC-09-4**：Given `--profile <id>` 指向不存在的 profile，When 运行任一 device 命令带 `--profile <id>`，Then 显示 `"profile '<id>' not found"` 错误，exit 1。
- **AC-09-5**：Given `--profile` 用于 device apps / uninstall（只读设备、不触及 library），When 运行 `device apps --profile <id>`，Then profile 参数被接受但**不影响**设备操作（设备操作与账号无关；profile 仅在涉及 library/下载时生效）。注：为避免误导，design 阶段决定 device apps/uninstall 是否接受 `--profile`（可能只 install 接受）。

### US-10 — `--version` 选择

- **AC-10-1**：Given library 含 bundle-id 的多个版本，When 运行 `device install <bundle-id> --version <v>`，Then 推送该指定版本，exit 0。
- **AC-10-2**：Given library 含 bundle-id 的多个版本且未传 `--version`，When 运行 `device install <bundle-id>`，Then 推送**最高**版本，exit 0。
- **AC-10-3**：Given `--version <v>` 指向 library 中不存在的版本，When 运行 `device install <bundle-id> --version <v>`，Then 显示 `"version '<v>' not in library for '<bundle-id>'"` 错误，exit 1。
- **AC-10-4**：Given `--latest` 与 `--version <v>` 同时传入，When 运行，Then 显示冲突错误（二者互斥），exit 1。

## 6. Non-Functional Requirements

| ID | Category | Requirement | Measurement |
|----|----------|-------------|-------------|
| NFR-01 | Reliability — tunnel precondition | iOS 17+ 缺 tunnel 时绝不静默失败或挂起；在合理时间内返回 `ErrTunnelRequired`。 | iOS 17+ 无 tunnel 时 `device install` 在 5s 内 exit 1 + 可操作消息。 |
| NFR-02 | Reliability — clean install | install 要么完整成功要么干净失败；不留下设备端歧义状态（zipconduit 设备端原子）。 | 失败后 `device apps` 状态与 install 前一致（无半装残留）。 |
| NFR-03 | Security — no privilege escalation | 工具**绝不**运行 `sudo` / 自动启动 tunnel；用户必须手动。 | 源码审计：执行路径 grep `sudo` / exec 提权调用为空。 |
| NFR-04 | Usability — actionable errors | 所有设备错误（无设备 / tunnel / trust / 未装 / 不存在）含人类可读原因 + 下一步建议。 | 每条错误消息含 cause + suggestion（如 `run: sudo ios tunnel start` / `trust this Mac on the device`）。 |
| NFR-05 | Compatibility | 仅支持 macOS（与 v1 一致）；依赖 go-ios 设备后端。 | `go build` 产出 macOS 二进制；非 macOS 不保证。 |
| NFR-06 | Maintainability — go-ios isolation | go-ios 类型仅出现在 `internal/device/`；CLI 层只见我们的接口。 | `grep -r "danielpaulus/go-ios" internal/cli` 无结果。 |
| NFR-07 | No regression | 现有全部测试（前三 mission）继续通过。 | `go test ./... -count=1` exit 0。 |
| NFR-08 | Performance — device list | 设备列举快速。 | `device list` 端到端 < 2s（正常 usbmuxd 响应）。 |
| NFR-09 | Usability — non-interactive safety | 破坏性操作（uninstall）非 TTY 时拒绝。 | CI 模式 `device uninstall` → exit 1（AC-04-4）。 |
| NFR-10 | Consistency | `--profile` / `--udid` flag 行为与现有 `--profile` 模式一致；错误消息风格统一。 | flag 解析、profile-not-found、device-not-connected 错误文案与 download/library 命令同源。 |

## 7. Key Domain Concepts

| Concept | Description |
|---------|-------------|
| **Device** | 一台已连接的 iOS 设备，由 UDID 唯一标识。含 Properties（name / iOS version / connection type / SupportsRsd flag）。go-ios `ios.DeviceEntry`。 |
| **UDID** | 设备唯一标识符。`--udid` flag 的主键；`device list` 输出供复制。 |
| **Connection Type** | USB vs Network（go-ios `ConnectionTypeLabel()`）。iOS 17+ RSD 设备走 tunnel。 |
| **SupportsRsd / iOS 17+** | 设备需 tunnel 才能做服务操作（install/apps/uninstall）的标志。Tunnel 经 `sudo ios tunnel start` 启动。 |
| **Tunnel** | CoreDevice tunnel 进程（go-ios `ios tunnel start`），启用与 iOS 17+ 设备的服务通信。本工具不启动，只检测缺失并提示。 |
| **zipconduit** | go-ios 推送 IPA 到设备安装的服务。非 RSD 走 `com.apple.streaming_zip_conduit`；RSD/tunnel 走 `.shim.remote`。 |
| **installationproxy** | go-ios 列举已装 app（`BrowseUserApps`）与卸载（`Uninstall`）的服务。 |
| **Push** | 把本地 IPA 推到设备安装的动作（`zipconduit.SendFile`）。 |
| **Auto-download** | install 编排步骤——library 缺 bundle-id 时运行下载流程（lookup → download → ReplicateSinf）填充 library 后再 push。 |
| **Library cache reuse** | install 复用 per-profile library；library 是 install 的快速缓存层。 |
| **Profile（继承）** | per-account 配置；install 的 library 隔离 + 下载凭据均按 profile 索引。 |
| **`--latest`** | install flag：强制查询 App Store 最新版，library 不及则下载再推（= update 语义）。 |
| **user app vs system app** | `device apps` 仅列 user-installed app（`BrowseUserApps`），不含系统 app。 |

## 8. Success Criteria

1. **闭环可用**：用户能把 app 从 App Store 一路装到设备（`device install <bid>` 自动下载 + 推送，live 设备验证）。
2. **设备管理可用**：list / install / uninstall / apps 四项在真实设备上工作（validate 阶段手动验收）。
3. **多设备可控**：多台连接时能精确选定目标设备（`--udid` 或交互）。
4. **iOS 17+ 正确**：缺 tunnel 时给可操作错误，不静默失败、不自动 sudo（AC-07 全过）。
5. **代码隔离**：go-ios 类型不泄露到 CLI 层（NFR-06）。
6. **无回归**：现有测试全绿（NFR-07）。
7. **体验一致**：`--profile` / `--udid` flag 与错误文案风格与现有命令统一（NFR-10）。

## 9. Clarification Notes

- **所有高影响歧义已在需求讨论中解决**（Q1 命令树统一为 `device` 组；Q2 install 来源 = library by bundle-id；Q3 砍掉 update 合并进 `install --latest`；多设备单命令单台）。无 NEEDS CLARIFICATION 项。
- **Spike 不需要**：go-ios API 签名已在 `research.md` §2 实证；live 设备端到端行为是 validate 阶段手动验收项，不是 requirements 阻塞。
- **命令树重构**：移除顶层 `devices` 单命令 + `install push/uninstall/update` 组（均 stub，零风险），统一为 `device list/apps/install/uninstall`。这是 UX 决策，不耦合内部实现。
- **install = library-or-download → push**：library 有则推（快、离线可用），library 无则自动下载（复用 download 流程）再推。`--latest` 收编 update 用例（强制刷新 App Store 最新版）。
- **多设备**：单命令操作单台设备；`--all` 批量留给未来。
- **iOS 17+ tunnel**：绝不自动 sudo（安全红线）；tunnel 缺失返回 `ErrTunnelRequired` + `sudo ios tunnel start` 提示；`device list` 经 usbmuxd 仍可列举 iOS 17+ 设备。
- **设备配对/信任**：假设已配对（A-02）；trust 错误由 go-ios 上浮 + 提示。
- **`--profile` 适用范围**：install 必须接受（涉及 library + 下载凭据）；device apps / uninstall 是否接受 `--profile` 留给 design 决定（设备操作本身与账号无关，AC-09-5 已标注）。
- **`device apps` 过滤**：不支持按 bundle-id 过滤参数（用户可 grep 输出）；保持命令极简。
