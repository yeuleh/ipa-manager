package appstore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/99designs/keyring"
	cookiejar "github.com/juju/persistent-cookiejar"
	"github.com/majd/ipatool/v2/pkg/appstore"
	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"

	"github.com/yeuleh/ipa-manager/internal/account"
)

// keyringOpener is the function used to open keyring backends.
// Package-level variable allows test injection (Spok finding 4).
// In production this calls keyring.Open; in tests it can return a mock keyring.
var keyringOpener = keyring.Open

// NewProfileAppStore constructs an ipatool AppStore scoped to a single account
// profile with isolated keychain namespace and cookie jar (design DD-01).
//
// Wiring steps (verified against ipatool v2.3.0 source):
//  1. keyring.Open with ServiceName "ipa-manager" (isolated from raw ipatool)
//  2. ipakeychain.New wraps keyring → ipatool keychain.Keychain
//  3. account.ProfileKeychain wraps keychain → per-profile namespace
//  4. cookiejar.New at per-profile cookie jar path
//  5. operatingsystem.New + machine.New (shared singletons)
//  6. appstore.NewAppStore with all deps injected
func NewProfileAppStore(p account.Profile, configRoot string) (appstore.AppStore, error) {
	// 1. Keyring backend (macOS Keychain only for v1, design DD-02).
	// FileDir and FilePasswordFunc are populated per DD-02 even though
	// FileBackend is not in AllowedBackends — defensive completeness.
	ring, err := keyringOpener(keyring.Config{
		AllowedBackends: []keyring.BackendType{keyring.KeychainBackend},
		ServiceName:     KeychainServiceName,
		FileDir:         filepath.Join(configRoot, "keychain"),
		FilePasswordFunc: func(prompt string) (string, error) {
			return "", fmt.Errorf("file keyring backend is not supported in ipa-manager v1; only macOS Keychain is allowed")
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open keyring: %w", err)
	}

	// 2. Base ipatool keychain.
	kc := ipakeychain.New(ipakeychain.Args{Keyring: ring})

	// 3. ProfileKeychain namespace wrapper (ADR 0002).
	pkc := account.ProfileKeychain{Base: kc, ProfileID: p.ID}

	// 4. Per-profile persistent cookie jar.
	jarPath := account.CookieJarPath(p.ID, configRoot)
	if err := os.MkdirAll(filepath.Dir(jarPath), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create cookie jar directory: %w", err)
	}
	jar, err := cookiejar.New(&cookiejar.Options{Filename: jarPath})
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	// 5. OS + Machine (shared, no per-profile state).
	osys := operatingsystem.New()
	mac := machine.New(machine.Args{OS: osys})

	// 6. Construct AppStore with isolated deps.
	return appstore.NewAppStore(appstore.Args{
		Keychain:        pkc,
		CookieJar:       jar,
		OperatingSystem: osys,
		Machine:         mac,
	}), nil
}
