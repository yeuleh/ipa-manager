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
//	T3: OpenInstaller (+ InstallerConn) / LookupTunnelInfo
//	T5: ProxyConn gains Uninstall
type Backend interface {
	// ListDeviceEntries returns connected devices via usbmuxd (ios.ListDevices).
	ListDeviceEntries() ([]ios.DeviceEntry, error)
	// GetDeviceEntry returns the device entry for a UDID (ios.GetDevice).
	GetDeviceEntry(udid string) (ios.DeviceEntry, error)
	// GetLockdownInfo returns DeviceName + ProductVersion via lockdown
	// (ios.GetValues). Used for DeviceInfo enrichment and tunnel diagnosis.
	GetLockdownInfo(entry ios.DeviceEntry) (name, version string, err error)
	// OpenInstallationProxy opens the installation_proxy service over usbmuxd
	// (installationproxy.New). Connect-stage: failure on iOS 17+ (rare, since
	// installationproxy uses usbmuxd lockdown) is diagnosed by the Service.
	OpenInstallationProxy(entry ios.DeviceEntry) (ProxyConn, error)
	// OpenInstaller opens the zipconduit service for IPA install
	// (zipconduit.New). Connect-stage: on iOS 17+ without a tunnel this fails
	// (Apple removed the usbmuxd zipconduit channel); diagnosed by the Service.
	OpenInstaller(entry ios.DeviceEntry) (InstallerConn, error)
	// LookupTunnelInfo queries a running tunnel agent (read-only HTTP GET to
	// 127.0.0.1:60105) for the device's tunnel address + RSD port. Returns an
	// error (e.g. tunnel.ErrTunnelNotFound) when no tunnel is running — no sudo.
	LookupTunnelInfo(udid string) (address string, rsdPort int, err error)
	// WithRsd injects the RSD provider (from a running tunnel) into a device
	// entry so OpenInstaller routes via the shim path. Mirrors go-ios's
	// deviceWithRsdProvider. Does NOT copy UserspaceTUN (this tool does not run
	// userspace tunnels).
	WithRsd(entry ios.DeviceEntry, udid, address string, rsdPort int) (ios.DeviceEntry, error)
}

// ProxyConn wraps installationproxy.Connection for the operate stage
// (BrowseUserApps / Uninstall). Package-internal; go-ios types appear here.
// T2 implements BrowseUserApps + Close; T5 adds Uninstall.
type ProxyConn interface {
	// BrowseUserApps lists user-installed apps (excludes system apps).
	BrowseUserApps() ([]installationproxy.AppInfo, error)
	// Uninstall removes an app by bundle-id (operate-stage).
	Uninstall(bundleID string) error
	// Close releases the service connection.
	Close()
}

// InstallerConn wraps zipconduit.Connection for the operate stage (SendFile).
// Package-internal; go-ios types appear here.
type InstallerConn interface {
	// SendFile pushes a local IPA to the device (operate-stage: errors here are
	// surfaced raw, never misjudged as tunnel — tunnel is connect-stage only).
	SendFile(ipaPath string) error
	// Close releases the service connection.
	Close() error
}
