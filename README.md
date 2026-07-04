# ipa-manager

**English** | [简体中文](README_cn.md)

A macOS CLI for managing the full lifecycle of iOS apps (`.ipa`) across **multiple Apple accounts**: log in / switch accounts, download and isolate apps per account, and push them to iOS devices for install / update.

## Why

Some apps only ship in certain App Store regions — they never appear in the global catalog. When your iOS device is already signed into one Apple ID, installing an app from another region normally means digging into the App Store settings to swap accounts on the device itself: disruptive, slow, and easy to lose track of which account owns which purchase. `ipa-manager` sidesteps all of that. It keeps several Apple accounts on your Mac, downloads each app from the right region's catalog, and pushes it to the device over USB — the account your device is signed into never has to change. Under the hood it orchestrates `ipatool` (login / search / download) and `go-ios` (device install) as libraries, and adds the multi-account isolation layer that is its core value.

## Status

Core commands are implemented and usable — account login/switching, App Store search/download, per-account IPA library, and device install/uninstall over usbmuxd. A few subcommands remain stubs (`app versions`, `doctor`). See `docs/bootstrap/` for the full design rationale.

## How it works

- **Account side** — [`ipatool`](https://github.com/majd/ipatool) (MIT, Go) handles Apple ID login / 2FA / App Store search / `.ipa` download. Imported as a library. This project uses a maintained fork (`yeuleh/ipatool/v2`, `fix-auth` branch) for auth fixes.
- **Device side** — [`go-ios`](https://github.com/danielpaulus/go-ios) (MIT, Go) handles device install / list / uninstall over `usbmuxd` (the USB multiplexing daemon bundled with macOS — no extra install, no tunnel). Imported as a library.
- **Multi-account isolation** — each account profile stores its Apple ID credentials in an isolated Keychain namespace, so multiple accounts coexist without clobbering each other. (Internals: `account.ProfileKeychain` repurposes ipatool's fixed `"account"` key; see `docs/bootstrap/decisions/0002-multi-account-isolation.md`.)

> ⚠️ Known caveats: ipatool depends on Apple's private API (breaks occasionally until upstream fixes it). Device operations (install / list apps / uninstall) work over usbmuxd without any tunnel or `sudo`.

## Getting Started

### Prerequisites

- macOS
- Go **1.26+** (`brew install go`)
- (Optional, for device ops) a paired iOS device connected over USB

### Build & run

```bash
make build              # → bin/ipa-manager
./bin/ipa-manager --help
./bin/ipa-manager --version
```

The command examples below use `ipa-manager` directly — alias it or add `./bin/` to your `PATH`:

```bash
alias ipa-manager=./bin/ipa-manager
```

Or run straight from source:

```bash
go run ./cmd/ipa-manager --help
```

### Quick start

```bash
# 1. log in an Apple ID (creates a profile; handles 2FA)
./bin/ipa-manager auth login

# 2. search the App Store
./bin/ipa-manager app search "your app name"

# 3. download the IPA into this account's isolated library
./bin/ipa-manager app download <bundle-id>

# 4. plug in an iOS device over USB and install it
./bin/ipa-manager device install <bundle-id>

# 5. add a second account and switch between them anytime
./bin/ipa-manager auth login          # another Apple ID
./bin/ipa-manager accounts list       # see all profiles
./bin/ipa-manager accounts use <id>   # switch active account
```

### Common commands

```bash
# accounts
ipa-manager auth login                  # log in an Apple ID (creates/refreshes profile, handles 2FA)
ipa-manager auth logout [profile-id]    # revoke credentials (defaults to active profile)
ipa-manager accounts list               # list configured profiles + status
ipa-manager accounts use <profile-id>   # switch active account (strict: must be logged-in)
ipa-manager accounts remove <id>        # delete profile + revoke credentials (with confirm)

# app store (active profile)
ipa-manager app search <term>           # search the App Store
ipa-manager app download <bundle-id>    # download an app's IPA to the profile library

# local library (per profile)
ipa-manager library list                # list downloaded IPAs for the active profile
ipa-manager library clean [bundle-id]   # remove downloaded IPAs

# devices (over usbmuxd, no tunnel / sudo)
ipa-manager device list                 # list connected iOS devices
ipa-manager device apps                 # list user-installed apps on a device
ipa-manager device install <bundle-id>  # install an app to a device (auto-downloads if missing; requires account login)
ipa-manager device uninstall <bundle-id># uninstall an app from a device
```

> Run `ipa-manager <command> --help` to see all available flags.
> `app versions` and `doctor` are not yet implemented.

### Known limitations

- **No concurrent access**: running multiple ipa-manager commands simultaneously on the **same profile** (e.g., `auth login` and `accounts remove` in different terminal windows) may corrupt state. Behavior is **undefined** and not covered by tests. Run commands sequentially.
- **No `accounts add` command**: use `auth login` to add new accounts.
- **macOS only**: depends on macOS Keychain for credential storage.

### Development

```bash
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -s -w .
make tidy      # go mod tidy
make lint      # golangci-lint run (if installed)
```

> Builds require CGO enabled (macOS Keychain backend). `make build` / `go build` handle this by default.

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

## License

Personal tool. Underlying libraries are MIT (`ipatool`, `go-ios`).
