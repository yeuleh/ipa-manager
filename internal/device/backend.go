package device

import (
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
)

// Backend abstracts go-ios device calls. The type is exported so it can be
// implemented by test doubles within the internal/device package, but because
// the package is under internal/, it is not reachable outside the module —
// go-ios types thus never leak to the CLI (NFR-06). Methods are added
// incrementally per task (staged interface):
//
//	T1: ListDeviceEntries / GetDeviceEntry / GetLockdownInfo
//	T2: OpenInstallationProxy (+ ProxyConn)
//	T3: OpenInstaller (+ InstallerConn)
//	T5: ProxyConn gains Uninstall
//	(historical: tunnel methods LookupTunnelInfo/WithRsd were removed after
//	 live testing showed iOS 17+ works over usbmuxd — see Live Amendment)
type Backend interface {
	// ListDeviceEntries returns connected devices via usbmuxd (ios.ListDevices).
	ListDeviceEntries() ([]ios.DeviceEntry, error)
	// GetDeviceEntry returns the device entry for a UDID (ios.GetDevice).
	GetDeviceEntry(udid string) (ios.DeviceEntry, error)
	// GetLockdownInfo returns DeviceName + ProductVersion via lockdown
	// (ios.GetValues). Used for DeviceInfo enrichment (name/version display).
	GetLockdownInfo(entry ios.DeviceEntry) (name, version string, err error)
	// OpenInstallationProxy opens the installation_proxy service over usbmuxd
	// (installationproxy.New).
	OpenInstallationProxy(entry ios.DeviceEntry) (ProxyConn, error)
	// OpenInstaller opens the zipconduit service for IPA install (zipconduit.New),
	// over usbmuxd.
	OpenInstaller(entry ios.DeviceEntry) (InstallerConn, error)
}

// ProxyConn wraps installationproxy.Connection for the operate stage
// (BrowseUserApps / Uninstall). Package-internal; go-ios types appear here.
type ProxyConn interface {
	// BrowseUserApps lists user-installed apps (excludes system apps).
	BrowseUserApps() ([]installationproxy.AppInfo, error)
	// Uninstall removes an app by bundle-id.
	Uninstall(bundleID string) error
	// Close releases the service connection.
	Close()
}

// InstallerConn wraps zipconduit.Connection for the operate stage (SendFile).
// Package-internal; go-ios types appear here.
type InstallerConn interface {
	// SendFile pushes a local IPA to the device.
	SendFile(ipaPath string) error
	// Close releases the service connection.
	Close() error
}
