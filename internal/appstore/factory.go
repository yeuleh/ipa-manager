package appstore

import (
	"github.com/yeuleh/ipa-manager/internal/account"
)

// AppStoreFactory is the dependency-injection type for constructing per-profile
// ProfileAppStore instances (design DD-12). Production implementations close
// over ConfigRoot via a closure; test implementations return mock ProfileAppStores.
//
// Returns ProfileAppStore (our interface), NOT ipatool's AppStore — this keeps
// ipatool types confined to the appstore package (R1 mitigation, OCP/DIP).
type AppStoreFactory func(p account.Profile) (ProfileAppStore, error)

// NewAppStoreFactory returns a production AppStoreFactory bound to the given
// config root. This avoids leaking any ipatool types into callers (e.g., cli/deps.go).
func NewAppStoreFactory(configRoot string) AppStoreFactory {
	return func(p account.Profile) (ProfileAppStore, error) {
		return NewProfileAppStore(p, configRoot)
	}
}
