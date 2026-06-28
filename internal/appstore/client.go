// Package appstore adapts ipatool's AppStore to per-profile account isolation.
package appstore

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// NewProfileAppStore constructs an ipatool AppStore scoped to a single account
// profile: an account.ProfileKeychain for namespaced keychain state plus a
// per-profile cookie jar (see account.CookieJarPath).
//
// TODO(mission): wire the keychain backend (99designs/keyring via ipatool),
// the per-profile cookie jar, and the OS/machine deps, then call
// appstore.NewAppStore(appstore.Args{...}).
func NewProfileAppStore(p account.Profile) (appstore.AppStore, error) {
	// Reference the types to lock the integration contract at compile time.
	var _ appstore.AppStore
	var _ account.ProfileKeychain
	return nil, apperr.ErrNotImplemented
}
