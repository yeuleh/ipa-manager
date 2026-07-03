package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/library"
)

// deviceInstallCmd implements `device install` (US-02/03/08/09/10).
func deviceInstallCmd(deps Deps) *cobra.Command {
	var profileFlag, udidFlag, versionFlag string
	var latestFlag bool
	cmd := &cobra.Command{
		Use:   "install <bundle-id>",
		Short: "Install an app to a device (from the profile library; auto-downloads if missing)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceInstall(deps, cmd.OutOrStdout(), args[0], profileFlag, udidFlag, versionFlag, latestFlag)
		},
	}
	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile whose library to install from (default: active)")
	cmd.Flags().StringVar(&udidFlag, "udid", "", "target device UDID (default: auto-select or prompt when multiple)")
	cmd.Flags().StringVar(&versionFlag, "version", "", "specific library version to install (default: most-recently-downloaded)")
	cmd.Flags().BoolVar(&latestFlag, "latest", false, "download the App Store's latest version before installing (mutually exclusive with --version)")
	return cmd
}

func runDeviceInstall(deps Deps, out io.Writer,
	bundleID, profileFlag, udidFlag, versionFlag string, latestFlag bool) error {

	// AC-10-4: --latest and --version are mutually exclusive.
	if latestFlag && versionFlag != "" {
		return fmt.Errorf("--latest and --version are mutually exclusive")
	}

	// 1. Resolve profile (requireCredentials=false: cached push needs no creds,
	//    AC-09-2. Credentials are checked inside downloadToLibrary when a
	//    download is actually needed, AC-03-3/AC-09-3).
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

	// 3. Resolve IPA source (decision tree §3.3).
	entry, err := resolveIPASource(deps, out, profile, bundleID, versionFlag, latestFlag)
	if err != nil {
		if isCancelled(err) {
			// License decline: handleLicenseRequired (reused from app_download.go)
			// already printed "cancelled" — don't duplicate. Just exit 0.
			return nil
		}
		return err
	}

	// 4. Push (AC-02-9: always push; device-side accept/reject surfaces raw).
	if err := deps.DeviceService.Install(dev.UDID, entry.FilePath); err != nil {
		return withTrustHint(err) // AC-02-7: trust/pair failure heuristic
	}

	// 5. Report.
	fmt.Fprintf(out, "✓ Installed %s %s → %s\n", entry.BundleID, entry.Version, orDash(dev.Name))
	return nil
}

// resolveIPASource implements the install decision tree (design §3.3).
func resolveIPASource(deps Deps, out io.Writer, profile account.Profile,
	bundleID, versionFlag string, latestFlag bool) (library.Entry, error) {
	if latestFlag {
		return downloadToLibrary(deps, out, profile, bundleID, true) // AC-08-1/2; needs creds
	}
	if versionFlag != "" {
		entry, err := deps.LibraryStore.GetVersion(profile.ID, bundleID, versionFlag)
		if err != nil {
			if errors.Is(err, library.ErrEntryNotFound) {
				return library.Entry{}, fmt.Errorf("version '%s' not in library for '%s'", versionFlag, bundleID) // AC-10-3
			}
			return library.Entry{}, fmt.Errorf("failed to query library: %w", err)
		}
		return entry, nil
	}
	entries, err := deps.LibraryStore.Get(profile.ID, bundleID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("failed to query library: %w", err)
	}
	if len(entries) > 0 {
		return mostRecentByDownloadedAt(entries), nil // AC-10-2
	}
	return downloadToLibrary(deps, out, profile, bundleID, false) // AC-03-1: auto-download
}

// downloadToLibrary downloads (or confirms) an IPA into the profile library and
// returns its Entry. Install-specific: always downloads to the default library
// directory (no --output/--force). Reuses app_download.go's already-factored
// error-recovery (handleDownloadError / handleLicenseRequired / handleTokenExpired)
// so license/token retry behavior stays consistent with `app download`.
//
//	latest=true  : Lookup the App Store's latest version; if the library already
//	               has that exact version string → return it (AC-08-2, no download);
//	               otherwise download as a NEW version entry, preserving older
//	               versions (AC-08-1, multi-version coexistence).
//	latest=false : standard download (AC-03 series).
//
// Returns apperr.ErrCancelled when the user declines a free-license prompt
// (AC-03-5 no) so the caller prints "cancelled" and exits 0.
func downloadToLibrary(deps Deps, out io.Writer, profile account.Profile,
	bundleID string, latest bool) (library.Entry, error) {

	// Credentials required for any App Store operation (AC-03-3 / AC-09-3).
	has, err := deps.Store.HasCredentials(profile.ID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("failed to check credentials: %w", err)
	}
	if !has {
		return library.Entry{}, fmt.Errorf("profile '%s' has no credentials. Run `auth login` to authenticate", profile.ID)
	}

	appStore, err := deps.AppStoreFactory(profile)
	if err != nil {
		return library.Entry{}, fmt.Errorf("failed to initialize App Store: %w", err)
	}
	if _, err := appStore.AccountInfo(); err != nil {
		return library.Entry{}, fmt.Errorf("failed to read account info: %w", err)
	}
	app, err := appStore.Lookup(bundleID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("%w: %s. Verify the bundle identifier", apperr.ErrAppNotFound, bundleID) // AC-03-4
	}

	// --latest: skip download if the library already holds this exact version.
	if latest {
		entries, err := deps.LibraryStore.Get(profile.ID, bundleID)
		if err != nil {
			return library.Entry{}, fmt.Errorf("failed to query library: %w", err) // surface, don't silently download
		}
		for _, e := range entries {
			if e.Version == app.Version { // exact string match (no semantic compare)
				fmt.Fprintf(out, "already up to date (%s)\n", e.Version) // AC-08-2
				return e, nil
			}
		}
	}

	outputPath := filepath.Join(deps.ConfigRoot, "library", profile.ID) + "/"
	if err := os.MkdirAll(outputPath, 0o700); err != nil {
		return library.Entry{}, fmt.Errorf("failed to create library directory: %w", err)
	}

	progress := appstore.NewProgress()
	downloadResult, err := appStore.Download(appstore.DownloadInput{
		BundleID:   bundleID,
		AppID:      app.ID,
		OutputPath: outputPath,
		Progress:   progress,
	})
	if err != nil {
		// Reuse app_download.go's error recovery (license retry + token retry).
		recovered, retryErr := handleDownloadError(deps, appStore, err, app, bundleID, outputPath, "", progress, out)
		if retryErr != nil {
			return library.Entry{}, retryErr
		}
		if !recovered {
			// handleLicenseRequired returns (false, nil) when the user declines → cancel.
			return library.Entry{}, apperr.ErrCancelled // AC-03-5 no
		}
		downloadResult, err = appStore.Download(appstore.DownloadInput{
			BundleID: bundleID, AppID: app.ID, OutputPath: outputPath, Progress: progress,
		})
		if err != nil {
			return library.Entry{}, fmt.Errorf("download failed after retry: %w", err)
		}
	}

	if err := appStore.ReplicateSinf(downloadResult.Sinfs, downloadResult.DestinationPath); err != nil {
		return library.Entry{}, fmt.Errorf("%w: %v. The IPA may not be installable", apperr.ErrReplicateSinfFailed, err)
	}

	stat, _ := os.Stat(downloadResult.DestinationPath)
	var size int64
	if stat != nil {
		size = stat.Size()
	}
	entry := library.Entry{
		BundleID:     bundleID,
		AppID:        app.ID,
		Version:      downloadResult.Version,
		FilePath:     downloadResult.DestinationPath,
		FileSize:     size,
		DownloadedAt: time.Now().UTC(),
	}
	if err := deps.LibraryStore.Add(profile.ID, entry); err != nil {
		fmt.Fprintf(out, "warning: downloaded IPA but failed to update library index: %v\n", err)
	}
	// Report the download step (AC-03-1: auto-download success reflects both
	// download + install; the install line is printed by the caller).
	fmt.Fprintf(out, "✓ Downloaded: %s %s → %s\n", app.Name, entry.Version, entry.FilePath)
	return entry, nil
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
