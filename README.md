# ipa-manager

A macOS CLI for managing the full lifecycle of iOS apps (`.ipa`) across **multiple Apple accounts**: log in / switch accounts, download and isolate apps per account, and push them to iOS devices for install / update.

## Why

`ipatool` and `go-ios` are excellent individual tools, but neither handles **multi-account switching + per-account isolation** out of the box. `ipa-manager` fills that gap: it orchestrates both as a single, friendly CLI and adds the multi-account layer that is its core value.

## Status

Scaffolded. The command tree and project structure are in place and compile; feature implementation lands via MindTrek missions (`/mission engage`). See `docs/bootstrap/` for the full design rationale.

## How it works

- **Account side** — [`ipatool`](https://github.com/majd/ipatool) (MIT, Go) handles Apple ID login / 2FA / App Store search / `.ipa` download. Imported as a library.
- **Device side** — [`go-ios`](https://github.com/danielpaulus/go-ios) (MIT, Go) handles device install / list / uninstall. Imported as a library.
- **Multi-account isolation** — `account.ProfileKeychain` namespaces ipatool's fixed `"account"` keychain key per profile, so multiple Apple accounts coexist without clobbering each other (see `docs/bootstrap/decisions/0002-multi-account-isolation.md`).

> ⚠️ Known caveats: ipatool depends on Apple's private API (breaks occasionally until upstream fixes it); iOS 17+ devices require `sudo ios tunnel start`.

## Getting Started

### Prerequisites

- macOS
- Go **1.26+** (`brew install go`)
- (Optional, for device ops) a paired iOS device; iOS 17+ needs `sudo ios tunnel start`

### Build & run

```bash
make build              # → bin/ipa-manager
./bin/ipa-manager --help
./bin/ipa-manager --version
```

Or directly:

```bash
go run ./cmd/ipa-manager --help
```

### Common commands (once implemented)

```bash
ipa-manager auth login                  # log in an Apple ID (handles 2FA)
ipa-manager accounts list               # list configured profiles
ipa-manager accounts use <profile-id>   # switch active account
ipa-manager apps search <term>          # search the App Store
ipa-manager install download <bundle-id>   # download an IPA (per-account isolation)
ipa-manager install push <ipa-path>     # install to a connected device
ipa-manager doctor                      # environment health check
```

### Development

```bash
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -s -w .
make tidy      # go mod tidy
make lint      # golangci-lint run (if installed)
```

## Project layout

```
cmd/ipa-manager/      entry point
internal/
  cli/                cobra command tree
  account/            profiles + ProfileKeychain (multi-account isolation)
  appstore/           ipatool adapter
  device/             go-ios adapter
  library/            per-account local .ipa store
  config/             paths + global config
  doctor/             health checks
  ui/                 huh prompts + lipgloss tables
docs/                 design docs (bootstrap, decisions)
```

## Using MindTrek on this project

- `/mission engage` — start a feature mission (e.g. implement `auth login` with 2FA).
- Read `AGENTS.md` for project-wide context every agent should know.
- Design history lives in `docs/bootstrap/`; architectural decisions in `docs/bootstrap/decisions/`.

## License

Personal tool. Underlying libraries are MIT (`ipatool`, `go-ios`).
