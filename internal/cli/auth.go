package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"

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

			// DD-05 step 5: construct per-profile AppStore.
			fmt.Fprintln(out, "Contacting Apple...")
			appStore, err := deps.AppStoreFactory(account.Profile{ID: id, Email: email})
			if err != nil {
				return fmt.Errorf("failed to initialize appstore: %w", err)
			}

			// DD-05 step 6: get auth endpoint (must call Bag before Login).
			bag, err := appStore.Bag(ipaappstore.BagInput{})
			if err != nil {
				return fmt.Errorf("failed to reach Apple: %w", err)
			}

			// DD-05 step 7: first login attempt (no auth code).
			output, err := appStore.Login(ipaappstore.LoginInput{
				Email:    email,
				Password: password,
				AuthCode: "",
				Endpoint: bag.AuthEndpoint,
			})

			// DD-05 step 8: handle 2FA if required.
			if errors.Is(err, appstore.ErrAuthCodeRequired) {
				fmt.Fprintln(out, "2FA verification code required. Check your Apple device.")
				authCode, err := deps.UI.InputAuthCode()
				if err != nil {
					return fmt.Errorf("failed to read 2FA code: %w", err)
				}
				fmt.Fprintln(out, "Authenticating with 2FA...")
				output, err = appStore.Login(ipaappstore.LoginInput{
					Email:    email,
					Password: password,
					AuthCode: authCode,
					Endpoint: bag.AuthEndpoint,
				})
				if err != nil {
					// AC-06-2: wrong 2FA → fail with Apple's message + hint.
					return fmt.Errorf("%w: verify your 2FA code and retry", err)
				}
			} else if err != nil {
				// AC-07-1: wrong password or other auth failure.
				return fmt.Errorf("%w: verify your credentials and retry", err)
			}

			// DD-05 step 9-10: persist profile metadata.
			acc := output.Account
			if err := deps.Store.Upsert(account.Profile{
				ID:         id,
				Name:       acc.Name,
				Email:      acc.Email,
				StoreFront: acc.StoreFront,
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
			fmt.Fprintf(out, "Logged in: %s (%s), profile: %s\n", acc.Name, acc.Email, id)
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
			// TODO(T5): implement logout flow (design §3.8)
			return fmt.Errorf("auth logout: not yet implemented")
		},
	}
}
