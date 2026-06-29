package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/ui"
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
			out := cmd.OutOrStdout()

			if err := deps.Store.Load(); err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			profiles, err := deps.Store.List()
			if err != nil {
				return fmt.Errorf("failed to list profiles: %w", err)
			}

			if len(profiles) == 0 {
				fmt.Fprintln(out, "No profiles configured. Run `auth login` to add one.")
				return nil
			}

			activeID, err := deps.Store.GetActiveID()
			if err != nil {
				return fmt.Errorf("failed to get active profile: %w", err)
			}

			headers := []string{"ACTIVE", "ID", "EMAIL", "NAME", "STATUS"}
			var rows [][]string
			for _, p := range profiles {
				hasCreds, _ := deps.Store.HasCredentials(p.ID)
				status := "logged-out"
				if hasCreds {
					status = "logged-in"
				}
				marker := ""
				if p.ID == activeID {
					marker = "*"
				}
				rows = append(rows, []string{marker, p.ID, p.Email, p.Name, status})
			}

			fmt.Fprintln(out, ui.RenderTable(headers, rows))
			return nil
		},
	}
}

func accountsUseCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile-id>",
		Short: "Set the active account profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			id := args[0]

			if err := deps.Store.Load(); err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// DD-07 strict validation order: existence before credentials.
			if _, err := deps.Store.Get(id); err != nil {
				return fmt.Errorf("profile '%s' not found. Run `accounts list` to see available profiles.", id)
			}

			hasCreds, _ := deps.Store.HasCredentials(id)
			if !hasCreds {
				return fmt.Errorf("profile '%s' has no credentials. Run `auth login` to authenticate.", id)
			}

			if err := deps.Store.SetActive(id); err != nil {
				return fmt.Errorf("failed to set active profile: %w", err)
			}
			if err := deps.Store.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Fprintf(out, "Active profile: %s\n", id)
			return nil
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
