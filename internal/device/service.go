// Package device adapts go-ios device operations (list / install / list apps /
// uninstall) and surfaces iOS 17+ tunnel requirements as actionable errors.
//
// go-ios types (ios.DeviceEntry, installationproxy.AppInfo, zipconduit) are
// confined to this package (NFR-06). The CLI layer depends only on the Service
// interface and the DeviceInfo / InstalledApp value types.
package device

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
