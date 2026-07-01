package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/library"
)

func libraryCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage the local per-profile IPA library",
	}
	cmd.AddCommand(libraryListCmd(deps))
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
