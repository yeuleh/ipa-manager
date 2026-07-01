package appstore

import (
	"errors"

	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// ErrAuthCodeRequired is re-exported from ipatool for the auth flow.
var ErrAuthCodeRequired = ipaappstore.ErrAuthCodeRequired

// mapDownloadError translates ipatool sentinel errors to our apperr sentinels.
// Used by the adapter's Download method.
func mapDownloadError(err error) error {
	if errors.Is(err, ipaappstore.ErrLicenseRequired) {
		return apperr.ErrLicenseRequired
	}
	if errors.Is(err, ipaappstore.ErrPasswordTokenExpired) {
		return apperr.ErrPasswordTokenExpired
	}
	return err // unknown: pass through
}
