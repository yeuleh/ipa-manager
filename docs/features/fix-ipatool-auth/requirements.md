# Requirements — fix-ipatool-auth

## 1. Intent & Context

### Problem

Previous mission `multi-account-login-switch` delivered the full multi-account CLI (69 passing tests, 31/31 ACs verified). However, **real Apple login was blocked** because the dependency `ipatool v2.3.0` uses a stale authentication endpoint:

- Apple moved the auth endpoint from `urlBag.authEndpoint` to a root-level `authenticateAccount` field in the Bag response (~May 2026).
- ipatool v2.3.0 (released Feb 2026) does not handle this change; it sends login requests to the old endpoint, which returns non-plist content, causing a plist decode error (`unexpected hex digit 'h'` — the 'h' from "https://..." in Apple's redirect body).
- Upstream maintainer is unresponsive; 7 community PRs pending (#490–#504), none merged.

### Spike Result (2026-06-30)

PR #493 ("Fix dynamic App Store auth endpoint discovery", 4 approvals) was applied to ipatool master and **verified against live Apple servers**:

```
./ipatool-patched auth login --email yeuleh@gmail.com
→ email=yeuleh@gmail.com name="乐 庾" success=true
```

PR #493 fixes the root cause by:
1. Reading the new `authenticateAccount` field from the Bag response (root-level, not inside `urlBag`).
2. Normalizing the endpoint to `https://auth.itunes.apple.com/auth/v1/native/fast/`.
3. When plist decode fails, extracting the new endpoint URL from the response body and retrying.

### Desired Outcome

`ipa-manager auth login` works end-to-end against real Apple servers, using a forked ipatool dependency that includes PR #493.

## 2. Actors / Assumptions / Dependencies

### Actors

| Actor           | Description                                                     |
| --------------- | --------------------------------------------------------------- |
| ipa-manager user | Runs `auth login` / `accounts list` / `auth logout` locally     |
| Maintainer       | Syncs the fork with upstream when ipatool eventually merges the fix |

### Assumptions

- A-01: PR #493 is the sole fix needed for the auth endpoint issue (verified by spike — login succeeded with only this PR applied).
- A-02: GitHub account `yeuleh` has permission to create repos / forks.
- A-03: ipa-manager's `ProfileAppStore` adapter (built in previous mission) correctly isolates ipatool types — no application code changes needed, only `go.mod`.
- A-04: The 69 existing automated tests are unaffected by the dependency swap (they mock the AppStore interface, not the real ipatool client).

### Dependencies

- D-01: `github.com/yeuleh/ipatool/v2` — fork of `github.com/majd/ipatool/v2` with PR #493 applied.
- D-02: Spike workspace at `./temp/ipatool-spike/` (branch `pr-493`) — source for the fork.

## 3. Scope

### In Scope

- Create GitHub fork `yeuleh/ipatool` from `majd/ipatool`.
- Apply PR #493 fix to the fork's `main` branch.
- Tag the fork at the patched commit.
- Update ipa-manager `go.mod` with a `replace` directive pointing to the fork.
- Verify `go build` / `go test` / real `auth login` end-to-end.

### Out of Scope

- Modifying ipa-manager application code (the adapter handles isolation).
- Applying PR #500 (serialNumber / FailureType 5002) or PR #502 (Content-Type change) — PR #493 alone is verified sufficient.
- Building a CI pipeline for the fork.
- Fixing other ipatool bugs (search, download — not in the auth path).
- Long-term fork maintenance tooling.

### Non-goals

- Replacing ipatool with a different library.
- Reverse-engineering Apple's auth protocol independently.
- Supporting non-Apple-Region accounts beyond what ipatool supports.

## 4. User Stories

| ID    | Priority | Story                                                                                       | Rationale                                      |
| ----- | -------- | ------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| US-01 | P1       | As an ipa-manager user, I want `auth login` to authenticate against Apple, so that I can start managing my iOS apps. | Core unblock — the entire account flow depends on login working. |
| US-02 | P1       | As an ipa-manager user, I want a reproducible build, so that anyone cloning the repo gets a working binary. | A branch-pointing replace is non-reproducible; a tagged fork commit is durable. |
| US-03 | P2       | As a maintainer, I want to easily switch back to upstream ipatool when it merges the fix, so that I do not maintain a fork indefinitely. | Minimizes long-term maintenance burden.        |

### Priority Rationale

- US-01 / US-02 are P1: without them, the tool is non-functional and non-shareable.
- US-03 is P2: important for maintainability but not blocking initial functionality.

## 5. Acceptance Criteria

### US-01 — Real login works

- **AC-01-1**: Given ipa-manager is built with the patched dependency, When I run `ipa-manager auth login` and enter valid Apple credentials + 2FA code, Then login succeeds and the account info (email, name) is displayed.
- **AC-01-2**: Given AC-01-1 succeeded, When I run `ipa-manager accounts list`, Then the profile for the logged-in account shows as logged-in with the correct name.
- **AC-01-3**: Given AC-01-1 succeeded, When I run `ipa-manager auth logout`, Then the account session is revoked and `accounts list` shows the profile as logged-out.

### US-02 — Reproducible build

- **AC-02-1**: Given a fresh clone of the ipa-manager repo, When I run `go build ./...`, Then it compiles without error using the forked dependency.
- **AC-02-2**: Given a fresh clone, When I run `go test ./...`, Then all existing tests pass with zero failures (no regression from the dependency swap).
- **AC-02-3**: Given `go.mod` is inspected, Then it contains a `replace github.com/majd/ipatool/v2 => github.com/yeuleh/ipatool/v2 <tag-or-sha>` directive pointing to an immutable ref (tag or commit SHA), not a branch name.

### US-03 — Upstream switch-back path

- **AC-03-1**: Given upstream `majd/ipatool` merges an equivalent fix and tags a release, When I remove the `replace` directive and run `go build ./...`, Then the build succeeds (no application code depends on fork-specific APIs).

## 6. Non-Functional Requirements

| ID      | Category        | Requirement                                                                                                   | Measurement                              |
| ------- | --------------- | ------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| NFR-01  | Compatibility   | Works on macOS Apple Silicon and Intel.                                                                        | `go build` succeeds on both architectures. |
| NFR-02  | Reproducibility | `go.mod` replace directive points to an immutable ref (tag or 40-char SHA), never a branch name.               | Manual inspection of go.mod.              |
| NFR-03  | Maintainability | The fork's README or a doc in ipa-manager records: (a) which upstream commit the fork is based on, (b) which PR was applied, (c) how to sync. | Doc exists and is accurate.               |
| NFR-04  | Security        | No credentials, tokens, or personal data in the fork repo or ipa-manager repo.                                 | `git log -p` audit of all changes.        |
| NFR-05  | No regression   | All 69 existing automated tests pass after the dependency swap.                                                 | `go test ./... -count=1` exit code 0.     |

## 7. Key Domain Concepts

| Concept               | Description                                                                                                  |
| --------------------- | ------------------------------------------------------------------------------------------------------------ |
| ipatool auth endpoint | The Apple URL where login credentials are POSTed. Apple moved it to `https://auth.itunes.apple.com/auth/v1/native/fast/`. |
| Bag response          | Apple's configuration endpoint that returns API URLs. PR #493 reads a new `authenticateAccount` field from it.  |
| go.mod replace        | Go module directive that redirects an import path to a different source (fork). Must point to an immutable ref.  |
| ProfileAppStore adapter | ipa-manager's isolation layer (`internal/appstore/`) — the only place ipatool types are imported. Enables swapping the dependency without touching application code. |

## 8. Success Criteria

1. `ipa-manager auth login` succeeds against live Apple servers (verified with a real Apple ID).
2. `go build ./...` and `go test ./...` pass cleanly on a fresh clone.
3. `go.mod` references the fork at an immutable ref.
4. A doc records how to sync the fork with upstream and switch back when the fix is merged.

## 9. Clarification Notes

- **Spike verified**: PR #493 is sufficient. No need to combine with PR #500/#502 at this stage.
- **Fork hosting**: `github.com/yeuleh/ipatool` (user-confirmed).
- **Why not wait for upstream?**: Upstream maintainer is unresponsive (7 PRs open since Jun 10, zero merges as of Jun 30). Waiting blocks all ipa-manager account functionality indefinitely.
- **Why not vendor?**: Vendoring bloats the repo and makes upstream sync harder. A fork with replace is the standard Go practice for unmaintained dependencies.
