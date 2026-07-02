package device

// InstalledApp is a device-installed app summary (user apps only; system apps
// are excluded). Populated from installationproxy.BrowseUserApps (T2).
type InstalledApp struct {
	BundleID string
	Name     string
	Version  string
}
