# E2E Test Plan — fix-ipatool-auth

## 1. Test Scope

Validate that swapping the ipatool dependency to a forked version with PR #493 applied:
1. Fixes real Apple login (US-01).
2. Maintains a reproducible build (US-02).
3. Enables a clean switch-back path (US-03).
4. Introduces no regressions (NFR-05).

## 2. Environment Prerequisites

| Prerequisite             | Value                                                              |
| ------------------------ | ------------------------------------------------------------------ |
| OS                       | macOS (Apple Silicon or Intel)                                     |
| Go version               | ≥ 1.26                                                             |
| Repo                     | `feature/fix-ipatool-auth` branch, clean working tree              |
| GitHub fork              | `github.com/yeuleh/ipatool` created, tagged `v2.3.1-fix-auth.5`     |
| Apple ID                 | A valid Apple ID with 2FA enabled (test account)                   |
| Keychain                 | Clean (no stale ipatool account entries from previous attempts)    |
| Network                  | Internet access to `auth.itunes.apple.com`                         |

## 3. Validation Oracles

| Oracle                  | How to observe                                                                  |
| ----------------------- | ------------------------------------------------------------------------------- |
| Build success           | `go build ./...` exit code 0                                                    |
| Test success            | `go test ./... -count=1` exit code 0, all tests pass                            |
| go.mod replace format   | `grep "replace.*ipatool" go.mod` — must show fork at immutable ref             |
| Cross-compile           | `GOARCH=arm64 go build ./...` and `GOARCH=amd64 go build ./...` exit 0          |
| Real login              | `./bin/ipa-manager auth login` interactive — returns `success=true`            |
| Account state           | `./bin/ipa-manager accounts list` — shows logged-in profile                     |
| Logout                  | `./bin/ipa-manager auth logout` — session revoked                               |
| Switch-back proof       | Temporarily replace fork module → build + test still pass                       |
| No personal data        | `git log -p --all` on ipa-manager repo — no emails, names, tokens in any commit |

## 4. E2E Test Cases

### E2E-001 — Build succeeds with forked dependency (happy)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-001                                        |
| **Type**       | happy                                          |
| **US/AC**      | US-02 / AC-02-1                                |
| **Preconditions** | E2E-001 passed (go build succeeds); fork tagged and pushed |

**Actions**:
1. `go build -o ./bin/ipa-manager ./cmd/ipa-manager`

**Expected**: Binary created at `./bin/ipa-manager`, exit code 0, no errors.

**Pass/Fail**: Exit 0 = PASS.

---

### E2E-002 — All existing tests pass (regression)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-002                                        |
| **Type**       | regression                                     |
| **US/AC**      | US-02 / AC-02-2, NFR-05                        |
| **Preconditions** | E2E-001 passed                                 |

**Actions**:
1. `go test ./... -count=1`

**Expected**: All packages pass. Total: 69 tests (or more if new tests added), 0 failures.

**Pass/Fail**: Exit 0 = PASS.

---

### E2E-003 — go.mod replace directive is correct (artifact)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-003                                        |
| **Type**       | happy                                          |
| **US/AC**      | US-02 / AC-02-3, NFR-02                        |
| **Preconditions** | go.mod updated                                 |

**Actions**:
1. Read go.mod, locate the `replace` directive for ipatool.
2. Verify: `replace github.com/majd/ipatool/v2 => github.com/yeuleh/ipatool/v2 v2.3.1-fix-auth.5`
3. Verify the ref is a tag or 40-char SHA (not a branch name).

**Expected**: Replace directive present, points to fork at immutable tag `v2.3.1-fix-auth.5`.

**Pass/Fail**: Directive present and ref is immutable = PASS.

---

### E2E-004 — Cross-compile for arm64 and amd64 (NFR)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-004                                        |
| **Type**       | NFR                                            |
| **US/AC**      | NFR-01                                         |
| **Preconditions** | E2E-001 passed                                 |

**Actions**:
1. `GOARCH=arm64 go build ./...`
2. `GOARCH=amd64 go build ./...`

**Expected**: Both exit code 0.

**Pass/Fail**: Both succeed = PASS.

---

### E2E-005 — Real auth login succeeds (happy — primary acceptance)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-005                                        |
| **Type**       | happy                                          |
| **US/AC**      | US-01 / AC-01-1                                |
| **Preconditions** | Binary built (`./bin/ipa-manager` via E2E-001); keychain clean; valid Apple ID with 2FA |

**Actions**:
1. `./bin/ipa-manager auth login`
2. Enter Apple ID email when prompted.
3. Enter password when prompted.
4. Enter 2FA code when prompted.

**Expected**: Login succeeds. Account info (email, name) displayed. No error.

**Pass/Fail**: `success=true` (or equivalent) displayed = PASS.

---

### E2E-006 — accounts list shows logged-in profile (happy)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-006                                        |
| **Type**       | happy                                          |
| **US/AC**      | US-01 / AC-01-2                                |
| **Preconditions** | E2E-005 passed (login succeeded)              |

**Actions**:
1. `./bin/ipa-manager accounts list`

**Expected**: The profile for the logged-in account appears, marked as logged-in, with the correct name from Apple.

**Pass/Fail**: Profile shown, status = logged-in = PASS.

---

### E2E-007 — auth logout revokes session (happy)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-007                                        |
| **Type**       | happy                                          |
| **US/AC**      | US-01 / AC-01-3                                |
| **Preconditions** | E2E-005 passed (session active)               |

**Actions**:
1. `./bin/ipa-manager auth logout`
2. `./bin/ipa-manager accounts list`

**Expected**: Logout completes without error. `accounts list` shows the profile as logged-out.

**Pass/Fail**: Logout succeeds + profile shows logged-out = PASS.

---

### E2E-008 — Switch-back compatibility proof (design)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-008                                        |
| **Type**       | regression                                     |
| **US/AC**      | US-03 / AC-03-1                                |
| **Preconditions** | E2E-001, E2E-002 passed                        |

**Actions**:
1. Temporarily change the replace directive to point to upstream `majd/ipatool/v2 v2.3.0` (the version without the fix).
2. `go build ./...`
3. `go test ./... -count=1`
4. Restore the fork replace directive.

**Expected**: Build succeeds and all tests pass with the upstream module — proving ipa-manager depends only on ipatool's public API, not fork-specific internals. (This proves build/test compatibility only; login runtime behavior with v2.3.0 is not asserted here.)

**Pass/Fail**: Build + test pass with upstream module = PASS. Restore fork directive after.

---

### E2E-009 — No personal data in repositories (security)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-009                                        |
| **Type**       | NFR                                            |
| **US/AC**      | NFR-04                                         |
| **Preconditions** | All commits made on both repos                 |

**Actions**:

**Part A — ipa-manager repo**:
1. Audit introduced diff content on `feature/fix-ipatool-auth`: `git diff main...feature/fix-ipatool-auth`
2. Check the diff (not git author metadata) for: Apple ID emails, account holder names, tokens, passwords, 2FA codes.

**Part B — fork repo (`yeuleh/ipatool`)**:
1. Audit introduced diff content: diff between fork main and upstream v2.3.0 (should be only PR #493 + `FORK_NOTES.md`).
2. Check for any personal data beyond what PR #493 originally contains.

**Scope note**: Git author/committer metadata (name/email in commit headers) is excluded from this audit — it is unavoidable commit attribution, not leaked Apple credential data. Only diff body content is checked.

**Expected**: No Apple credentials, personal account data, tokens, or 2FA codes in any diff body in either repo.

**Pass/Fail**: No personal data in diff bodies of either repo = PASS.

---

### E2E-010 — Fork documentation exists (maintainability)

| Field          | Value                                          |
| -------------- | ---------------------------------------------- |
| **ID**         | E2E-010                                        |
| **Type**       | NFR                                            |
| **US/AC**      | NFR-03                                         |
| **Preconditions** | Fork repo created                               |

**Actions**:
1. Check `github.com/yeuleh/ipatool` for `FORK_NOTES.md`.
2. Verify it records: (a) base commit, (b) applied PR, (c) sync procedure.

**Expected**: `FORK_NOTES.md` exists with all three items.

**Pass/Fail**: Document exists and is accurate = PASS.

## 5. Traceability Matrix

| E2E Case  | User Story | Acceptance Criterion | NFR    | Type       |
| --------- | ---------- | -------------------- | ------ | ---------- |
| E2E-001   | US-02      | AC-02-1              | —      | happy      |
| E2E-002   | US-02      | AC-02-2              | NFR-05 | regression |
| E2E-003   | US-02      | AC-02-3              | NFR-02 | happy      |
| E2E-004   | —          | —                    | NFR-01 | NFR        |
| E2E-005   | US-01      | AC-01-1              | —      | happy      |
| E2E-006   | US-01      | AC-01-2              | —      | happy      |
| E2E-007   | US-01      | AC-01-3              | —      | happy      |
| E2E-008   | US-03      | AC-03-1              | —      | regression |
| E2E-009   | —          | —                    | NFR-04 | NFR        |
| E2E-010   | —          | —                    | NFR-03 | NFR        |

### Reverse Coverage Check

| User Story | Covered by E2E cases      |
| ---------- | ------------------------- |
| US-01      | E2E-005, E2E-006, E2E-007 |
| US-02      | E2E-001, E2E-002, E2E-003 |
| US-03      | E2E-008                   |

All user stories have at least one E2E case. ✅

### AC Coverage Check

| AC      | Covered by   |
| ------- | ------------ |
| AC-01-1 | E2E-005      |
| AC-01-2 | E2E-006      |
| AC-01-3 | E2E-007      |
| AC-02-1 | E2E-001      |
| AC-02-2 | E2E-002      |
| AC-02-3 | E2E-003      |
| AC-03-1 | E2E-008      |

All 7 acceptance criteria covered. ✅

### NFR Coverage Check

| NFR    | Covered by   |
| ------ | ------------ |
| NFR-01 | E2E-004      |
| NFR-02 | E2E-003      |
| NFR-03 | E2E-010      |
| NFR-04 | E2E-009      |
| NFR-05 | E2E-002      |

All 5 NFRs covered. ✅
