package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/account"
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
			out := cmd.OutOrStdout()
			id := args[0]

			if err := deps.Store.Load(); err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// AC-04-5: check existence FIRST (fast fail, no confirm prompt).
			profile, err := deps.Store.Get(id)
			if err != nil {
				return fmt.Errorf("profile '%s' not found. Run `accounts list` to see available profiles.", id)
			}

			// AC-04-4: confirm prompt.
			confirmed, err := deps.UI.Confirm(fmt.Sprintf("Remove profile '%s'? This deletes credentials and metadata.", id))
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}
			if !confirmed {
				fmt.Fprintln(out, "Cancelled.")
				return nil // exit 0 (AC-04-4: not an error)
			}

			// DD-08 cascade: collect ALL errors, don't abort on individual failures.
			var errs []error

			// Step 1: Revoke keychain credentials.
			appStore, factoryErr := deps.AppStoreFactory(profile)
			if factoryErr != nil {
				errs = append(errs, fmt.Errorf("construct appstore: %w", factoryErr))
			} else {
				if err := appStore.Revoke(); err != nil && !isKeychainNotFound(err) {
					errs = append(errs, fmt.Errorf("revoke keychain: %w", err))
				}
			}

			// Step 2: Delete cookie jar (best-effort, ignore NotExist).
			cookiePath := account.CookieJarPath(id, deps.ConfigRoot)
			if err := os.Remove(cookiePath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("delete cookie jar: %w", err))
			}

			// Step 3: Remove metadata (Store enforces active-clearing invariant).
			if err := deps.Store.Remove(id); err != nil {
				errs = append(errs, fmt.Errorf("remove metadata: %w", err))
			}

			// Step 4: Persist config.
			if err := deps.Store.Save(); err != nil {
				errs = append(errs, fmt.Errorf("persist config: %w", err))
			}

			// Report results (NFR-04: no silent partial success).
			if len(errs) > 0 {
				fmt.Fprintf(out, "Profile '%s' removed with %d error(s):\n", id, len(errs))
				for _, e := range errs {
					fmt.Fprintf(out, "  - %v\n", e)
				}
				return fmt.Errorf("remove completed with %d error(s)", len(errs))
			}

			fmt.Fprintf(out, "Profile '%s' removed.\n", id)
			return nil
		},
	}
}
