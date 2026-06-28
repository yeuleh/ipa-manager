package appstore

import "github.com/yeuleh/ipa-manager/internal/apperr"

// SearchResult is an App Store search hit.
type SearchResult struct {
	BundleID string
	Name     string
	Version  string
	Price    string
}

// Search wraps appstore.AppStore.Search for the active profile.
//
// TODO(mission): use the active profile's AppStore instance.
func Search(query string) ([]SearchResult, error) { return nil, apperr.ErrNotImplemented }

// DownloadResult summarizes a completed download.
type DownloadResult struct {
	Path string
}

// Download wraps appstore.AppStore.Download, writing the IPA into the
// profile-isolated local library (internal/library).
func Download(bundleID string) (DownloadResult, error) {
	return DownloadResult{}, apperr.ErrNotImplemented
}
