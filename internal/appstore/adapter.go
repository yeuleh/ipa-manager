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
	GetAuthEndpoint() (string, error)

	// Login authenticates with Apple. Returns ErrAuthCodeRequired if 2FA is needed.
	Login(input LoginInput) (LoginResult, error)

	// Revoke removes the profile's credentials from keychain.
	Revoke() error

	// AccountInfo reads the cached Account from keychain.
	// Must be called before Lookup/Search/Download (adapter caches the full Account).
	// Does NOT expose Password/PasswordToken (NFR-04).
	AccountInfo() (AccountInfoResult, error)

	// Search queries the App Store for apps matching the term.
	// Uses the cached Account's StoreFront for region-scoped results.
	Search(query string, limit int64) ([]AppInfo, error)

	// Lookup looks up a single app by bundle-id.
	// Must be called after AccountInfo.
	Lookup(bundleID string) (AppInfo, error)

	// Download downloads the IPA to OutputPath (directory or file).
	// Must be called after AccountInfo + Lookup.
	Download(input DownloadInput) (DownloadResult, error)

	// ReplicateSinf writes DRM decryption keys into the downloaded IPA.
	// Must be called after Download for the IPA to be installable.
	ReplicateSinf(sinfs []Sinf, packagePath string) error

	// Purchase acquires a free-app license (price must be 0).
	// Must be called after AccountInfo. Used for license-required retry.
	Purchase(bundleID string, appID int64, price float64) error

	// RefreshSession re-authenticates using cached Account.Password.
	// Must be called after AccountInfo. Used for token-expired retry.
	// Does NOT expose Password to callers (NFR-04).
	RefreshSession() error
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
