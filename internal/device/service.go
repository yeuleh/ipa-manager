// Package device adapts go-ios device operations (list / install / list apps /
// uninstall) and surfaces iOS 17+ tunnel requirements as actionable errors.
//
// go-ios types (ios.DeviceEntry, installationproxy.AppInfo, zipconduit) are
// confined to this package (NFR-06). The CLI layer depends only on the Service
// interface and the DeviceInfo / InstalledApp value types.
package device

import (
	"fmt"
	"strings"

	"github.com/danielpaulus/go-ios/ios"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// DeviceInfo is our device summary (no go-ios types leak to the CLI).
type DeviceInfo struct {
	UDID           string // ios.DeviceEntry.Properties.SerialNumber
	Name           string // lockdown DeviceName; empty when unavailable (untrusted / tunnel missing)
	IOSVersion     string // lockdown ProductVersion; empty when unavailable
	ConnectionType string // USB / Network (ConnectionTypeLabel)
	NeedsTunnel    bool   // iOS version >= 17 (see isIOS17OrLater); false when version unknown
}

// Service is the CLI-facing device operation interface. Methods are added
// incrementally per task (staged interface): T1 ListConnected, T2
// ListInstalledApps, T3 Install, T5 Uninstall.
type Service interface {
	// ListConnected enumerates connected devices via usbmuxd and enriches each
	// with lockdown info (name/iOS version) best-effort. iOS 17+ devices are
	// still listed via usbmuxd even without a tunnel (AC-07-1).
	ListConnected() ([]DeviceInfo, error)
	// ListInstalledApps lists user-installed apps on a device (excludes system
	// apps). Connect-stage failure on iOS 17+ (rare) → ErrTunnelRequired.
	ListInstalledApps(udid string) ([]InstalledApp, error)
	// Install pushes a local IPA to a device. On iOS 17+, reuses a running
	// tunnel (read-only query) so zipconduit routes via the shim; without a
	// tunnel, connect-stage fails → ErrTunnelRequired. Operate-stage errors
	// (device rejects the IPA) surface raw.
	Install(udid, ipaPath string) error
	// Uninstall removes an app from a device. Connect-stage failure on iOS 17+
	// (rare) → ErrTunnelRequired. Operate-stage "not installed" →
	// apperr.ErrAppNotInstalled; other operate errors surface raw.
	Uninstall(udid, bundleID string) error
}

// NewService constructs a Service backed by the given Backend.
func NewService(backend Backend) Service {
	return &backendService{backend: backend}
}

type backendService struct {
	backend Backend
}

// ListConnected implements Service.ListConnected.
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
		// best-effort lockdown enrichment: failure (untrusted device, iOS 17+
		// without tunnel) leaves Name/IOSVersion empty and NeedsTunnel false
		// (version unknown) — device is still listed (AC-01-1, AC-07-1).
		if name, version, lerr := s.backend.GetLockdownInfo(entry); lerr == nil {
			info.Name = name
			info.IOSVersion = version
			info.NeedsTunnel = isIOS17OrLater(version)
		}
		result[i] = info
	}
	return result, nil
}

// Compile-time assertion that backendService implements Service.
var _ Service = (*backendService)(nil)

// resolveEntry resolves a device entry by UDID plus best-effort lockdown info.
// GetDeviceEntry failure is a hard error (returned). GetLockdownInfo failure is
// best-effort (name/version left empty) — used for tunnel diagnosis only.
func (s *backendService) resolveEntry(udid string) (entry ios.DeviceEntry, name, version string, err error) {
	entry, err = s.backend.GetDeviceEntry(udid)
	if err != nil {
		return ios.DeviceEntry{}, "", "", err
	}
	name, version, _ = s.backend.GetLockdownInfo(entry)
	return entry, name, version, nil
}

// ListInstalledApps implements Service.ListInstalledApps.
func (s *backendService) ListInstalledApps(udid string) ([]InstalledApp, error) {
	entry, _, version, err := s.resolveEntry(udid)
	if err != nil {
		return nil, err
	}
	conn, err := s.backend.OpenInstallationProxy(entry) // connect-stage
	if err != nil {
		return nil, diagnoseConnectError(err, version) // iOS≥17 paired → ErrTunnelRequired; else raw
	}
	defer conn.Close()
	apps, err := conn.BrowseUserApps() // operate-stage → raw error (never tunnel-misjudged)
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

// Install implements Service.Install (design DD-02 layered + tunnel-info reuse).
func (s *backendService) Install(udid, ipaPath string) error {
	entry, _, version, err := s.resolveEntry(udid)
	if err != nil {
		return err
	}
	// iOS 17+: try to reuse a running tunnel (read-only HTTP query, no sudo) so
	// OpenInstaller routes via the shim path. If no tunnel is running, skip —
	// OpenInstaller stays on usbmuxd, which fails on iOS 17+ → ErrTunnelRequired.
	if isIOS17OrLater(version) {
		if addr, port, e := s.backend.LookupTunnelInfo(udid); e == nil {
			if entry, err = s.backend.WithRsd(entry, udid, addr, port); err != nil {
				return fmt.Errorf("failed to use tunnel: %w", err)
			}
		}
		// e != nil (no tunnel) → fall through; connect will fail → ErrTunnelRequired.
	}
	conn, err := s.backend.OpenInstaller(entry) // connect-stage
	if err != nil {
		return diagnoseConnectError(err, version) // iOS≥17 paired → ErrTunnelRequired; else raw
	}
	defer conn.Close()
	return conn.SendFile(ipaPath) // operate-stage → raw (never misjudged as tunnel)
}

// Uninstall implements Service.Uninstall.
func (s *backendService) Uninstall(udid, bundleID string) error {
	entry, _, version, err := s.resolveEntry(udid)
	if err != nil {
		return err
	}
	conn, err := s.backend.OpenInstallationProxy(entry) // connect-stage
	if err != nil {
		return diagnoseConnectError(err, version) // iOS≥17 paired → ErrTunnelRequired; else raw
	}
	defer conn.Close()
	if err := conn.Uninstall(bundleID); err != nil {
		// operate-stage: never tunnel. Map "not installed" → ErrAppNotInstalled
		// (exact pattern confirmed live at validate; heuristics cover common forms).
		if isNotInstalledErr(err) {
			return apperr.ErrAppNotInstalled
		}
		return err
	}
	return nil
}

// isNotInstalledErr reports whether a go-ios uninstall error indicates the
// bundle is not installed on the device. go-ios returns a generic error here
// (no exported sentinel), so a heuristic is used. The exact pattern is
// confirmed against a real device at validate; this covers common forms.
func isNotInstalledErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not installed") ||
		strings.Contains(msg, "no such app") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist")
}
