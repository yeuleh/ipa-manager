# Plan — fix-ipatool-auth

## 1. Implementation Context

| Item                  | Value                                                                                  |
| --------------------- | -------------------------------------------------------------------------------------- |
| **Runtime/Language**    | Go ≥ 1.26                                                                               |
| **Key dependencies**    | `github.com/majd/ipatool/v2` (being replaced by fork `github.com/yeuleh/ipatool/v2`)   |
| **Testing framework**   | Go standard `testing` package                                                          |
| **External tooling**    | `gh` CLI (GitHub fork creation), `git` (cherry-pick, tag)                              |
| **Constraint**          | No application code changes — only `go.mod` + `go.sum` (DD-04)                         |
| **Spike workspace**     | `./temp/ipatool-spike/` (branch `pr-493`) — source for verification, cleaned at end     |

## 2. Dependency Graph

```
T1 (Fork + patch + tag + document)
  │
  ▼
T2 (go.mod integrate + automated verification)
  │
  ▼
T3 (Real E2E: login + list + logout)
  │
  ▼
T4 (Switch-back proof + security audit + cleanup)
```

**All tasks are strictly sequential** — each depends on the previous task's output:
- T2 needs the tagged fork from T1.
- T3 needs the integrated binary from T2.
- T4 needs all prior work complete to audit and prove.

No parallel tracks. No Foundation tasks (T1 is a story task — it produces the core deliverable).

## 3. Task List

### T1 — Create fork, apply PR #493, tag, document

| Field                    | Value                                                                 |
| ------------------------ | --------------------------------------------------------------------- |
| **User Stories**           | US-02                                                                 |
| **Acceptance Criteria**    | AC-02-3 (partial: fork exists at immutable tag)                       |
| **E2E Cases**              | E2E-010                                                               |
| **NFRs**                   | NFR-03 (fork documentation)                                           |
| **Blocked by**             | —                                                                     |
| **Blocks**                | T2, T3, T4                                                            |

**Files to create/modify** (external — in `github.com/yeuleh/ipatool`):
- Fork repo created from `majd/ipatool`
- `main` branch with PR #493 cherry-picked (commit `a98f833`)
- `FORK_NOTES.md` at fork root
- Tag `v2.3.1-fix-auth.5`

**Steps**:
1. Fork `majd/ipatool` to `yeuleh/ipatool` via `gh repo fork` or GitHub UI.
2. Clone fork locally (or reuse spike workspace `./temp/ipatool-spike`).
3. `git cherry-pick a98f833` (PR #493 — trivial conflict in `appstore_bag_test.go` test additions, resolved by accepting theirs).
4. `go generate ./...` (regenerate mocks).
5. `go build ./...` — verify exit 0.
6. `go test ./...` — verify all pass.
7. Write `FORK_NOTES.md` with: base commit `majd/ipatool@v2.3.0`, applied PR #493 commit `a98f833`, purpose, sync procedure.
8. `git add FORK_NOTES.md && git commit -m "docs: FORK_NOTES — fork provenance and sync procedure"`
9. `git tag v2.3.1-fix-auth.5`
10. `git push origin main --tags`

**Acceptance command**:
```bash
# Local verification
cd ./temp/ipatool-spike && go build ./... && go test ./... && git tag --list v2.3.1-fix-auth.5 && test -f FORK_NOTES.md
# Remote verification (tag pushed and fetchable)
cd ./temp/ipatool-spike && git ls-remote --tags origin v2.3.1-fix-auth.5 | grep v2.3.1-fix-auth.5
# Go module fetchability (from ipa-manager repo)
GOPROXY=direct go mod download github.com/yeuleh/ipatool/v2@v2.3.1-fix-auth.5 && echo "fetchable"
```

**Completion criteria**:
- Spock review pass (fork has correct base + patch + tag + docs).
- `go build ./...` and `go test ./...` pass in the fork.
- Tag `v2.3.1-fix-auth.5` exists.
- `FORK_NOTES.md` exists and records all three items (base commit, applied PR, sync procedure).

**Rollback note**: The fork repo is external. If created incorrectly, delete it on GitHub and recreate. No ipa-manager state is affected at this point.

---

### T2 — Integrate fork into ipa-manager + automated verification

| Field                    | Value                                                                 |
| ------------------------ | --------------------------------------------------------------------- |
| **User Stories**           | US-02                                                                 |
| **Acceptance Criteria**    | AC-02-1, AC-02-2, AC-02-3                                             |
| **E2E Cases**              | E2E-001, E2E-002, E2E-003, E2E-004                                    |
| **NFRs**                   | NFR-01 (cross-compile), NFR-02 (immutable ref), NFR-05 (no regression) |
| **Blocked by**             | T1                                                                    |
| **Blocks**                | T3                                                                    |

**Files to modify** (in ipa-manager repo):
- `go.mod` — add replace directive
- `go.sum` — updated by `go mod tidy`

**Steps**:
1. Edit `go.mod`: add `replace github.com/majd/ipatool/v2 => github.com/yeuleh/ipatool/v2 v2.3.1-fix-auth.5`
2. `go mod tidy` — updates go.sum with fork checksums.
3. `go build ./...` — must succeed (E2E-001).
4. `go build -o ./bin/ipa-manager ./cmd/ipa-manager` — build binary for T3.
5. `go test ./... -count=1` — all 69 tests pass (E2E-002).
6. `GOARCH=arm64 go build ./...` — cross-compile arm64 (E2E-004).
7. `GOARCH=amd64 go build ./...` — cross-compile amd64 (E2E-004).
8. Verify go.mod replace directive format is correct (E2E-003).

**Acceptance commands**:
```bash
go build ./... && \
go test ./... -count=1 && \
GOARCH=arm64 go build ./... && \
GOARCH=amd64 go build ./... && \
grep "replace.*yeuleh/ipatool" go.mod
```

**Completion criteria**:
- Spock review pass (go.mod correct, no app code changed).
- All acceptance commands exit 0.
- `git diff main...HEAD -- '*.go'` shows zero application code changes (only go.mod + go.sum).

**Rollback note**: Remove the replace directive from go.mod, run `go mod tidy`. Reverts to upstream v2.3.0. No persisted state affected.

---

### T3 — Real E2E verification (login + list + logout)

| Field                    | Value                                                                 |
| ------------------------ | --------------------------------------------------------------------- |
| **User Stories**           | US-01                                                                 |
| **Acceptance Criteria**    | AC-01-1, AC-01-2, AC-01-3                                             |
| **E2E Cases**              | E2E-005, E2E-006, E2E-007                                             |
| **NFRs**                   | —                                                                     |
| **Blocked by**             | T2                                                                    |
| **Blocks**                | T4                                                                    |

**Files to modify**: None (verification only).

**Steps**:
1. Ensure keychain is clean (no stale ipatool account): `./bin/ipa-manager auth info` should return "not found".
2. Run `./bin/ipa-manager auth login` interactively (E2E-005):
   - Enter Apple ID email.
   - Enter password.
   - Enter 2FA code.
   - Verify: login succeeds, account info displayed.
3. Run `./bin/ipa-manager accounts list` (E2E-006):
   - Verify: profile shows as logged-in with correct name.
4. Run `./bin/ipa-manager auth logout` (E2E-007):
   - Verify: logout succeeds.
5. Run `./bin/ipa-manager accounts list` again:
   - Verify: profile shows as logged-out.

**Acceptance commands** (manual interactive — record actual output):
```bash
# E2E-005: real login (interactive — enter email/password/2FA)
./bin/ipa-manager auth login
# Expected: success=true, account info displayed

# E2E-006: list shows logged-in profile
./bin/ipa-manager accounts list
# Expected: profile shows as logged-in with correct name

# E2E-007: logout revokes session
./bin/ipa-manager auth logout
./bin/ipa-manager accounts list
# Expected: logout succeeds, profile shows logged-out
```

**Completion criteria**:
- Spock review pass (E2E results documented in validate.md).
- All three E2E cases pass with real Apple servers.
- Results recorded with actual command output.

**Rollback note**: `./bin/ipa-manager auth logout` cleans up the session. No persisted state beyond keychain (which logout removes).

---

### T4 — Switch-back proof + security audit + cleanup

| Field                    | Value                                                                 |
| ------------------------ | --------------------------------------------------------------------- |
| **User Stories**           | US-03                                                                 |
| **Acceptance Criteria**    | AC-03-1                                                               |
| **E2E Cases**              | E2E-008, E2E-009                                                      |
| **NFRs**                   | NFR-04 (no personal data)                                             |
| **Blocked by**             | T3                                                                    |
| **Blocks**                | —                                                                     |

**Files to modify**: None (verification + cleanup).

**Steps**:
1. **Switch-back proof** (E2E-008):
   - Temporarily change replace directive to `github.com/majd/ipatool/v2 v2.3.0`.
   - `go build ./...` — must succeed.
   - `go test ./... -count=1` — all 69 tests must pass.
   - Restore fork replace directive.
   - `go mod tidy` to restore correct go.sum.
2. **Security audit** (E2E-009):
   - Part A: `git diff main...feature/fix-ipatool-auth` — audit diff body for personal data.
   - Part B: diff fork main vs upstream v2.3.0 — audit for personal data beyond PR #493 content.
   - Verify: no Apple credentials, names, tokens, or 2FA codes in diff bodies.
3. **Cleanup**:
   - Remove `./temp/ipatool-spike/` (per AGENTS.md CONVENTIONS — temp cleanup after task).
   - Verify `./temp/` is empty or removed.

**Acceptance commands**:
```bash
# Switch-back proof (E2E-008)
sed -i.bak 's|yeuleh/ipatool/v2 v2.3.1-fix-auth.5|majd/ipatool/v2 v2.3.0|' go.mod && \
go build ./... && go test ./... -count=1 && \
mv go.mod.bak go.mod && go mod tidy

# Security audit Part A — ipa-manager diff (E2E-009 Part A)
git diff main...feature/fix-ipatool-auth

# Security audit Part B — fork diff vs upstream (E2E-009 Part B)
cd ./temp/ipatool-spike && git diff 19ffd1b...HEAD

# Cleanup
rm -rf ./temp/
```

**Bundling justification**: Switch-back proof (E2E-008), security audit (E2E-009), and temp cleanup are all post-implementation validation/housekeeping activities with no code changes. Each takes < 5 minutes. Splitting would create sub-task-sized units (e.g., "diff audit + rm -rf") that don't warrant independent Spock review. They are bundled as the mission's final validation gate.

**Completion criteria**:
- Spock review pass (switch-back proved, audit clean, temp cleaned).
- Build + test pass with upstream v2.3.0 (proving no fork-specific API dependency).
- No personal data in diff bodies of either repo.
- `./temp/` cleaned up.

**Rollback note**: If switch-back test leaves go.mod in wrong state, restore from git: `git checkout go.mod go.sum`.

## 4. Traceability Matrices

### US/AC → Task

| User Story | Acceptance Criterion | Task   |
| ---------- | -------------------- | ------ |
| US-01      | AC-01-1              | T3     |
| US-01      | AC-01-2              | T3     |
| US-01      | AC-01-3              | T3     |
| US-02      | AC-02-1              | T2     |
| US-02      | AC-02-2              | T2     |
| US-02      | AC-02-3              | T1, T2 |
| US-03      | AC-03-1              | T4     |

### NFR → Task

| NFR    | Task |
| ------ | ---- |
| NFR-01 | T2   |
| NFR-02 | T2   |
| NFR-03 | T1   |
| NFR-04 | T4   |
| NFR-05 | T2   |

### E2E → Task

| E2E Case | Task |
| -------- | ---- |
| E2E-001  | T2   |
| E2E-002  | T2   |
| E2E-003  | T2   |
| E2E-004  | T2   |
| E2E-005  | T3   |
| E2E-006  | T3   |
| E2E-007  | T3   |
| E2E-008  | T4   |
| E2E-009  | T4   |
| E2E-010  | T1   |

### Reverse Coverage: US → Task

| User Story | Tasks      | Covered? |
| ---------- | ---------- | -------- |
| US-01      | T3         | ✅       |
| US-02      | T1, T2     | ✅       |
| US-03      | T4         | ✅       |

All user stories have at least one task. ✅

## 5. Risk Section

| Risk                                          | Task    | Mitigation                                                                                         |
| --------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------- |
| PR #493 doesn't apply cleanly to v2.3.0    | T1      | Spike-verified clean application. If conflict, resolve manually or rebase onto PR parent.          |
| `go mod tidy` fails (checksum/version issues) | T2      | Verify fork tag is valid semver. `go mod download` + retry.                                         |
| Real login fails (Apple changed again)        | T3      | Re-run spike with latest community PRs. Check if Apple endpoint moved further.                     |
| Switch-back build fails (fork-specific API)   | T4      | Should not happen — adapter isolates ipatool. If it does, identify the API leak and refactor.       |
| Fork accidentally deleted on GitHub           | Any     | Local backup in git history. Re-creatable from upstream + cherry-pick.                             |
| Personal data accidentally committed          | T4      | E2E-009 audit catches it. Requirements redacted per Spock review.                                  |

## 6. Pre-Execution Baseline

Captured at commit `16d5b47` on `feature/fix-ipatool-auth`:

| Check                         | Result                                                |
| ----------------------------- | ----------------------------------------------------- |
| Git branch                    | `feature/fix-ipatool-auth`                             |
| Working tree                  | Clean (`git status --porcelain` = empty)               |
| `go build ./...`              | ✅ Pass (exit 0)                                       |
| `go test ./... -count=1`      | ✅ 69 tests, 0 failures (account: 32, appstore: 6, cli: 31) |
| Spike workspace               | `./temp/ipatool-spike/` exists (branch `pr-493`, build + tests verified) |
| `.gitignore` includes `/temp/` | ✅                                                     |

## 7. Decision-Completeness Declaration

This plan is decision-complete. Execution can proceed without inventing:
- **Architecture**: Fork + replace directive (DD-01 through DD-06 in design.md).
- **Interfaces**: No interface changes (DD-04 — adapter isolation).
- **Task order**: Strictly sequential T1 → T2 → T3 → T4.
- **Validation strategy**: 10 E2E cases mapped to tasks, acceptance commands specified.
