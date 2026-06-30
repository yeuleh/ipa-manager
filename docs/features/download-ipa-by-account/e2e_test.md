# E2E Test — download-ipa-by-account

> 本文档从 `requirements.md`（42 AC）+ `design.md` 派生。遵循 spec → cases → code 单向流：测试用例不反向从实现推导。

---

## 1. Test Scope

### 1.1 测试类型

| 类型 | 覆盖范围 | 说明 |
|------|----------|------|
| **自动化 E2E**（Go test） | CLI 命令行为（mock Apple API） | cobra RunE + mock ProfileAppStore + mock LibraryStore；断言 stdout / exit code / mock 内部状态 |
| **单元测试**（Go test） | library.Store（JSON CRUD）、appstore adapter（类型转换）、resolveProfile / validateOutputPath | temp dir 隔离；不依赖 Apple API |
| **手动验收** | live Apple API + 真实 keychain + 真实下载 | validate 阶段执行；需要真实 Apple 账号 + 2FA |

### 1.2 测试环境前置

- **自动化**：Go ≥ 1.26；`go test ./... -count=1`；无网络依赖（全 mock）。
- **手动**：macOS；已通过 `auth login` 登录至少一个真实 Apple 账号；macOS Keychain 可解锁；网络可达 Apple。

### 1.3 Validation Oracles（验收断言层次）

| 层次 | 断言对象 | 示例 |
|------|----------|------|
| **L1 — stdout** | 命令输出含预期字符串 | `assert.Contains(t, output, "Downloaded")` |
| **L2 — exit code** | RunE 返回 nil（exit 0）或 error（exit 1） | `require.NoError(t, err)` / `require.Error(t, err)` |
| **L3 — mock 内部状态** | mock AppStore/LibraryStore 的方法被调用 / 调用次数 | `assert.True(t, mockAS.downloadCalled)` / `assert.Equal(t, 1, mockAS.downloadCalls)` |
| **L4 — 文件系统** |（手动验收）IPA 文件存在且非空 | `stat.Size() > 0` |
| **L5 — 设备可安装性** |（手动验收）下载的 IPA 可被 go-ios 安装到设备 | `ios install --path=<ipa>` 成功 |

> 自动化测试覆盖 L1-L3。L4-L5 在 validate 阶段手动执行。

### 1.4 Mock 注入架构

参考 design DD-12：测试构造 `Deps{Store: mockStore, AppStoreFactory: func(p) { return mockAppStore, nil }, LibraryStore: mockLibraryStore, UI: mockPrompter}`，传给命令构造器，跑 RunE，断言 L1-L3。

---

## 2. E2E Test Cases

### US-01 — Search（模糊搜索）

#### E2E-001 / AC-01-1 — Search happy path
- **Type**: happy
- **Given**: active profile 已登录；mockAppStore.Search 返回 3 个结果
- **When**: 运行 `apps search wechat`
- **Then**: stdout 含表格（Name / Bundle-ID / Version / Price 列）；exit 0
- **Pass**: output 含 "WeChat" AND "com.tencent.xin" AND exit 0

#### E2E-002 / AC-01-2 — Search no active profile
- **Type**: failure
- **Given**: 无 active profile
- **When**: 运行 `apps search wechat`
- **Then**: stderr 含 "no active profile" + "accounts use"；exit 1
- **Pass**: err 含 "no active profile" AND err 含 "accounts use"

#### E2E-002a / AC-01-2 — Search active profile not logged in（E-04 fix: 补充此变体）
- **Type**: failure
- **Given**: active profile 存在但 mockStore.HasCredentials=false
- **When**: 运行 `apps search wechat`
- **Then**: err 含 "has no credentials" + "auth login"；exit 1
- **Pass**: err 含 "no credentials" AND err 含 "auth login"

#### E2E-003 / AC-01-3 — Search zero results
- **Type**: edge
- **Given**: active profile 已登录；mockAppStore.Search 返回空 slice
- **When**: 运行 `apps search nonexistentapp12345`
- **Then**: stdout 含 "no results found"；exit 0
- **Pass**: output 含 "no results" AND exit 0

#### E2E-004 / AC-01-4 — Search with --limit
- **Type**: edge
- **Given**: active profile 已登录；mockAppStore.Search 被调用时 limit=2
- **When**: 运行 `apps search wechat --limit 2`
- **Then**: mockAppStore 收到 limit=2；exit 0
- **Pass**: mockAppStore.searchLimit == 2

#### E2E-005 / AC-01-5 — Search with --profile
- **Type**: happy
- **Given**: profile "bob_test" 存在且已登录
- **When**: 运行 `apps search wechat --profile bob_test`
- **Then**: mockAppStore.AccountInfo 被调用（用 bob_test 的 factory）；exit 0
- **Pass**: factory 收到 Profile{ID:"bob_test"} AND exit 0

#### E2E-006 / AC-01-6 — Search invalid --limit
- **Type**: failure
- **Given**: active profile 已登录
- **When**: 运行 `apps search wechat --limit 0`（或 -1 / "abc"）
- **Then**: stderr 含 "invalid --limit value"；exit 1
- **Pass**: err 含 "invalid --limit"

### US-02 — Download（下载 IPA）

#### E2E-007 / AC-02-1 — Download happy path
- **Type**: happy
- **Given**: active profile 已登录；mockAppStore.AccountInfo/Lookup/Download/ReplicateSinf 均成功
- **When**: 运行 `download com.tencent.xin`
- **Then**: stdout 含 "Downloaded" + 文件路径；mockLibraryStore.Add 被调用；exit 0
- **Pass**: output 含 "Downloaded" AND mockLibraryStore.addCalled == true AND exit 0

#### E2E-008 / AC-02-2 — Downloaded file exists
- **Type**: happy（手动验收 L4）
- **Given**: E2E-007 成功（或手动 download）
- **When**: 检查 library 目录
- **Then**: IPA 文件存在且 size > 0
- **Pass**: `stat(path).Size() > 0`（手动）

#### E2E-009 / AC-02-3 — Download no active profile
- **Type**: failure
- **Given**: 无 active profile
- **When**: 运行 `download com.tencent.xin`
- **Then**: err 含 "no active profile"；exit 1
- **Pass**: err 含 "no active profile"

#### E2E-010 / AC-02-4 — Download app not found
- **Type**: failure
- **Given**: mockAppStore.Lookup 返回 error（"app not found"）
- **When**: 运行 `download com.nonexistent.app`
- **Then**: err 含 "app not found"；exit 1
- **Pass**: err 含 "app not found: com.nonexistent.app"

#### E2E-011 / AC-02-5 — Download already exists (skip)
- **Type**: edge
- **Given**: mockLibraryStore.Get 返回已有 Entry（同版本）；无 --force
- **When**: 运行 `download com.tencent.xin`
- **Then**: stdout 含 "already exists"；mockAppStore.Download **未**被调用；exit 0
- **Pass**: output 含 "already exists" AND mockAS.downloadCalls == 0 AND exit 0

#### E2E-012 / AC-02-6 — Download already exists with --force
- **Type**: edge
- **Given**: mockLibraryStore.Get 返回已有 Entry；有 --force
- **When**: 运行 `download com.tencent.xin --force`
- **Then**: mockAppStore.Download 被调用（重新下载）；exit 0
- **Pass**: mockAS.downloadCalls == 1 AND exit 0

#### E2E-013 / AC-02-7 — Download license required (free, interactive, yes)
- **Type**: happy
- **Given**: mockAppStore.Download 第一次返回 ErrLicenseRequired；Price=0；mockPrompter.confirm=true；Purchase 成功；Download 第二次成功
- **When**: 运行 `download com.example.freeapp`
- **Then**: mockPrompter.Confirm 被调用；mockAppStore.Purchase 被调用；Download 重试成功；exit 0
- **Pass**: mockAS.purchaseCalled == true AND mockAS.downloadCalls == 2 AND exit 0

#### E2E-014 / AC-02-7 — Download license required (free, interactive, no)
- **Type**: edge
- **Given**: mockAppStore.Download 返回 ErrLicenseRequired；Price=0；mockPrompter.confirm=false
- **When**: 运行 `download com.example.freeapp`
- **Then**: stdout 含 "cancelled"；mockAppStore.Purchase **未**被调用；exit 0
- **Pass**: output 含 "cancelled" AND mockAS.purchaseCalled == false AND exit 0

#### E2E-015 / AC-02-8 — Download license required (paid)
- **Type**: failure
- **Given**: mockAppStore.Download 返回 ErrLicenseRequired；Price=9.99
- **When**: 运行 `download com.example.paidapp`
- **Then**: err 含 "paid apps are not supported"；exit 1
- **Pass**: err 含 "paid apps are not supported"

#### E2E-016 / AC-02-9 — Download with --profile
- **Type**: happy
- **Given**: profile "bob_test" 存在且已登录
- **When**: 运行 `download com.tencent.xin --profile bob_test`
- **Then**: factory 收到 bob_test；mockLibraryStore.Add 用 bob_test 的 profileID；exit 0
- **Pass**: factory profile.ID == "bob_test" AND mockLS.addProfileID == "bob_test"

#### E2E-017 / AC-02-10 — Download token expired (auto re-login)
- **Type**: happy
- **Given**: mockAppStore.Download 第一次返回 ErrPasswordTokenExpired；Login 成功；Download 第二次成功
- **When**: 运行 `download com.tencent.xin`
- **Then**: mockAppStore.Login 被调用（重登录）；Download 重试成功；exit 0
- **Pass**: mockAS.loginCalls >= 1 AND mockAS.downloadCalls == 2 AND exit 0

#### E2E-018 / AC-02-11 — Download license required (non-interactive)
- **Type**: failure
- **Given**: mockAppStore.Download 返回 ErrLicenseRequired；Price=0；isInteractive()=false
- **When**: 运行 `download com.example.freeapp`（stdin 非 TTY）
- **Then**: err 含 "interactive confirmation required"；mockPrompter.Confirm **未**被调用；exit 1
- **Pass**: err 含 "interactive confirmation required" AND mockPrompter.confirmCalled == false

### US-03 — Per-account isolation

#### E2E-019 / AC-03-1 — Library isolation between profiles
- **Type**: edge
- **Given**: profile A 有 IPA（mockLibraryStore）；profile B 无
- **When**: 运行 `library list --profile B`
- **Then**: profile B 的列表不含 profile A 的 IPA
- **Pass**: mockLS.List("B") 返回空 AND mockLS.List("A") 返回非空

#### E2E-020 / AC-03-2 — Same app downloaded by two profiles
- **Type**: edge（手动验收 L4）
- **Given**: profile A 和 B 各自下载同一 app
- **When**: 检查两个 profile 的 library 目录
- **Then**: 各自目录下均有 IPA 文件
- **Pass**: 两个文件均存在（手动）

### US-04 — Library list

#### E2E-021 / AC-04-1 — Library list happy path
- **Type**: happy
- **Given**: mockLibraryStore.List 返回 2 个 Entry
- **When**: 运行 `library list`
- **Then**: stdout 含表格（Bundle-ID / Version / Size / Downloaded-At）；exit 0
- **Pass**: output 含两个 bundle-id AND exit 0

#### E2E-022 / AC-04-2 — Library list empty
- **Type**: edge
- **Given**: mockLibraryStore.List 返回空
- **When**: 运行 `library list`
- **Then**: stdout 含 "no IPAs in library"；exit 0
- **Pass**: output 含 "no IPAs" AND exit 0

#### E2E-023 / AC-04-3 — Library list with --profile
- **Type**: happy
- **Given**: profile "bob_test" 存在
- **When**: 运行 `library list --profile bob_test`
- **Then**: mockLibraryStore.List 用 "bob_test"
- **Pass**: mockLS.listProfileID == "bob_test"

### US-05 — Library clean

#### E2E-024 / AC-05-1 — Library clean all (with custom path disclosure)
- **Type**: happy
- **Given**: mockLibraryStore.List 返回 3 个 Entry（1 个自定义路径）；mockPrompter.confirm=true
- **When**: 运行 `library clean`
- **Then**: stdout 含 "remove all 3" + 自定义路径；mockLibraryStore.CleanAll 被调用；exit 0
- **Pass**: output 含 "remove all 3" AND output 含自定义路径 AND mockLS.cleanAllCalled == true AND exit 0

#### E2E-025 / AC-05-2 — Library clean empty
- **Type**: edge
- **Given**: mockLibraryStore.List 返回空
- **When**: 运行 `library clean`
- **Then**: stdout 含 "already empty"；mockLibraryStore.CleanAll **未**被调用；exit 0
- **Pass**: output 含 "already empty" AND mockLS.cleanAllCalled == false AND exit 0

#### E2E-026 / AC-05-3 — Library clean specific bundle-id
- **Type**: happy
- **Given**: mockLibraryStore.Get 返回 Entry；mockPrompter.confirm=true
- **When**: 运行 `library clean com.tencent.xin`
- **Then**: stdout 含 "remove" + 版本 + 大小；mockLibraryStore.Remove 被调用；exit 0
- **Pass**: output 含 "remove" AND mockLS.removeBundleID == "com.tencent.xin" AND exit 0

#### E2E-027 / AC-05-3 — Library clean specific bundle-id (reject)
- **Type**: edge
- **Given**: mockLibraryStore.Get 返回 Entry；mockPrompter.confirm=false
- **When**: 运行 `library clean com.tencent.xin`
- **Then**: stdout 含 "cancelled"；mockLibraryStore.Remove **未**被调用；exit 0
- **Pass**: output 含 "cancelled" AND mockLS.removeCalled == false AND exit 0

#### E2E-028 / AC-05-4 — Library clean non-existent bundle-id
- **Type**: edge
- **Given**: mockLibraryStore.Get 返回 ErrEntryNotFound
- **When**: 运行 `library clean com.nonexistent.app`
- **Then**: stdout 含 "no IPA"；exit 0
- **Pass**: output 含 "no IPA" AND exit 0

#### E2E-029 / AC-05-5 — Library clean with --profile
- **Type**: happy
- **Given**: profile "bob_test" 存在
- **When**: 运行 `library clean --profile bob_test`
- **Then**: mockLibraryStore 用 "bob_test"
- **Pass**: mockLS 的 profileID == "bob_test"

#### E2E-030 / AC-05-6 — Library list after clean
- **Type**: edge
- **Given**: clean 成功删除了一个 IPA
- **When**: 运行 `library list`
- **Then**: 被删除的 bundle-id 不在列表中
- **Pass**: mockLS.List 不含已删除 bundle-id

#### E2E-031 / AC-05-7 — Library clean custom-output IPA
- **Type**: edge
- **Given**: mockLibraryStore.Get 返回 Entry（file_path = "/custom/path.ipa"）；mockPrompter.confirm=true
- **When**: 运行 `library clean com.example.app`
- **Then**: stdout 含完整自定义路径 "/custom/path.ipa"；exit 0
- **Pass**: output 含 "/custom/path.ipa"

#### E2E-032 / AC-05-8 — Library clean file already absent
- **Type**: edge
- **Given**: mockLibraryStore.Remove 时文件不存在（os.IsNotExist）
- **When**: 运行 `library clean com.example.app`
- **Then**: stdout 含 "file already absent"；index 条目被移除；exit 0
- **Pass**: output 含 "file already absent" AND exit 0

#### E2E-033 / AC-05-9 — Library clean non-interactive (destructive)
- **Type**: failure
- **Given**: mockLibraryStore.List 返回非空；isInteractive()=false
- **When**: 运行 `library clean`（stdin 非 TTY）
- **Then**: err 含 "confirmation required in non-interactive mode"；exit 1
- **Pass**: err 含 "confirmation required in non-interactive"

#### E2E-034 / AC-05-9 — Library clean non-interactive (empty, no-op)
- **Type**: edge
- **Given**: mockLibraryStore.List 返回空；isInteractive()=false
- **When**: 运行 `library clean`（stdin 非 TTY）
- **Then**: stdout 含 "already empty"；exit 0（no-op 不受非交互约束）
- **Pass**: output 含 "already empty" AND exit 0

#### E2E-034a / AC-05-9 — Library clean non-interactive specific bundle (file exists)
- **Type**: failure（E-01 fix: 补充 specific-bundle 非交互覆盖）
- **Given**: mockLibraryStore.Get 返回 Entry（文件存在）；isInteractive()=false
- **When**: 运行 `library clean com.tencent.xin`（stdin 非 TTY）
- **Then**: err 含 "confirmation required in non-interactive mode"；exit 1
- **Pass**: err 含 "confirmation required"

#### E2E-034b / AC-05-9 / AC-05-8 — Library clean non-interactive specific bundle (file absent)
- **Type**: edge（E-01 fix: 非交互 + 文件不存在 = no-op）
- **Given**: mockLibraryStore.Get 返回 Entry（FilePath 指向不存在文件）；isInteractive()=false
- **When**: 运行 `library clean com.example.app`（stdin 非 TTY）
- **Then**: stdout 含 "file already absent"；index 条目被移除；exit 0（无文件可删，不需确认）
- **Pass**: output 含 "file already absent" AND exit 0

### US-08 — --profile flag（跨命令）

#### E2E-035 / AC-08-1 — --profile not found
- **Type**: failure
- **Given**: profile "ghost" 不存在
- **When**: 运行 `download com.tencent.xin --profile ghost`
- **Then**: err 含 "profile 'ghost' not found"；exit 1
- **Pass**: err 含 "profile" AND err 含 "not found"

#### E2E-036 / AC-08-2 — --profile not logged in
- **Type**: failure
- **Given**: profile "charlie" 存在但 mockStore.HasCredentials=false
- **When**: 运行 `apps search wechat --profile charlie`
- **Then**: err 含 "has no credentials"；exit 1
- **Pass**: err 含 "no credentials"

#### E2E-037 / AC-08-3 — No --profile uses active
- **Type**: happy
- **Given**: active profile = "alice"
- **When**: 运行 `download com.tencent.xin`（无 --profile）
- **Then**: factory 收到 alice
- **Pass**: factory profile.ID == "alice"

### US-09 — Version selection

#### E2E-038 / AC-09-1 — Download specific version
- **Type**: happy
- **Given**: mockAppStore.Download 接收 ExternalVersionID="abc123"
- **When**: 运行 `download com.tencent.xin --external-version-id abc123`
- **Then**: mockAppStore.Download 收到 ExternalVersionID；downloadResult.Version 对应；stdout + library list 显示该版本；exit 0
- **Pass**: mockAS.downloadExternalVersionID == "abc123" AND output 含版本号

#### E2E-039 / AC-09-2 — Download invalid version
- **Type**: failure
- **Given**: mockAppStore.Download 返回 Apple error
- **When**: 运行 `download com.tencent.xin --external-version-id invalid`
- **Then**: err 含 Apple 错误消息；exit 1
- **Pass**: require.Error

### US-10 — Custom output path

#### E2E-040 / AC-10-1 — Download with --output
- **Type**: happy
- **Given**: mockAppStore.Download 成功
- **When**: 运行 `download com.tencent.xin --output /tmp/test.ipa`
- **Then**: mockAppStore.Download OutputPath="/tmp/test.ipa"；exit 0
- **Pass**: mockAS.downloadOutputPath == "/tmp/test.ipa"

#### E2E-041 / AC-10-2 — --output tracked in library list
- **Type**: happy
- **Given**: 之前用 --output 下载；mockLibraryStore 有该 Entry（FilePath = 自定义路径）
- **When**: 运行 `library list`
- **Then**: 该 IPA 出现在列表中（含自定义路径在 PATH 列）
- **Pass**: output 含该 bundle-id AND mockLibraryStore.Get 返回的 Entry.FilePath == 自定义路径（L3 oracle）

#### E2E-042 / AC-10-3 — --output already exists (no --force)
- **Type**: edge（E-03 fix: 基于物理文件存在性，非索引）
- **Given**: outputPath 物理文件已存在（temp file 预创建）；未传 --force
- **When**: 运行 `download com.tencent.xin --output /tmp/existing.ipa`
- **Then**: stdout 含 "already exists"；mockAppStore.Download **未**被调用；exit 0
- **Pass**: output 含 "already exists" AND mockAS.downloadCalls == 0 AND exit 0

#### E2E-043 / AC-10-4 — --output parent dir missing
- **Type**: failure
- **Given**: --output 指向不存在的目录
- **When**: 运行 `download com.tencent.xin --output /nonexistent/dir/app.ipa`
- **Then**: err 含 "output directory does not exist"；exit 1
- **Pass**: err 含 "output directory does not exist"

#### E2E-044 / AC-10-5 — --output is directory
- **Type**: failure
- **Given**: --output 指向已存在的目录
- **When**: 运行 `download com.tencent.xin --output /tmp`
- **Then**: err 含 "output path is a directory"；exit 1
- **Pass**: err 含 "output path is a directory"

#### E2E-045 / AC-10-6 — --output permission denied
- **Type**: failure
- **Given**: --output 父目录无写权限（手动创建只读目录）
- **When**: 运行 `download com.tencent.xin --output /readonly/app.ipa`
- **Then**: err 含 "cannot write to output path"；exit 1
- **Pass**: err 含 "cannot write" OR err 含 "permission denied"

### NFR Cases

#### E2E-N01 / NFR-04 — No credential leak in output
- **Type**: NFR
- **Given**: download 成功（mockAppStore AccountInfo 含 Password）
- **When**: 运行 `download com.tencent.xin`
- **Then**: stdout/stderr **不含** password / passwordToken / directoryServicesIdentifier
- **Pass**: `assert.NotContains(output, "password-value")` etc.

#### E2E-N02 / NFR-05 — Progress bar in interactive mode
- **Type**: NFR（手动验收）
- **Given**: 交互式终端；下载 > 5MB 的 app
- **When**: 运行 `download <bundle-id>`
- **Then**: 可见进度条更新
- **Pass**: 肉眼确认进度条（手动）

#### E2E-N03 / NFR-08 — ipatool types not leaked
- **Type**: NFR
- **Given**: 编译后
- **When**: `grep -r "majd/ipatool" internal/cli internal/library`
- **Then**: 无结果
- **Pass**: grep 退出码 1（无匹配）

#### E2E-N04 / NFR-09 — No regression
- **Type**: NFR
- **Given**: 全部代码实现完成
- **When**: `go test ./... -count=1`
- **Then**: exit 0（含前两 mission 的 69+ 测试）
- **Pass**: exit 0

#### E2E-N05 / NFR-01 — Atomic download
- **Type**: NFR（手动验收）
- **Given**: 下载进行中
- **When**: 中断（Ctrl-C）
- **Then**: 目标路径要么不存在，要么是完整的上一次版本
- **Pass**: 无 `.ipa.tmp` 残留；无损坏 IPA（手动）

---

## 3. Unit Test Coverage（补充）

除 E2E 外，以下需单元测试覆盖：

| 模块 | 测试文件 | 覆盖点 |
|------|----------|--------|
| `library.Store` | `internal/library/store_test.go` | Add/List/Get/Remove/CleanAll；temp dir 隔离；JSON 原子写；空状态；STALE 条目 |
| `appstore` 类型转换 | `internal/appstore/query_test.go` | `appToAppInfo` / `appInfoToApp` 字段映射；`sinfsToOur` 转换 |
| `cli.resolveProfile` | `internal/cli/helpers_test.go` | active 缺省 / --profile 覆盖 / not found / not logged in / requireCredentials 开关 |
| `cli.validateOutputPath` | `internal/cli/helpers_test.go` | 正常 / 目录 / 父缺失 / 权限拒绝 |
| `cli.isInteractive` | `internal/cli/helpers_test.go` | TTY / pipe 检测 |

---

## 4. Traceability Matrix

### E2E ↔ AC ↔ US

| E2E | AC | US | Type |
|-----|----|----|------|
| E2E-001 | AC-01-1 | US-01 | happy |
| E2E-002 | AC-01-2 | US-01 | failure |
| E2E-002a | AC-01-2 | US-01 | failure (E-04 fix) |
| E2E-003 | AC-01-3 | US-01 | edge |
| E2E-004 | AC-01-4 | US-01 | edge |
| E2E-005 | AC-01-5 | US-01/08 | happy |
| E2E-006 | AC-01-6 | US-01 | failure |
| E2E-007 | AC-02-1 | US-02 | happy |
| E2E-008 | AC-02-2 | US-02 | happy (manual) |
| E2E-009 | AC-02-3 | US-02 | failure |
| E2E-010 | AC-02-4 | US-02 | failure |
| E2E-011 | AC-02-5 | US-06 | edge |
| E2E-012 | AC-02-6 | US-06 | edge |
| E2E-013 | AC-02-7 (yes) | US-07 | happy |
| E2E-014 | AC-02-7 (no) | US-07 | edge |
| E2E-015 | AC-02-8 | US-07 | failure |
| E2E-016 | AC-02-9 | US-08 | happy |
| E2E-017 | AC-02-10 | US-02 | happy |
| E2E-018 | AC-02-11 | US-07 | failure |
| E2E-019 | AC-03-1 | US-03 | edge |
| E2E-020 | AC-03-2 | US-03 | edge (manual) |
| E2E-021 | AC-04-1 | US-04 | happy |
| E2E-022 | AC-04-2 | US-04 | edge |
| E2E-023 | AC-04-3 | US-04/08 | happy |
| E2E-024 | AC-05-1 | US-05 | happy |
| E2E-025 | AC-05-2 | US-05 | edge |
| E2E-026 | AC-05-3 (yes) | US-05 | happy |
| E2E-027 | AC-05-3 (no) | US-05 | edge |
| E2E-028 | AC-05-4 | US-05 | edge |
| E2E-029 | AC-05-5 | US-05/08 | happy |
| E2E-030 | AC-05-6 | US-05 | edge |
| E2E-031 | AC-05-7 | US-05/10 | edge |
| E2E-032 | AC-05-8 | US-05 | edge |
| E2E-033 | AC-05-9 (destructive) | US-05 | failure |
| E2E-034 | AC-05-9 (no-op) | US-05 | edge |
| E2E-034a | AC-05-9 | US-05 | failure (E-01 fix) |
| E2E-034b | AC-05-9/08 | US-05 | edge (E-01 fix) |
| E2E-035 | AC-08-1 | US-08 | failure |
| E2E-036 | AC-08-2 | US-08 | failure |
| E2E-037 | AC-08-3 | US-08 | happy |
| E2E-038 | AC-09-1 | US-09 | happy |
| E2E-039 | AC-09-2 | US-09 | failure |
| E2E-040 | AC-10-1 | US-10 | happy |
| E2E-041 | AC-10-2 | US-10 | happy |
| E2E-042 | AC-10-3 | US-10 | edge |
| E2E-043 | AC-10-4 | US-10 | failure |
| E2E-044 | AC-10-5 | US-10 | failure |
| E2E-045 | AC-10-6 | US-10 | failure |
| E2E-N01 | NFR-04 | — | NFR |
| E2E-N02 | NFR-05 | — | NFR (manual) |
| E2E-N03 | NFR-08 | — | NFR |
| E2E-N04 | NFR-09 | — | NFR |
| E2E-N05 | NFR-01 | — | NFR (manual) |

### Reverse Coverage（US → E2E）

| US | E2E 覆盖 | 状态 |
|----|----------|------|
| US-01 | E2E-001~006 | ✓ 全覆盖 |
| US-02 | E2E-007~010, 017 | ✓ 全覆盖 |
| US-03 | E2E-019, 020 | ✓ 全覆盖 |
| US-04 | E2E-021~023 | ✓ 全覆盖 |
| US-05 | E2E-024~034 | ✓ 全覆盖 |
| US-06 | E2E-011, 012 | ✓ 全覆盖（AC-02-5/6） |
| US-07 | E2E-013~015, 018 | ✓ 全覆盖（AC-02-7/8/11） |
| US-08 | E2E-005, 016, 023, 029, 035~037 | ✓ 全覆盖 |
| US-09 | E2E-038, 039 | ✓ 全覆盖 |
| US-10 | E2E-040~045 | ✓ 全覆盖 |

**无未覆盖的 user story。**

### 注：AC-08-1 跨命令覆盖说明（E-05 fix）

AC-08-1 要求 `--profile <bad-id>` 在**所有命令**（search / download / library list / library clean）上报错。E2E-035 仅测试 download 的变体。其余三个命令的 `--profile` 解析共享同一 `resolveProfile()` helper（design DD-07），该 helper 的单元测试覆盖所有错误分支（not found / not logged in / no active）。因此 E2E 层不重复测试每个命令的 `--profile` 失败——`resolveProfile` 的单元测试（`helpers_test.go`）提供跨命令保证。
