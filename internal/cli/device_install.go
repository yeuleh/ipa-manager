package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/device"
	"github.com/yeuleh/ipa-manager/internal/library"
)

// deviceInstallCmd implements `device install` (US-02/03/07/08/09/10).
//
// T3 scope: push an IPA from the profile library (--version / --profile /
// --udid). Auto-download (--latest) is added in T4; --latest is intentionally
// NOT registered here (T4 registers it + the mutual-exclusion check).
func deviceInstallCmd(deps Deps) *cobra.Command {
	var profileFlag, udidFlag, versionFlag string
	cmd := &cobra.Command{
		Use:   "install <bundle-id>",
		Short: "Install an app to a device from the profile library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceInstall(deps, cmd.OutOrStdout(), args[0], profileFlag, udidFlag, versionFlag)
		},
	}
	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile whose library to install from (default: active)")
	cmd.Flags().StringVar(&udidFlag, "udid", "", "target device UDID (default: auto-select or prompt when multiple)")
	cmd.Flags().StringVar(&versionFlag, "version", "", "specific library version to install (default: most-recently-downloaded)")
	return cmd
}

func runDeviceInstall(deps Deps, out io.Writer,
	bundleID, profileFlag, udidFlag, versionFlag string) error {

	// 1. Resolve profile (requireCredentials=false: cached push needs no creds;
	//    AC-09-2. creds only required when auto-download triggers, which is T4).
	profile, err := resolveProfile(deps, profileFlag, false)
	if err != nil {
		return err
	}

	// 2. Resolve device (AC-06-*).
	dev, err := resolveDevice(deps, udidFlag)
	if err != nil {
		if isCancelled(err) {
			fmt.Fprintln(out, "cancelled")
			return nil
		}
		return err
	}

	// 3. Resolve IPA source from the library (AC-10-1/2/3). T3 = library-has
	//    path only; auto-download (AC-03) lands in T4.
	entry, err := resolveLibraryEntry(deps, profile.ID, bundleID, versionFlag)
	if err != nil {
		return err
	}

	// 4. Push (AC-02-9: always push; AC-07-2: iOS 17+ tunnel).
	if err := deps.DeviceService.Install(dev.UDID, entry.FilePath); err != nil {
		if errors.Is(err, device.ErrTunnelRequired) {
			return fmt.Errorf("iOS 17+ tunnel required; run: sudo ios tunnel start")
		}
		return withTrustHint(err) // AC-02-7: trust/pair failure heuristic
	}

	// 5. Report.
	fmt.Fprintf(out, "✓ Installed %s %s → %s\n", entry.BundleID, entry.Version, orDash(dev.Name))
	return nil
}

// resolveLibraryEntry picks the IPA entry from the profile library (AC-10-1/2/3).
func resolveLibraryEntry(deps Deps, profileID, bundleID, versionFlag string) (library.Entry, error) {
	if versionFlag != "" {
		entry, err := deps.LibraryStore.GetVersion(profileID, bundleID, versionFlag)
		if err != nil {
			if errors.Is(err, library.ErrEntryNotFound) {
				return library.Entry{}, fmt.Errorf("version '%s' not in library for '%s'", versionFlag, bundleID) // AC-10-3
			}
			return library.Entry{}, fmt.Errorf("failed to query library: %w", err) // real storage error, not "not found"
		}
		return entry, nil
	}
	entries, err := deps.LibraryStore.Get(profileID, bundleID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("failed to query library: %w", err)
	}
	if len(entries) == 0 {
		// T4 replaces this branch with auto-download (AC-03). For now, library-only.
		return library.Entry{}, fmt.Errorf("%w: no IPA for '%s' in profile '%s' library",
			apperr.ErrAppNotFound, bundleID, profileID)
	}
	return mostRecentByDownloadedAt(entries), nil // AC-10-2
}

// mostRecentByDownloadedAt returns the entry with the latest DownloadedAt
// timestamp (AC-10-2: avoids unreliable version-string semantic comparison).
func mostRecentByDownloadedAt(entries []library.Entry) library.Entry {
	latest := entries[0]
	for _, e := range entries[1:] {
		if e.DownloadedAt.After(latest.DownloadedAt) {
			latest = e
		}
	}
	return latest
}

// withTrustHint appends an actionable hint when a device error looks like a
// trust/pairing failure (AC-02-7). Non-trust errors pass through unchanged.
// go-ios trust errors are generic (no exported sentinel), so a heuristic is
// used; this never affects tunnel diagnosis (which is connect-stage, handled
// separately and returned as ErrTunnelRequired before reaching here).
func withTrustHint(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "pair") || strings.Contains(msg, "trust") || strings.Contains(msg, "not paired") {
		return fmt.Errorf("%w\n  hint: trust this Mac on the device (disconnect/reconnect and tap Trust, or Settings → General → VPN & Device Management)", err)
	}
	return err
}
