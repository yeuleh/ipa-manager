package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/appstore"
)

func appCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Search the App Store and download apps (active profile)",
	}
	cmd.AddCommand(appSearchCmd(deps), appVersionsCmd())
	return cmd
}

func appSearchCmd(deps Deps) *cobra.Command {
	var (
		profileFlag string
		limitFlag   int64
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search the App Store for apps (active profile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := args[0]

			// Validate limit (AC-01-6)
			if limitFlag <= 0 {
				return fmt.Errorf("invalid --limit value: must be a positive integer")
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
	cmd.Flags().Int64VarP(&limitFlag, "limit", "l", 5, "maximum amount of search results")
	return cmd
}

func renderSearchResults(cmd *cobra.Command, results []appstore.AppInfo) {
	out := cmd.OutOrStdout()
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
