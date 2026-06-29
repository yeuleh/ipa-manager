package appstore

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/yeuleh/ipa-manager/internal/account"
)

// AppStoreFactory is the dependency-injection type for constructing per-profile
// AppStore instances (design DD-12). Production implementations close over
// ConfigRoot via a closure; test implementations return mock AppStores.
type AppStoreFactory func(p account.Profile) (appstore.AppStore, error)

// NewAppStoreFactory returns a production AppStoreFactory bound to the given
// config root. This avoids leaking ipatool's appstore.AppStore type into
// callers (e.g., cli/deps.go).
func NewAppStoreFactory(configRoot string) AppStoreFactory {
	return func(p account.Profile) (appstore.AppStore, error) {
		return NewProfileAppStore(p, configRoot)
	}
}
