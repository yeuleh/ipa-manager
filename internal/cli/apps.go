package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func appsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Search the App Store and list app versions",
	}
	cmd.AddCommand(appsSearchCmd(), appsVersionsCmd())
	return cmd
}

func appsSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Search the App Store for apps (active profile)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("apps search: not yet implemented")
		},
	}
}

func appsVersionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "versions <bundle-id>",
		Short: "List available versions of an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("apps versions: not yet implemented")
		},
	}
}
