package device

import (
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// InstalledApp is a device-installed app summary.
type InstalledApp struct {
	BundleID string
	Name     string
	Version  string
}

// ListInstalledApps lists user-installed apps on a device.
//
// TODO(mission): connect installationproxy.New(device), call BrowseUserApps(),
// and map installationproxy.AppInfo -> InstalledApp.
func ListInstalledApps(deviceUDID string) ([]InstalledApp, error) {
	// Reference the type to lock the integration contract at compile time.
	var _ installationproxy.AppInfo
	return nil, apperr.ErrNotImplemented
}

// Install installs a local IPA to a device via go-ios zipconduit.
//
// TODO(mission): acquire device, detect iOS 17+ tunnel need, zipconduit install.
func Install(deviceUDID, ipaPath string) error { return apperr.ErrNotImplemented }

// Uninstall removes an app from a device via installationproxy.Uninstall.
func Uninstall(deviceUDID, bundleID string) error { return apperr.ErrNotImplemented }
