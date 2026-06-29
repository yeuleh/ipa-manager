package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func accountCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accounts",
		Short: "Manage Apple account profiles (list / use / remove)",
	}
	// NOTE: accountsAddCmd removed per DD-11 — auth login absorbs the add flow.
	cmd.AddCommand(accountsListCmd(deps), accountsUseCmd(deps), accountsRemoveCmd(deps))
	return cmd
}

func accountsListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured account profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(T2): implement list flow (design §3.6)
			return fmt.Errorf("accounts list: not yet implemented")
		},
	}
}

func accountsUseCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile-id>",
		Short: "Set the active account profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(T3): implement use flow (design §3.5, DD-07)
			return fmt.Errorf("accounts use: not yet implemented")
		},
	}
}

func accountsRemoveCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <profile-id>",
		Short: "Remove an account profile and revoke its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(T6): implement remove flow (design §3.7, DD-08)
			return fmt.Errorf("accounts remove: not yet implemented")
		},
	}
}
