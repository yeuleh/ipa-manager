package appstore

import (
	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"
)

// AppInfo is our version of ipatool's App (only fields we use).
// Exposed to CLI layer; ipatool types never leak past the adapter.
type AppInfo struct {
	ID       int64   // ipatool App.ID (trackId)
	BundleID string  // ipatool App.BundleID
	Name     string  // ipatool App.Name (trackName)
	Version  string  // ipatool App.Version
	Price    float64 // ipatool App.Price
}

// AccountInfoResult wraps the Account fields needed by callers.
// Does NOT expose Password/PasswordToken/DirectoryServicesID (NFR-04).
type AccountInfoResult struct {
	Email      string
	Name       string
	StoreFront string
}

// appToAppInfo converts ipatool's App to our AppInfo.
func appToAppInfo(app ipaappstore.App) AppInfo {
	return AppInfo{
		ID:       app.ID,
		BundleID: app.BundleID,
		Name:     app.Name,
		Version:  app.Version,
		Price:    app.Price,
	}
}
