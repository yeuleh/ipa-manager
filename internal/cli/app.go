package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/appstore"
)

func appCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Search the App Store and download apps (active profile)",
	}
	cmd.AddCommand(appSearchCmd(deps), appDownloadCmd(deps), appVersionsCmd())
	return cmd
}

func appSearchCmd(deps Deps) *cobra.Command {
	var (
		profileFlag  string
		limitStrFlag string
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search the App Store for apps (active profile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := args[0]

			// Validate limit (AC-01-6: 0, negative, non-integer all rejected)
			limitFlag, err := parseLimit(limitStrFlag)
			if err != nil {
				return err
			}

			// Resolve profile (requires credentials for Apple API)
			profile, err := resolveProfile(deps, profileFlag, true)
			if err != nil {
				return err
			}

			// Construct per-profile AppStore
			appStore, err := deps.AppStoreFactory(profile)
			if err != nil {
				return fmt.Errorf("failed to initialize App Store: %w", err)
			}

			// AccountInfo (adapter caches full Account for Search)
			_, err = appStore.AccountInfo()
			if err != nil {
				return fmt.Errorf("failed to read account info: %w", err)
			}

			// Search
			results, err := appStore.Search(term, limitFlag)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			// Render
			if len(results) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no results found for '%s'\n", term)
				return nil
			}

			renderSearchResults(cmd, results)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileFlag, "profile", "", "profile to use (default: active)")
	cmd.Flags().StringVarP(&limitStrFlag, "limit", "l", "5", "maximum amount of search results (positive integer)")
	return cmd
}

// parseLimit validates and parses the --limit flag value (AC-01-6).
func parseLimit(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid --limit value: must be a positive integer")
	}
	if n <= 0 {
		return 0, fmt.Errorf("invalid --limit value: must be a positive integer")
	}
	return n, nil
}

func renderSearchResults(cmd *cobra.Command, results []appstore.AppInfo) {
	out := cmd.OutOrStdout()
	// T1-01 fix: table header (AC-01-1: columns Name / Bundle-ID / Version / Price)
	fmt.Fprintln(out, "NAME\tBUNDLE-ID\tVERSION\tPRICE")
	for _, app := range results {
		price := "Free"
		if app.Price > 0 {
			price = fmt.Sprintf("%.2f", app.Price)
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", app.Name, app.BundleID, app.Version, price)
	}
}

func appVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions <bundle-id>",
		Short: "List available versions of an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("app versions: not yet implemented")
		},
	}
}
