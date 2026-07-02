package device

import (
	"github.com/danielpaulus/go-ios/ios"
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
}
