package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/appstore"
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
			out := cmd.OutOrStdout()

			// DD-05 step 1-2: collect credentials.
			email, err := deps.UI.InputEmail()
			if err != nil {
				return fmt.Errorf("failed to read email: %w", err)
			}
			password, err := deps.UI.InputPassword()
			if err != nil {
				return fmt.Errorf("failed to read password: %w", err)
			}

			// DD-05 step 3: derive profile ID.
			id := account.DeriveProfileID(email)

			// DD-05 step 4: load store (to check existing + active state).
			if err := deps.Store.Load(); err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// DD-05 step 5: construct per-profile ProfileAppStore (adapter isolates ipatool).
			fmt.Fprintln(out, "Contacting Apple...")
			appStore, err := deps.AppStoreFactory(account.Profile{ID: id, Email: email})
			if err != nil {
				return fmt.Errorf("failed to initialize appstore: %w", err)
			}

			// DD-05 step 6: get auth endpoint.
			endpoint, err := appStore.GetAuthEndpoint()
			if err != nil {
				return fmt.Errorf("failed to reach Apple: %w", err)
			}

			// DD-05 step 7: first login attempt (no auth code).
			result, err := appStore.Login(appstore.LoginInput{
				Email:    email,
				Password: password,
				AuthCode: "",
				Endpoint: endpoint,
			})

			// DD-05 step 8: handle 2FA if required.
			if errors.Is(err, appstore.ErrAuthCodeRequired) {
				fmt.Fprintln(out, "2FA verification code required. Check your Apple device.")
				authCode, err := deps.UI.InputAuthCode()
				if err != nil {
					return fmt.Errorf("failed to read 2FA code: %w", err)
				}
				fmt.Fprintln(out, "Authenticating with 2FA...")
				result, err = appStore.Login(appstore.LoginInput{
					Email:    email,
					Password: password,
					AuthCode: authCode,
					Endpoint: endpoint,
				})
				if err != nil {
					return fmt.Errorf("%w: verify your 2FA code and retry", err)
				}
			} else if err != nil {
				return fmt.Errorf("%w: verify your credentials and retry", err)
			}

			// DD-05 step 9-10: persist profile metadata.
			if err := deps.Store.Upsert(account.Profile{
				ID:         id,
				Name:       result.Name,
				Email:      result.Email,
				StoreFront: result.StoreFront,
			}); err != nil {
				return fmt.Errorf("failed to save profile: %w", err)
			}

			// DD-05 step 11: first profile auto-active (AC-01-3).
			activeID, _ := deps.Store.GetActiveID()
			if activeID == "" {
				if err := deps.Store.SetActive(id); err != nil {
					return fmt.Errorf("failed to set active profile: %w", err)
				}
			}

			// DD-05 step 12: save.
			if err := deps.Store.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			// DD-05 step 13: success (NFR-09: no password/token in output).
			fmt.Fprintf(out, "Logged in: %s (%s), profile: %s\n", result.Name, result.Email, id)
			return nil
		},
	}
}

func authLogoutCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout [profile-id]",
		Short: "Revoke credentials (defaults to active profile)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			if err := deps.Store.Load(); err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Resolve target profile (design §3.8).
			var targetID string
			if len(args) > 0 {
				targetID = args[0]
			} else {
				activeID, _ := deps.Store.GetActiveID()
				if activeID == "" {
					return fmt.Errorf("no active profile. Run `accounts use <profile-id>` to set one.")
				}
				targetID = activeID
			}

			// Check profile exists (AC-05-3).
			profile, err := deps.Store.Get(targetID)
			if err != nil {
				return fmt.Errorf("profile '%s' not found. Run `accounts list` to see available profiles.", targetID)
			}

			// Check credentials — idempotent if already logged out (AC-05-5).
			hasCreds, _ := deps.Store.HasCredentials(targetID)
			if !hasCreds {
				fmt.Fprintf(out, "Profile '%s' is already logged out.\n", targetID)
				return nil
			}

			// Revoke credentials + delete cookie jar (best-effort error collection).
			var errs []error

			appStore, err := deps.AppStoreFactory(profile)
			if err != nil {
				errs = append(errs, fmt.Errorf("construct appstore: %w", err))
			} else {
				if err := appStore.Revoke(); err != nil && !isKeychainNotFound(err) {
					errs = append(errs, fmt.Errorf("revoke keychain: %w", err))
				}
			}

			// Delete cookie jar file (best-effort, ignore NotExist).
			cookiePath := account.CookieJarPath(targetID, deps.ConfigRoot)
			if err := os.Remove(cookiePath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("delete cookie jar: %w", err))
			}

			// Do NOT touch metadata, do NOT change active (AC-05-1).
			if len(errs) > 0 {
				errOut := cmd.ErrOrStderr()
				fmt.Fprintf(errOut, "Logged out '%s' with errors:\n", targetID)
				for _, e := range errs {
					fmt.Fprintf(errOut, "  - %v\n", e)
				}
				return fmt.Errorf("logout completed with %d error(s)", len(errs))
			}

			fmt.Fprintf(out, "Logged out: %s (profile metadata retained).\n", targetID)
			return nil
		},
	}
}
