package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func accountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "Manage Apple account profiles (list / use / add / remove)",
	}
	cmd.AddCommand(accountsListCmd(), accountsUseCmd(), accountsAddCmd(), accountsRemoveCmd())
	return cmd
}

func accountsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured account profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("accounts list: not yet implemented")
		},
	}
}

func accountsUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile-id>",
		Short: "Set the active account profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("accounts use: not yet implemented")
		},
	}
}

func accountsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new account profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("accounts add: not yet implemented")
		},
	}
}

func accountsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <profile-id>",
		Short: "Remove an account profile and revoke its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("accounts remove: not yet implemented")
		},
	}
}
