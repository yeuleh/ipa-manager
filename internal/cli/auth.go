package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func authCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Apple ID login / logout (per account profile)",
	}
	cmd.AddCommand(authLoginCmd(deps), authLogoutCmd(deps))
	return cmd
}

func authLoginCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in an Apple ID (creates or refreshes a profile, handles 2FA)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(T4): implement login flow (DD-05)
			return fmt.Errorf("auth login: not yet implemented")
		},
	}
}

func authLogoutCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout [profile-id]",
		Short: "Revoke credentials (defaults to active profile)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(T5): implement logout flow (design §3.8)
			return fmt.Errorf("auth logout: not yet implemented")
		},
	}
}
