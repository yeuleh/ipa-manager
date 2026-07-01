# Plan — download-ipa-by-account

> 本计划基于 accepted `requirements.md`（47 AC / 10 NFR / 11 US）和 `design.md`（12 DD）。任务分解为垂直切片，每个切片交付端到端可观测行为。

---

## 1. Implementation Context

| 维度 | 说明 |
|------|------|
| **Runtime/Language** | Go ≥ 1.26；单二进制 macOS CLI |
| **Key dependencies** | `yeuleh/ipatool/v2@v2.3.1-fix-auth.5`（Apple API）；`spf13/cobra`（CLI）；`charm.land/huh`（交互提示）；`charm.land/lipgloss`（表格）；`schollz/progressbar/v3`（进度条，已在 go.sum）；`golang.org/x/term`（TTY 检测，go-ios 传递依赖） |
| **Testing framework** | `testing` + `stretchr/testify`（assert/require）；mock 为手写 struct（与 auth_test.go 模式一致） |
| **Key constraints** | ipatool 类型不泄露到 CLI/library 层（NFR-08）；per-profile 隔离（NFR-03）；现有 69+ 测试不回归（NFR-09） |
| **Non-goals** | 不修改 ipatool fork 源码；不做设备侧操作（install push/uninstall/update）；不做 `app versions` |

---

## 2. Dependency Graph

```
T1 (app search) ──┐
                   ├──► T3 (download core) ──┬──► T4 (download retry)
T2 (lib list+Store)┤                         └──► T5 (download flags)
                   └──► T6 (library clean)
```

**并行轨道**：
- T1 + T2 可并行（互不依赖）
- T4 + T5 可在 T3 完成后并行（互不依赖）
- T6 可在 T2 完成后与 T3 并行

**依赖理由**：
- T3 依赖 T1：复用 resolveProfile helper + mockAppStore 扩展模式 + adapter AccountInfo 方法
- T3 依赖 T2：download 成功后调 `libraryStore.Add` 记录索引
- T4 依赖 T3：在 T3 的核心 download 编排上叠加 error recovery（skip/force/license/token retry/non-interactive）
- T5 依赖 T3：在 T3 的核心 download 编排上叠加 output control（--output 验证/跟踪 + --external-version-id skip bypass）
- T6 依赖 T2：clean 使用 library.Store 的 Remove/RemoveVersion/CleanAll 方法

**Foundation tasks**：0。T2 包含 library.Store 基础设施，但同时交付 `library list` CLI 行为——是垂直切片，不是纯 Foundation。

---

## 3. Task List

### T1: `app search` — App Store 搜索 ✅ DONE

| 维度 | 内容 |
|------|------|
| **User Stories** | US-01 (P1) |
| **Acceptance Criteria** | AC-01-1, AC-01-2, AC-01-3, AC-01-4, AC-01-5, AC-01-6, AC-08-1, AC-08-2, AC-08-3 |
| **E2E Cases** | E2E-001, E2E-002, E2E-002a, E2E-003, E2E-004, E2E-005, E2E-006 |
| **Files to create** | `internal/cli/app.go`（RENAME from apps.go）；`internal/cli/app_search_test.go`；`internal/cli/helpers_test.go`（resolveProfile 单元）；`internal/appstore/query.go`（AppInfo / AccountInfoResult 类型 + 转换 helper） |
| **Files to modify** | `internal/appstore/adapter.go`（ProfileAppStore 接口 +AccountInfo/Search）；`internal/appstore/client_impl.go`（adapter 实现 AccountInfo/Search + account 缓存）；`internal/cli/root.go`（appsCmd→appCmd）；`internal/cli/helpers.go`（+resolveProfile）；`internal/cli/auth_test.go`（mockAppStore +AccountInfo/Search 零值方法）；`go.mod`（如需） |
| **Required tests** | E2E-001~006/002a（CLI mock）；resolveProfile 单元测试（含 --profile not-found / not-logged-in / active 缺省，P2 fix: 跨命令覆盖 AC-08-1~3） |
| **Acceptance command** | `go test ./internal/cli/ -run "TestAppSearch|TestResolveProfile|TestAuth" -count=1 && go test ./... -count=1 && go vet ./...`（P3/P4 fix: 含全量回归 + auth 测试验证 mock 兼容） |
| **Completion criteria** | Spock review pass + tests pass + build + vet |
| **Rollback note** | 无持久化变更（search 只读）；若 `apps.go→app.go` rename 导致问题，git revert 即可 |

### T2: `library list` + Library Store 基础设施 ✅ DONE

| 维度 | 内容 |
|------|------|
| **User Stories** | US-04 (P1) |
| **Acceptance Criteria** | AC-04-1, AC-04-2, AC-04-3 |
| **E2E Cases** | E2E-021, E2E-022, E2E-023 |
| **Files to create** | `internal/library/store.go`（**完全重写**：Store 接口 7 方法 + JSON 实现 + 多版本复合键）；`internal/library/store_test.go`（comprehensive 单元测试）；`internal/cli/library.go`（libraryCmd + libraryListCmd）；`internal/cli/library_test.go`（library list E2E + mockLibraryStore） |
| **Files to modify** | `internal/library/ipa_store.go`（**删除**旧 stub）；`internal/cli/deps.go`（+LibraryStore 字段 + newProductionDeps）；`internal/cli/root.go`（+libraryCmd） |
| **Required tests** | store_test.go：Add 替换/共存、List 排序、Get 多版本、GetVersion found/not-found、Remove 全部版本、RemoveVersion 保留其他、CleanAll、原子写、空状态、STALE；library list E2E |
| **Acceptance command** | `go test ./internal/library/ -count=1 && go test ./internal/cli/ -run TestLibraryList -count=1 && go test ./... -count=1 && go vet ./...` |
| **Completion criteria** | Spock review pass + tests pass + build + vet |
| **Rollback note** | library/ 目录首次 download 时自动创建（additive，无迁移） |

### T3: `app download` — 核心下载流程 ✅ DONE

| 维度 | 内容 |
|------|------|
| **User Stories** | US-02 (P1), US-03 (P1), US-11 (P2 partial: download 多版本) |
| **Acceptance Criteria** | AC-02-1, AC-02-2, AC-02-3, AC-02-4, AC-02-5, AC-02-9, AC-03-1, AC-03-2, AC-08-1, AC-08-2, AC-08-3, AC-11-1, AC-11-2 |
| **E2E Cases** | E2E-007, E2E-008(manual), E2E-009, E2E-010, E2E-016, E2E-019, E2E-020(manual), E2E-035, E2E-036, E2E-037, E2E-046, E2E-047 |
| **Files to create** | `internal/cli/app_download.go`（download 命令 + DD-04 编排）；`internal/cli/app_download_test.go`（download E2E）；`internal/appstore/download_types.go`（DownloadInput/DownloadResult/Sinf/Progress）；`internal/appstore/progress.go`（progressBarWrapper + NewProgress + isInteractive） |
| **Files to modify** | `internal/appstore/adapter.go`（+Lookup/Download/ReplicateSinf）；`internal/appstore/client_impl.go`（adapter 实现 + extractVersionFromPath + mapDownloadError）；`internal/cli/helpers.go`（+validateOutputPath）；`internal/appstore/apps.go`（删除 stub）；`internal/cli/install.go`（移除 installDownloadCmd）；`internal/cli/auth_test.go`（mockAppStore +Lookup/Download/ReplicateSinf 字段）；`internal/apperr/errors.go`（+ErrAppNotFound/ErrReplicateSinfFailed）；`go.mod`（progressbar 转 direct） |
| **Required tests** | download happy path + skip (os.Stat) + not-found + multi-version + --profile (E2E-035/036/037, P2 fix: 从 T1 移入)；progress/validateOutputPath/isInteractive 单元 |
| **Acceptance command** | `go test ./internal/cli/ -run "TestDownload|TestValidateOutput|TestIsInteractive" -count=1 && go test ./internal/appstore/ -count=1 && go test ./... -count=1 && go vet ./...` |
| **Completion criteria** | Spock review pass + tests pass + build + vet |
| **Rollback note** | 写 .ipa 文件 + index.json 到 library 目录（additive）；失败不写索引 |

**依赖**：T1（resolveProfile + adapter AccountInfo）+ T2（libraryStore.Add）

### T4: `app download` — error recovery ✅ DONE

| 维度 | 内容 |
|------|------|
| **User Stories** | US-06 (P2), US-07 (P2) |
| **Acceptance Criteria** | AC-02-6, AC-02-7, AC-02-8, AC-02-10, AC-02-11 |
| **E2E Cases** | E2E-011, E2E-012, E2E-013, E2E-014, E2E-015, E2E-017, E2E-018 |
| **Files to modify** | `internal/cli/app_download.go`（+--force flag / license retry / token retry / non-interactive 检测）；`internal/appstore/adapter.go`（+Purchase/RefreshSession）；`internal/appstore/client_impl.go`（adapter 实现 Purchase/RefreshSession）；`internal/appstore/errors.go`（+ErrLicenseRequired/ErrPasswordTokenExpired 别名）；`internal/apperr/errors.go`（+ErrPaidAppNotSupported/ErrDownloadNonInteractive）；`internal/cli/auth_test.go`（mockAppStore +Purchase/RefreshSession 字段） |
| **Required tests** | --force overwrite + license retry (yes/no/paid/non-interactive) + token retry (RefreshSession success/fail) |
| **Acceptance command** | `go test ./internal/cli/ -run "TestDownloadForce|TestDownloadLicense|TestDownloadToken|TestDownloadNonInteractive" -count=1 && go test ./... -count=1 && go vet ./...` |
| **Completion criteria** | Spock review pass + tests pass + full regression + vet |
| **Rollback note** | 无新持久化（error recovery 逻辑） |

**依赖**：T3（核心 download 编排）

### T5: `app download` — output control ✅ DONE

| 维度 | 内容 |
|------|------|
| **User Stories** | US-09 (P3), US-10 (P3) |
| **Acceptance Criteria** | AC-09-1, AC-09-2, AC-10-1, AC-10-2, AC-10-3, AC-10-4, AC-10-5, AC-10-6 |
| **E2E Cases** | E2E-038, E2E-039, E2E-040, E2E-041, E2E-042, E2E-043, E2E-044, E2E-045 |
| **Files to modify** | `internal/cli/app_download.go`（+--output 路径校验/跟踪 + --external-version-id skip bypass）；`internal/cli/app_download_test.go`（+output/version E2E） |
| **Required tests** | --output 正常/目录/父缺失/权限拒绝/已存在 + --output library list 跟踪 + --external-version-id 下载/无效 |
| **Acceptance command** | `go test ./internal/cli/ -run "TestDownloadOutput|TestDownloadVersion" -count=1 && go test ./... -count=1 && go vet ./...` |
| **Completion criteria** | Spock review pass + tests pass + full regression + vet |
| **Rollback note** | 无新持久化（flag 处理逻辑） |

**依赖**：T3（核心 download 编排）。T4 + T5 可并行。

### T6: `library clean` — 清理（全部/单 app/指定版本/非交互）

| 维度 | 内容 |
|------|------|
| **User Stories** | US-05 (P1), US-11 (P2 partial: clean 多版本) |
| **Acceptance Criteria** | AC-05-1, AC-05-2, AC-05-3, AC-05-4, AC-05-5, AC-05-6, AC-05-7, AC-05-8, AC-05-9, AC-05-10, AC-05-11, AC-05-12 |
| **E2E Cases** | E2E-024, E2E-025, E2E-026, E2E-027, E2E-028, E2E-029, E2E-030, E2E-031, E2E-032, E2E-033, E2E-034, E2E-034a, E2E-034b, E2E-034c, E2E-048, E2E-049, E2E-050 |
| **Files to modify** | `internal/cli/library.go`（+libraryCleanCmd）；`internal/cli/library_test.go`（+clean E2E） |
| **Required tests** | clean all (with custom path disclosure) + empty + specific (yes/no) + not-found + file-absent + non-interactive (destructive/no-op/stale-only) + multi-version all + --version specific + --version not-found |
| **Acceptance command** | `go test ./internal/cli/ -run TestLibraryClean -count=1 && go test ./... -count=1 && go vet ./...` |
| **Completion criteria** | Spock review pass + tests pass + full regression + vet |
| **Rollback note** | 删除 .ipa 文件 + 修改 index.json（destructive，但用户确认后执行） |

**依赖**：T2（library.Store.Remove/RemoveVersion/CleanAll）

---

## 4. Traceability Matrices

### US/AC → Task

| US | AC | Task |
|----|-----|------|
| US-01 | AC-01-1~6 | T1 |
| US-02 | AC-02-1~5, 02-9 | T3 |
| US-02 | AC-02-6~8, 02-10~11 | T4 |
| US-03 | AC-03-1~2 | T3 |
| US-04 | AC-04-1~3 | T2 |
| US-05 | AC-05-1~12 | T6 |
| US-06 | AC-02-6 | T4 |
| US-07 | AC-02-7~8, 02-11 | T4 |
| US-08 | AC-08-1~3 | T1 (resolveProfile 单元) + T3 (download 集成) |
| US-09 | AC-09-1~2 | T5 |
| US-10 | AC-10-1~6 | T5 |
| US-11 | AC-11-1~2 | T3 |
| US-11 | AC-05-10~12 | T6 |

**无未覆盖的 US 或 AC。**

### E2E → Task

| E2E | Task |
|-----|------|
| E2E-001~006, 002a | T1 |
| E2E-021~023 | T2 |
| E2E-007~010, 016, 019, 035~037, 046~047 | T3 |
| E2E-011~015, 017~018 | T4 |
| E2E-038~045 | T5 |
| E2E-024~034c, 048~050 | T6 |
| E2E-008, 020 (manual L4/L5) | T3 (validate 阶段手动) |
| E2E-N01~N05 (NFR) | 跨任务（NFR-04 全任务；NFR-05 T3；NFR-08 全任务；NFR-09 全任务 acceptance 含 `go test ./...`；NFR-01 validate 手动） |

**无未覆盖的 E2E。**

### Reverse: US → Task

| US | Task(s) | ✓ |
|----|---------|---|
| US-01 | T1 | ✓ |
| US-02 | T3, T4 | ✓ |
| US-03 | T3 | ✓ |
| US-04 | T2 | ✓ |
| US-05 | T6 | ✓ |
| US-06 | T4 | ✓ |
| US-07 | T4 | ✓ |
| US-08 | T1 (+ T3 集成) | ✓ |
| US-09 | T5 | ✓ |
| US-10 | T5 | ✓ |
| US-11 | T3, T6 | ✓ |

**全部 11 个 user story 有对应 task。**

---

## 5. Risk Section

| 风险 | 级别 | 缓解 |
|------|------|------|
| **mockAppStore 接口扩展破坏现有 auth 测试** | 中 | T1 DD-12 策略：新方法零值默认返回，auth 测试不调新方法不受影响。T1 验证 `go test ./internal/cli/ -run TestAuth` 全绿。 |
| **ipatool Download/ReplicateSinf 在真实 Apple 环境的行为未验证** | 中 | 自动化测试全 mock；validate 阶段手动 live Apple 验收（E2E-008/020）。design 已从源码实证 API 签名。 |
| **library/index.json 并发写** | 低 | v1 单进程，同 profile 并发未定义（R3 继承）。原子写（tmp+rename）保证不损坏。 |
| **`apps.go` → `app.go` rename 可能遗漏引用** | 低 | T1 acceptance 含 `go build ./... + go vet ./...`；编译期捕获所有引用。 |
| **progressbar 转 direct dep 可能引入传递依赖变化** | 低 | 已在 go.sum；`go mod tidy` 自动处理。 |
| **多版本索引 JSON 无限增长** | 低 | 用户通过 `library clean` 手动管理；v1 不做自动清理（Non-goals）。 |
| **--external-version-id 下载历史版本 Apple 可能不支持** | 中 | AC-09-2 处理 Apple 返回的错误；validate 阶段手动验证。 |

---

## 6. Pre-Execution Baseline

```
Branch: feature/download-ipa-by-account
Git:    clean (no uncommitted changes)
Build:  go build ./...     → PASS
Test:   go test ./...       → PASS (account, appstore, cli packages)
Vet:    go vet ./...        → PASS
```

---

## 7. Decision-Completeness Declaration

本计划 decision-complete：execution 阶段无需发明架构、接口、任务顺序或验证策略。
- 每个任务的文件清单、测试范围、acceptance command、完成标准均已明确。
- 依赖图确定并行/串行关系。
- 追溯矩阵覆盖全部 47 AC / 11 US / 50 E2E。
- 风险均有缓解措施。
