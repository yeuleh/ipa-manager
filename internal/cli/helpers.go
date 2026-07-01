package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/99designs/keyring"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// isKeychainNotFound returns true if the error indicates the keychain key
// was not found (treated as success for Revoke/remove operations per DD-08).
func isKeychainNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return true
	}
	// Fallback for backends that wrap differently.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "no match")
}

// resolveProfile resolves the target profile from --profile flag or active.
// Returns ErrProfileNotFound / ErrProfileNotLoggedIn / ErrNoActiveProfile
// with actionable hints (NFR-06).
//
// requireCredentials=true: validates that the profile has keychain credentials
// (needed for search/download which call Apple API; not needed for library list/clean).
func resolveProfile(deps Deps, profileFlag string, requireCredentials bool) (account.Profile, error) {
	var profile account.Profile

	if profileFlag != "" {
		var err error
		profile, err = deps.Store.Get(profileFlag)
		if err != nil || (err == nil && profile.ID == "") {
			if errors.Is(err, apperr.ErrProfileNotFound) || profile.ID == "" {
				return account.Profile{}, fmt.Errorf("%w. Run `accounts list` to see available profiles", apperr.ErrProfileNotFound)
			}
			return account.Profile{}, fmt.Errorf("failed to get profile: %w", err)
		}
	} else {
		activeID, err := deps.Store.GetActiveID()
		if err != nil {
			return account.Profile{}, fmt.Errorf("failed to get active profile: %w", err)
		}
		if activeID == "" {
			return account.Profile{}, fmt.Errorf("%w. Run `accounts use <profile-id>` to set one", apperr.ErrNoActiveProfile)
		}
		profile, err = deps.Store.Get(activeID)
		if err != nil {
			return account.Profile{}, fmt.Errorf("failed to get active profile: %w", err)
		}
	}

	if requireCredentials {
		has, err := deps.Store.HasCredentials(profile.ID)
		if err != nil {
			return account.Profile{}, fmt.Errorf("failed to check credentials: %w", err)
		}
		if !has {
			return account.Profile{}, fmt.Errorf("%w. Run `auth login` to authenticate", apperr.ErrProfileNotLoggedIn)
		}
	}

	return profile, nil
}
