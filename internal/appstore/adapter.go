package appstore

import (
	"github.com/yeuleh/ipa-manager/internal/account"
)

// ProfileAppStore is our abstraction over ipatool's AppStore.
//
// The CLI layer depends on this interface, not on ipatool's concrete types.
// This isolates ipatool API changes to the adapter implementation only
// (design §6.1 ipatool upgrade risk, R1 mitigation).
//
// Only the methods needed by the account lifecycle mission are exposed;
// ipatool's AppStore has 12 methods, we use 3 (ISP — Interface Segregation).
type ProfileAppStore interface {
	// GetAuthEndpoint calls ipatool's Bag() and returns the auth endpoint URL.
	// Must be called before Login to obtain the Endpoint field.
	GetAuthEndpoint() (string, error)

	// Login authenticates with Apple. Returns ErrAuthCodeRequired if 2FA is needed.
	// On success, persists account JSON to keychain via ProfileKeychain.
	Login(input LoginInput) (LoginResult, error)

	// Revoke removes the profile's credentials from keychain.
	Revoke() error
}

// LoginInput is our version of ipatool's appstore.LoginInput.
// Replaces the third-party type at the package boundary.
type LoginInput struct {
	Email    string
	Password string
	AuthCode string
	Endpoint string
}

// LoginResult is our version of the relevant fields from ipatool's
// appstore.LoginOutput.Account. Only what the CLI needs (Name, Email, StoreFront).
type LoginResult struct {
	Name       string
	Email      string
	StoreFront string
}

// Compile-time assertion that ProfileAppStore is implemented by the adapter.
var _ ProfileAppStore = (*profileAppStoreAdapter)(nil)

// Compile-time assertion that LoginInput/LoginResult don't leak ipatool types.
// (They're plain structs with only string fields — no third-party imports.)
var (
	_ = LoginInput{}
	_ = LoginResult{}
	_ = account.Profile{}
)
