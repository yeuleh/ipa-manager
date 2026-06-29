package cli

import (
	"fmt"

	"github.com/99designs/keyring"
	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/config"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// Deps holds all dependencies injected into CLI commands (design DD-12).
// This enables testability: tests construct mock Deps and pass them to
// command constructors, avoiding package-level global state.
type Deps struct {
	Store             account.Store             // profile CRUD + active + credential state
	AppStoreFactory   appstore.AppStoreFactory  // per-profile AppStore construction
	UI                ui.Prompter               // interactive prompts (nil until T4 implements)
	ConfigRoot        string                    // ~/.ipa-manager root path
}

// newProductionDeps constructs real (non-mock) dependencies for production use.
// Called once from Execute().
func newProductionDeps() (Deps, error) {
	paths, err := config.Default()
	if err != nil {
		return Deps{}, fmt.Errorf("failed to resolve paths: %w", err)
	}

	// Open keyring for keychain operations (macOS Keychain).
	ring, err := keyring.Open(keyring.Config{
		AllowedBackends: []keyring.BackendType{keyring.KeychainBackend},
		ServiceName:     appstore.KeychainServiceName,
	})
	if err != nil {
		return Deps{}, fmt.Errorf("failed to open keyring: %w", err)
	}

	// Base keychain (shared across all profile credential probes).
	baseKeychain := ipakeychain.New(ipakeychain.Args{Keyring: ring})

	return Deps{
		Store:           account.NewStore(paths.Config, baseKeychain),
		AppStoreFactory: appstore.NewAppStoreFactory(paths.Root),
		UI:              ui.NewPrompter(),
		ConfigRoot:      paths.Root,
	}, nil
}
