package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Apple ID login / logout (per account profile)",
	}
	cmd.AddCommand(authLoginCmd(), authLogoutCmd())
	return cmd
}

func authLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in an Apple ID under a new or existing profile (handles 2FA)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(mission): collect email/password, call appstore.Login,
			// retry with AuthCode on ErrAuthCodeRequired, persist via ProfileKeychain.
			return fmt.Errorf("auth login: not yet implemented")
		},
	}
}

func authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the active profile's Apple ID credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("auth logout: not yet implemented")
		},
	}
}
