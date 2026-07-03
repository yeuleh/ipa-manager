// Package device adapts go-ios device operations (list / install / list apps /
// uninstall). go-ios types are confined to this package (NFR-06); the CLI layer
// depends only on the Service interface and the DeviceInfo / InstalledApp types.
//
// NOTE on iOS 17+ / tunnels: live testing on iOS 26 showed install/apps/uninstall
// work over usbmuxd WITHOUT a tunnel. The earlier "iOS 17+ needs a tunnel" premise
// (and its detection/reuse machinery) was removed as empirically falsified. If a
// future environment genuinely requires a tunnel, surface the raw go-ios error and
// add targeted handling based on real evidence — do not reintroduce speculative
// detection. See docs/features/ios-device-manage plan.md ledger.
package device

import (
	"github.com/danielpaulus/go-ios/ios/installationproxy"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// DeviceInfo is our device summary (no go-ios types leak to the CLI).
type DeviceInfo struct {
	UDID           string // ios.DeviceEntry.Properties.SerialNumber
	Name           string // lockdown DeviceName; empty when unavailable (untrusted)
	IOSVersion     string // lockdown ProductVersion; empty when unavailable
	ConnectionType string // USB / Network (ConnectionTypeLabel)
}

// Service is the CLI-facing device operation interface.
type Service interface {
	ListConnected() ([]DeviceInfo, error)
	ListInstalledApps(udid string) ([]InstalledApp, error)
	Install(udid, ipaPath string) error
	Uninstall(udid, bundleID string) error
}

// NewService constructs a Service backed by the given Backend.
func NewService(backend Backend) Service {
	return &backendService{backend: backend}
}

type backendService struct {
	backend Backend
}

// ListConnected enumerates connected devices via usbmuxd and enriches each with
// lockdown info (name/iOS version) best-effort. A device whose lockdown fails
// (untrusted) is still listed with empty Name/IOSVersion (AC-01-1).
func (s *backendService) ListConnected() ([]DeviceInfo, error) {
	entries, err := s.backend.ListDeviceEntries()
	if err != nil {
		return nil, err
	}
	result := make([]DeviceInfo, len(entries))
	for i, entry := range entries {
		info := DeviceInfo{
			UDID:           entry.Properties.SerialNumber,
			ConnectionType: entry.ConnectionTypeLabel(),
		}
		if name, version, lerr := s.backend.GetLockdownInfo(entry); lerr == nil {
			info.Name = name
			info.IOSVersion = version
		}
		result[i] = info
	}
	return result, nil
}

// ListInstalledApps lists user-installed apps on a device (excludes system apps).
func (s *backendService) ListInstalledApps(udid string) ([]InstalledApp, error) {
	entry, err := s.backend.GetDeviceEntry(udid)
	if err != nil {
		return nil, err
	}
	conn, err := s.backend.OpenInstallationProxy(entry)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	apps, err := conn.BrowseUserApps()
	if err != nil {
		return nil, err
	}
	result := make([]InstalledApp, len(apps))
	for i, a := range apps {
		result[i] = InstalledApp{
			BundleID: a.CFBundleIdentifier(),
			Name:     a.CFBundleName(),
			Version:  a.CFBundleShortVersionString(),
		}
	}
	return result, nil
}

// Install pushes a local IPA to a device via go-ios zipconduit (over usbmuxd).
func (s *backendService) Install(udid, ipaPath string) error {
	entry, err := s.backend.GetDeviceEntry(udid)
	if err != nil {
		return err
	}
	conn, err := s.backend.OpenInstaller(entry)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.SendFile(ipaPath)
}

// Uninstall removes an app from a device. A pre-check (BrowseUserApps) confirms
// the bundle is installed — go-ios treats uninstalling a non-existent app as
// idempotent success, so we surface absence as apperr.ErrAppNotInstalled (AC-04-3).
func (s *backendService) Uninstall(udid, bundleID string) error {
	entry, err := s.backend.GetDeviceEntry(udid)
	if err != nil {
		return err
	}
	conn, err := s.backend.OpenInstallationProxy(entry)
	if err != nil {
		return err
	}
	defer conn.Close()
	apps, err := conn.BrowseUserApps()
	if err != nil {
		return err
	}
	if !bundleInstalled(apps, bundleID) {
		return apperr.ErrAppNotInstalled
	}
	return conn.Uninstall(bundleID)
}

// bundleInstalled reports whether bundleID is among the device's user apps.
func bundleInstalled(apps []installationproxy.AppInfo, bundleID string) bool {
	for _, a := range apps {
		if a.CFBundleIdentifier() == bundleID {
			return true
		}
	}
	return false
}

// Compile-time assertion that backendService implements Service.
var _ Service = (*backendService)(nil)
