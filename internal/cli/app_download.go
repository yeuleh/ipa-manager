package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/library"
)

func appDownloadCmd(deps Deps) *cobra.Command {
	var (
		profileFlag  string
		outputFlag   string
		forceFlag    bool
		versionIDArg string
	)

	cmd := &cobra.Command{
		Use:   "download <bundle-id>",
		Short: "Download an app's IPA (active profile, isolated per account)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundleID := args[0]
			return runDownload(deps, cmd, bundleID, profileFlag, outputFlag, forceFlag, versionIDArg)
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile to use (default: active)")
	cmd.Flags().StringVar(&outputFlag, "output", "", "custom output file path")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "overwrite existing file")
	cmd.Flags().StringVar(&versionIDArg, "external-version-id", "", "specific version to download (default: latest)")
	return cmd
}

func runDownload(deps Deps, cmd *cobra.Command, bundleID, profileFlag, outputFlag string, forceFlag bool, versionIDArg string) error {
	out := cmd.OutOrStdout()

	// 1. Resolve profile (requires credentials)
	profile, err := resolveProfile(deps, profileFlag, true)
	if err != nil {
		return err
	}

	// 2. Construct AppStore
	appStore, err := deps.AppStoreFactory(profile)
	if err != nil {
		return fmt.Errorf("failed to initialize App Store: %w", err)
	}

	// 3. AccountInfo (adapter caches full Account)
	_, err = appStore.AccountInfo()
	if err != nil {
		return fmt.Errorf("failed to read account info: %w", err)
	}

	// 4. Lookup app metadata
	app, err := appStore.Lookup(bundleID)
	if err != nil {
		return fmt.Errorf("%w: %s. Verify the bundle identifier", apperr.ErrAppNotFound, bundleID)
	}

	// 5. Resolve output path
	var outputPath string
	if outputFlag != "" {
		if err := validateOutputPath(outputFlag); err != nil {
			return err
		}
		outputPath = outputFlag
	} else {
		// Pass library directory to ipatool — it generates filename with actual version
		outputPath = filepath.Join(deps.ConfigRoot, "library", profile.ID) + "/"
	}

	// 6. Skip check (AC-02-5: physical file existence)
	// For --external-version-id, bypass skip (version unknown until download)
	if versionIDArg == "" {
		skipPath := computeSkipPath(outputPath, bundleID, app.ID, app.Version)
		if skipPath != "" {
			if _, statErr := os.Stat(skipPath); statErr == nil && !forceFlag {
				fmt.Fprintf(out, "already exists: %s (use --force to overwrite)\n", skipPath)
				return nil
			}
		}
	}

	// 7. Progress
	progress := appstore.NewProgress()

	// 8. Download
	downloadResult, err := appStore.Download(appstore.DownloadInput{
		BundleID:          bundleID,
		AppID:             app.ID,
		OutputPath:        outputPath,
		ExternalVersionID: versionIDArg,
		Progress:          progress,
	})
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// 9. ReplicateSinf (DRM keys)
	err = appStore.ReplicateSinf(downloadResult.Sinfs, downloadResult.DestinationPath)
	if err != nil {
		return fmt.Errorf("%w: %v. The IPA may not be installable", apperr.ErrReplicateSinfFailed, err)
	}

	// 10. Register in library index
	stat, _ := os.Stat(downloadResult.DestinationPath)
	var fileSize int64
	if stat != nil {
		fileSize = stat.Size()
	}
	err = deps.LibraryStore.Add(profile.ID, library.Entry{
		BundleID:     bundleID,
		AppID:        app.ID,
		Version:      downloadResult.Version,
		FilePath:     downloadResult.DestinationPath,
		FileSize:     fileSize,
	})
	if err != nil {
		fmt.Fprintf(out, "warning: downloaded IPA but failed to update library index: %v\n", err)
	}

	// 11. Report
	fmt.Fprintf(out, "✓ Downloaded: %s %s → %s\n", app.Name, downloadResult.Version, downloadResult.DestinationPath)
	return nil
}

// computeSkipPath returns the file path to check for skip detection.
// For directory output: constructs approximate filename using lookup version.
// For file output: returns the path as-is.
func computeSkipPath(outputPath, bundleID string, appID int64, version string) string {
	if outputPath == "" {
		return ""
	}
	info, err := os.Stat(outputPath)
	if err == nil && info.IsDir() {
		return filepath.Join(outputPath, fmt.Sprintf("%s_%d_%s.ipa", bundleID, appID, version))
	}
	if strings.HasSuffix(outputPath, "/") {
		return filepath.Join(outputPath, fmt.Sprintf("%s_%d_%s.ipa", bundleID, appID, version))
	}
	return outputPath
}

// validateOutputPath validates the --output flag value (AC-10-4/5/6).
func validateOutputPath(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return fmt.Errorf("output path is a directory: %s", path)
	}
	parent := filepath.Dir(path)
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		return fmt.Errorf("output directory does not exist: %s", parent)
	}
	return nil
}
