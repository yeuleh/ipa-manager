package appstore

import (
	"errors"

	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// ErrAuthCodeRequired is re-exported from ipatool for the auth flow.
var ErrAuthCodeRequired = ipaappstore.ErrAuthCodeRequired

// mapAppStoreError translates ipatool sentinel errors to our apperr sentinels.
// Used by the adapter's Download AND Purchase methods (NFR-06: shared conversion
// prevents sentinel-conversion bugs like the Purchase token-expired bypass).
//
// Rename history: was mapDownloadError (pre fix-purchase-token-expired mission).
// Behavioral impact: Download behavior unchanged; Purchase return semantics
// intentionally changed (returns apperr sentinel instead of raw ipatool sentinel).
func mapAppStoreError(err error) error {
	if errors.Is(err, ipaappstore.ErrLicenseRequired) {
		return apperr.ErrLicenseRequired
	}
	if errors.Is(err, ipaappstore.ErrPasswordTokenExpired) {
		return apperr.ErrPasswordTokenExpired
	}
	return err // unknown: pass through
}
