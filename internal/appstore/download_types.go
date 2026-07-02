package appstore

import (
	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"
)

// DownloadInput is our version of ipatool's DownloadInput.
type DownloadInput struct {
	BundleID          string
	AppID             int64
	OutputPath        string   // directory (default) or file path (--output)
	ExternalVersionID string   // empty = latest
	Progress          Progress // nil = no progress display
}

// DownloadResult summarizes a completed download.
type DownloadResult struct {
	DestinationPath string
	Version         string // parsed from DestinationPath; "unknown" for custom paths
	Sinfs           []Sinf
}

// Sinf is our version of ipatool's Sinf (DRM key fragment).
type Sinf struct {
	ID   int64
	Data []byte
}

// Progress is our abstraction over ipatool's *progressbar.ProgressBar.
// nil-safe: ipatool checks `if progress != nil` before using it.
type Progress interface {
	ChangeMax64(max int64)
	Set64(v int64) error
}

// appInfoToApp converts our AppInfo back to ipatool's App (for Download/Purchase).
func appInfoToApp(bundleID string, appID int64) ipaappstore.App {
	return ipaappstore.App{
		ID:       appID,
		BundleID: bundleID,
	}
}

// sinfsToOur converts ipatool's []Sinf to our []Sinf.
func sinfsToOur(ipaSinfs []ipaappstore.Sinf) []Sinf {
	result := make([]Sinf, len(ipaSinfs))
	for i, s := range ipaSinfs {
		result[i] = Sinf{ID: s.ID, Data: s.Data}
	}
	return result
}
