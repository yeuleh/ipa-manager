package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/library"
)

func libraryCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage the local per-profile IPA library",
	}
	cmd.AddCommand(libraryListCmd(deps), libraryCleanCmd(deps))
	return cmd
}

func libraryListCmd(deps Deps) *cobra.Command {
	var profileFlag string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded IPAs for a profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Library is local — no Apple API, no credentials needed
			profile, err := resolveProfile(deps, profileFlag, false)
			if err != nil {
				return err
			}

			entries, err := deps.LibraryStore.List(profile.ID)
			if err != nil {
				return fmt.Errorf("failed to list library: %w", err)
			}

			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintf(out, "no IPAs in library for profile '%s'\n", profile.ID)
				return nil
			}

			renderLibraryList(out, entries)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile to use (default: active)")
	return cmd
}

func renderLibraryList(out interface{ Write([]byte) (int, error) }, entries []library.Entry) {
	fmt.Fprintln(out, "BUNDLE-ID\tVERSION\tSIZE\tDOWNLOADED-AT\tPATH")
	for _, e := range entries {
		size := formatSize(e.FileSize)
		ts := e.DownloadedAt.Format("2006-01-02 15:04")
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", e.BundleID, e.Version, size, ts, e.FilePath)
	}
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// =============================================================================
// library clean
// =============================================================================

func libraryCleanCmd(deps Deps) *cobra.Command {
	var (
		profileFlag string
		versionFlag string
	)

	cmd := &cobra.Command{
		Use:   "clean [bundle-id]",
		Short: "Remove downloaded IPAs from the library",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			profile, err := resolveProfile(deps, profileFlag, false)
			if err != nil {
				return err
			}

			if len(args) > 0 {
				// clean specific bundle-id (optionally --version)
				return runCleanBundleID(deps, out, profile.ID, args[0], versionFlag)
			}
			// clean all
			return runCleanAll(deps, out, profile.ID)
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile to use (default: active)")
	cmd.Flags().StringVar(&versionFlag, "version", "", "remove only this version (requires bundle-id argument)")
	return cmd
}

// runCleanAll removes all IPAs for the profile (AC-05-1/2/9).
func runCleanAll(deps Deps, out interface{ Write([]byte) (int, error) }, profileID string) error {
	entries, err := deps.LibraryStore.List(profileID)
	if err != nil {
		return fmt.Errorf("failed to list library: %w", err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(out, "library is already empty")
		return nil
	}

	// D-05 fix: stat partitioning — existing vs absent
	var existing, absent []library.Entry
	for _, e := range entries {
		if _, statErr := os.Stat(e.FilePath); statErr == nil {
			existing = append(existing, e)
		} else {
			absent = append(absent, e)
		}
	}

	// All absent → remove stale index entries, no confirmation needed
	if len(existing) == 0 {
		count, _ := deps.LibraryStore.CleanAll(profileID)
		fmt.Fprintf(out, "removed %d stale index entries (files already absent)\n", count)
		return nil
	}

	// Non-interactive + has existing files → error (AC-05-9)
	if !checkInteractive() {
		return fmt.Errorf("confirmation required in non-interactive mode; cannot proceed")
	}

	// Confirm (AC-05-1: disclose custom paths)
	fmt.Fprintf(out, "remove all %d IPA(s) for profile '%s'? [y/N]\n", len(existing), profileID)
	for _, e := range existing {
		// Show custom-output paths (non-default library path)
		if !isDefaultLibraryPath(e.FilePath, profileID) {
			fmt.Fprintf(out, "  - %s\n", e.FilePath)
		}
	}
	if len(absent) > 0 {
		fmt.Fprintf(out, "(%d stale index entries will also be removed)\n", len(absent))
	}

	confirmed, err := deps.UI.Confirm("confirm removal?")
	if err != nil {
		return fmt.Errorf("failed to prompt: %w", err)
	}
	if !confirmed {
		fmt.Fprintln(out, "cancelled")
		return nil
	}

	count, err := deps.LibraryStore.CleanAll(profileID)
	if err != nil {
		return fmt.Errorf("failed to clean library: %w", err)
	}
	fmt.Fprintf(out, "✓ Removed %d IPA(s)", count)
	if len(absent) > 0 {
		fmt.Fprintf(out, " + %d stale entries", len(absent))
	}
	fmt.Fprintln(out, ".")
	return nil
}

// runCleanBundleID removes specific bundle-id versions (AC-05-3/4/7/8/10/11/12).
func runCleanBundleID(deps Deps, out interface{ Write([]byte) (int, error) }, profileID, bundleID, versionFlag string) error {
	if versionFlag != "" {
		// AC-05-11/12: clean specific version
		entry, err := deps.LibraryStore.GetVersion(profileID, bundleID, versionFlag)
		if err != nil {
			fmt.Fprintf(out, "no IPA for '%s' version '%s' in profile '%s'\n", bundleID, versionFlag, profileID)
			return nil
		}
		return cleanSingleEntry(deps, out, profileID, entry, true)
	}

	// AC-05-3/4/10: clean all versions of bundle-id
	entries, err := deps.LibraryStore.Get(profileID, bundleID)
	if err != nil {
		return fmt.Errorf("failed to query library: %w", err)
	}
	if len(entries) == 0 {
		fmt.Fprintf(out, "no IPA for '%s' in profile '%s'\n", bundleID, profileID)
		return nil
	}

	// D-05 fix: stat partitioning
	var existing, absent []library.Entry
	for _, e := range entries {
		if _, statErr := os.Stat(e.FilePath); statErr == nil {
			existing = append(existing, e)
		} else {
			absent = append(absent, e)
		}
	}

	// All absent → remove stale entries, no confirmation (AC-05-8)
	if len(existing) == 0 {
		deps.LibraryStore.Remove(profileID, bundleID)
		fmt.Fprintf(out, "file already absent for '%s', removed index entries\n", bundleID)
		return nil
	}

	// Non-interactive + has existing → error (AC-05-9)
	if !checkInteractive() {
		return fmt.Errorf("confirmation required in non-interactive mode; cannot proceed")
	}

	// Single version → single confirm (AC-05-3)
	if len(existing) == 1 {
		return cleanSingleEntry(deps, out, profileID, existing[0], false)
	}

	// Multiple versions → list all, confirm all (AC-05-10)
	fmt.Fprintf(out, "remove all %d version(s) of '%s'? [y/N]\n", len(existing), bundleID)
	for _, e := range existing {
		fmt.Fprintf(out, "  - %s %s (%s)\n", e.BundleID, e.Version, formatSize(e.FileSize))
		if !isDefaultLibraryPath(e.FilePath, profileID) {
			fmt.Fprintf(out, "    at %s\n", e.FilePath)
		}
	}

	confirmed, err := deps.UI.Confirm("confirm removal?")
	if err != nil {
		return fmt.Errorf("failed to prompt: %w", err)
	}
	if !confirmed {
		fmt.Fprintln(out, "cancelled")
		return nil
	}

	count, err := deps.LibraryStore.Remove(profileID, bundleID)
	if err != nil {
		return fmt.Errorf("failed to remove: %w", err)
	}
	fmt.Fprintf(out, "✓ Removed %d version(s) of '%s'.\n", count, bundleID)
	return nil
}

// cleanSingleEntry handles removing a single entry with confirmation.
func cleanSingleEntry(deps Deps, out interface{ Write([]byte) (int, error) }, profileID string, entry library.Entry, specificVersion bool) error {
	// Stat check (D-05)
	if _, err := os.Stat(entry.FilePath); err != nil {
		// File absent — remove index only (AC-05-8)
		if specificVersion {
			deps.LibraryStore.RemoveVersion(profileID, entry.BundleID, entry.Version)
		} else {
			deps.LibraryStore.Remove(profileID, entry.BundleID)
		}
		fmt.Fprintf(out, "file already absent for '%s'\n", entry.BundleID)
		return nil
	}

	// Non-interactive (AC-05-9)
	if !checkInteractive() {
		return fmt.Errorf("confirmation required in non-interactive mode; cannot proceed")
	}

	// Confirm (AC-05-3)
	prompt := fmt.Sprintf("remove '%s' version %s (%s)", entry.BundleID, entry.Version, formatSize(entry.FileSize))
	if !isDefaultLibraryPath(entry.FilePath, profileID) {
		prompt += fmt.Sprintf(" at %s", entry.FilePath)
	}
	prompt += "? [y/N]"

	fmt.Fprintln(out, prompt)
	confirmed, err := deps.UI.Confirm("confirm removal?")
	if err != nil {
		return fmt.Errorf("failed to prompt: %w", err)
	}
	if !confirmed {
		fmt.Fprintln(out, "cancelled")
		return nil
	}

	if specificVersion {
		if err := deps.LibraryStore.RemoveVersion(profileID, entry.BundleID, entry.Version); err != nil {
			return fmt.Errorf("failed to remove: %w", err)
		}
	} else {
		if _, err := deps.LibraryStore.Remove(profileID, entry.BundleID); err != nil {
			return fmt.Errorf("failed to remove: %w", err)
		}
	}
	fmt.Fprintf(out, "✓ Removed '%s' version %s.\n", entry.BundleID, entry.Version)
	return nil
}

// isDefaultLibraryPath returns true if the file path is inside the default library directory.
func isDefaultLibraryPath(filePath, profileID string) bool {
	return !startsWith(filePath, "/tmp") && !startsWith(filePath, os.Getenv("HOME")+"/Desktop") // simplified check
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
