# Design — fix-ipatool-auth

## 1. Goals & Non-Goals

### Goals

Satisfy these user stories from requirements.md:

- **US-01** (P1): `auth login` authenticates against live Apple servers.
- **US-02** (P1): Reproducible build via a tagged fork.
- **US-03** (P2): Clean switch-back path to upstream.

### Non-Goals (design constraints)

- No ipa-manager application code changes (the `ProfileAppStore` adapter isolates ipatool types — A-03).
- No additional PRs beyond #493 (spike verified sufficiency — A-01).
- No fork CI/CD pipeline.
- No changes to failure-path behavior (A-05 — out of scope, unchanged from previous mission).

## 2. Architecture & Key Decisions

### Component Overview

```
┌─────────────────────────────────────────────────────┐
│                  ipa-manager (Go)                    │
│                                                      │
│  internal/cli  ──▶  internal/appstore (adapter)      │
│                          │                           │
│                    interface ProfileAppStore         │
│                          │                           │
│                    profileAppStoreAdapter             │
│                          │  ← ONLY import boundary   │
│                          ▼                           │
│              github.com/yeuleh/ipatool/v2  (FORK)    │
│              └─ PR #493 applied (auth endpoint fix)  │
│                          │                           │
│                    Apple App Store API               │
└─────────────────────────────────────────────────────┘
```

The only change in this mission is the dependency source: `majd/ipatool/v2` → `yeuleh/ipatool/v2`. The adapter layer (`internal/appstore/`) is unchanged.

### Decision Record

#### DD-01: Fork from upstream main HEAD, not v2.3.0 tag

**Decision**: Fork from `majd/ipatool` main HEAD (`dcddce4`, 2026-05-26), then cherry-pick PR #493.

**Rationale**: Main HEAD includes post-v2.3.0 fixes (e.g., commit `dcddce4` "Fix 429 rate limit handling and context key type mismatch" from PR #481). Starting from the latest main gives us those fixes for free. PR #493 applies cleanly to main (verified during spike — build + tests pass).

**Alternative considered**: Fork from v2.3.0 tag. Rejected — would miss the 429 fix and require more manual patches later.

#### DD-02: Tag as v2.3.1-fix-auth.1

**Decision**: Tag the fork's patched commit as `v2.3.1-fix-auth.1`.

**Rationale**:
- Semver-compatible (Go modules require valid semver for tagged releases).
- `v2.3.1` signals it's post-v2.3.0.
- `-fix-auth.1` pre-release suffix clearly identifies the purpose.
- If we need to revise (e.g., add PR #500), we increment to `.2`.

**Alternative considered**: Pseudo-version `v2.3.1-0.20260630000000-abcdef`. Rejected — unreadable, though valid. A named tag is more maintainable.

#### DD-03: go.mod replace directive format

**Decision**:
```go
replace github.com/majd/ipatool/v2 => github.com/yeuleh/ipatool/v2 v2.3.1-fix-auth.1
```

**Rationale**:
- `replace` redirects the module path to the fork.
- Points to an immutable tag (NFR-02), not a branch.
- All ipa-manager imports (`github.com/majd/ipatool/v2/...`) automatically resolve to the fork.

**Alternative considered**: Change all import paths to `github.com/yeuleh/ipatool/v2`. Rejected — would require editing every file that imports ipatool, and breaks the clean switch-back path (US-03).

#### DD-04: No application code changes

**Decision**: Only `go.mod` and `go.sum` change in the ipa-manager repo. No `.go` files are modified.

**Rationale**: The `ProfileAppStore` adapter (built in the previous mission, `internal/appstore/adapter.go` + `client_impl.go`) imports ipatool types only inside the adapter package. The `replace` directive transparently redirects these imports to the fork. No source-level changes needed.

**Verification**: `git diff` after the swap should show only `go.mod` + `go.sum`.

#### DD-05: Fork documentation location

**Decision**: Add a `FORK_NOTES.md` at the root of the fork repo (`yeuleh/ipatool`), and reference it from ipa-manager's `docs/features/fix-ipatool-auth/`.

**Contents**:
- Base commit: `majd/ipatool@dcddce4`
- Applied patch: PR #493 commit `a98f833`
- Purpose: Apple auth endpoint fix
- Sync procedure: when upstream merges an equivalent fix, rebase onto upstream main and re-tag

**Rationale**: NFR-03 requires documenting the fork's provenance and sync procedure. Placing it in the fork repo (not ipa-manager) makes it discoverable by anyone who encounters the fork.

## 3. Data Models / State / Interfaces

**N/A — no changes.**

The ipa-manager application's data models, state, and interfaces are unchanged. The dependency swap is transparent to all application code. The `ProfileAppStore` interface, `LoginInput`, `LoginResult` types, and all CLI state machines remain identical.

## 4. Code Structure

### Files Modified

| File         | Change                                                                                       |
| ------------ | -------------------------------------------------------------------------------------------- |
| `go.mod`     | Add `replace github.com/majd/ipatool/v2 => github.com/yeuleh/ipatool/v2 v2.3.1-fix-auth.1`    |
| `go.sum`     | Updated by `go mod tidy` (new checksums for the fork)                                         |

### Files NOT Modified (important)

| File / Package                     | Reason                                                                 |
| ---------------------------------- | ---------------------------------------------------------------------- |
| `internal/appstore/adapter.go`     | Interface unchanged — fork has same public API as upstream             |
| `internal/appstore/client_impl.go` | Implementation unchanged — ipatool imports resolve via replace         |
| `internal/appstore/factory.go`     | Factory unchanged — same `ipatool.New()` call                          |
| `internal/cli/auth.go`             | CLI commands unchanged — depend on adapter interface, not ipatool directly |
| `internal/account/*`              | Account management unchanged                                           |
| `internal/ui/*`                    | UI prompts unchanged                                                   |

### External Artifacts Created (not in ipa-manager repo)

| Artifact                          | Location                     |
| --------------------------------- | ---------------------------- |
| GitHub fork                       | `github.com/yeuleh/ipatool`  |
| Patched main branch               | fork's `main`                |
| Tag                               | `v2.3.1-fix-auth.1`          |
| Fork documentation                | `FORK_NOTES.md` in fork root |

## 5. Processing Flows

### Happy Path: Fork Creation + Integration

```
┌─ Fork Creation (external) ──────────────────────────────────────┐
│                                                                  │
│  1. gh repo fork majd/ipatool --clone=false                      │
│     → creates github.com/yeuleh/ipatool                          │
│                                                                  │
│  2. Clone fork locally (or use spike workspace)                  │
│                                                                  │
│  3. Cherry-pick PR #493 (commit a98f833) onto main               │
│     git cherry-pick a98f833                                      │
│     → applies cleanly (spike-verified)                           │
│                                                                  │
│  4. Regenerate mocks (go generate ./...)                         │
│                                                                  │
│  5. Verify: go build ./... && go test ./...                      │
│     → all pass (spike-verified)                                 │
│                                                                  │
│  6. Write FORK_NOTES.md                                          │
│                                                                  │
│  7. Commit + tag: git tag v2.3.1-fix-auth.1                      │
│                                                                  │
│  8. Push fork to github.com/yeuleh/ipatool                       │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─ Integration (ipa-manager repo) ─────────────────────────────────┐
│                                                                  │
│  9.  Edit go.mod: add replace directive                          │
│                                                                  │
│  10. go mod tidy                                                 │
│      → updates go.sum with fork checksums                         │
│                                                                  │
│  11. go build ./...                                              │
│      → must succeed                                              │
│                                                                  │
│  12. go test ./... -count=1                                      │
│      → 69 tests, 0 failures                                      │
│                                                                  │
│  13. go build -o ./bin/ipa-manager ./                             │
│                                                                  │
│  14. ./bin/ipa-manager auth login (interactive, real Apple ID)   │
│      → success=true                                              │
│                                                                  │
│  15. ./bin/ipa-manager accounts list                             │
│      → shows logged-in profile                                   │
│                                                                  │
│  16. ./bin/ipa-manager auth logout                               │
│      → session revoked                                           │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### Failure Path: PR #493 doesn't apply cleanly to main HEAD

```
git cherry-pick a98f833 → CONFLICT
  │
  ├─ If trivial conflict (context drift) → resolve manually, continue
  │
  └─ If structural conflict → checkout a98f833's parent, apply onto main via rebase
     → re-test build + tests
     → if still fails → ESCALATE (NEEDS CLARIFICATION: combine with PR #502?)
```

**Likelihood**: LOW — spike verified clean application on main HEAD.

### Failure Path: go mod tidy fails

```
go mod tidy → error
  │
  ├─ "missing go.sum entry" → go mod download, retry
  │
  └─ "incompatible version" → check fork tag is valid semver, re-tag if needed
```

### Failure Path: Real login fails after integration

```
./bin/ipa-manager auth login → error
  │
  ├─ Same plist decode error → PR #493 not actually applied → verify fork diff
  │
  ├─ Different Apple error → Apple changed again → spike with latest community PRs
  │
  └─ Network/credential error → not a dependency issue → retry
```

### Switch-Back Path (when upstream merges fix)

```
1. Remove replace directive from go.mod
2. Update require to upstream version with the fix (e.g., v2.4.0)
3. go mod tidy
4. go build + go test
5. Verify real login
6. Optional: archive or delete fork
```

## 6. Impact Analysis

| Concern               | Impact Assessment                                                                                                                              |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| **Compatibility**     | LOW RISK. Only go.mod/go.sum change. Binary interface unchanged. Existing config files (`~/.ipa-manager/config.json`) and keychain entries are fully compatible. |
| **Migration**         | N/A. No data migration needed. Users who could never log in before simply gain the ability.                                                      |
| **Security**          | LOW RISK. Fork is public, contains only MIT-licensed open-source code. PR #493 adds endpoint-discovery logic — no credential handling changes. NFR-04: no personal data committed. |
| **Performance**       | NONE. Same codebase, same algorithms. The only runtime difference is hitting the correct Apple endpoint (which may be marginally faster due to fewer failed retries). |
| **Reliability**       | IMPROVED. Login stops failing. The retry-with-discovered-endpoint logic (PR #493) adds resilience against future endpoint changes.              |
| **Observability**     | NONE. No logging changes in ipa-manager. ipatool's own logging is unchanged.                                                                     |
| **Rollout**           | N/A. Personal tool, single user. No phased rollout needed. Build the new binary and use it.                                                     |
| **Rollback**          | TRIVIAL. Remove the replace directive from go.mod → revert to upstream v2.3.0. One-line change, no side effects.                                |
| **Maintainability**   | MODERATE COST. Fork must be synced with upstream periodically. Mitigated by FORK_NOTES.md documenting the sync procedure (NFR-03). When upstream merges the fix, switch back (US-03). |

## 7. Validation Strategy

See `e2e_test.md` for the complete test matrix.

### Test Pyramid

| Level       | Scope                                                           | Count |
| ----------- | --------------------------------------------------------------- | ----- |
| **Unit**    | Existing 69 tests — must pass with no regression (NFR-05)       | 69    |
| **Build**   | go build, cross-compile arm64/amd64 (NFR-01)                    | 3     |
| **E2E**     | Real auth login + accounts list + auth logout (US-01)           | 3     |
| **Artifact**| go.mod replace directive format, go.sum integrity (US-02)      | 2     |
| **Design**  | Switch-back compatibility proof (US-03)                         | 1     |

### Key Validation Principles

1. **No new unit tests** — the dependency swap doesn't change any application code. Existing tests prove no regression.
2. **Real E2E is the primary acceptance signal** — automated tests can't verify Apple's live servers.
3. **Switch-back proof** — temporarily replacing the fork with an API-equivalent module proves US-03 without waiting for upstream.

## 8. Risk Register

| Risk ID | Risk                                              | Likelihood | Impact | Mitigation                                                                     |
| ------- | ------------------------------------------------- | ---------- | ------ | ------------------------------------------------------------------------------ |
| R1      | Apple changes endpoint again before/after release | LOW        | HIGH   | PR #493 includes dynamic discovery + retry logic, resilient to endpoint moves. |
| R2      | Upstream merges a different, incompatible fix      | LOW        | MEDIUM | Fork's public API is identical to upstream; replace can swap to either.        |
| R3      | GitHub fork gets deleted accidentally             | LOW        | HIGH   | Fork is backed up locally (spike workspace + git history). Re-creatable.       |
| R4      | go.sum checksum mismatch on fresh clone           | LOW        | MEDIUM | `go mod tidy` + `go mod verify` in validation.                                  |

> **Note**: R1 was the primary risk in the previous mission. PR #493's dynamic endpoint discovery directly mitigates it — this is a design-level improvement, not just a patch.

## 9. Open Questions

None. All high-impact unknowns were resolved by the spike.
