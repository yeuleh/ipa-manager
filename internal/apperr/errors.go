// Package apperr defines shared sentinel errors used across packages.
package apperr

import "errors"

// ErrNotImplemented marks a stubbed operation not yet implemented.
// Scaffolded commands return this until their feature mission lands.
var ErrNotImplemented = errors.New("not implemented")

// Profile management sentinels (mission: multi-account-login-switch).
// These represent ipa-manager's own command errors (AC-07-3 scope).
var (
	// ErrProfileNotFound is returned when a referenced profile ID does not
	// exist in the config. CLI layer formats this with a "Run `accounts list`"
	// hint.
	ErrProfileNotFound = errors.New("profile not found")

	// ErrProfileNotLoggedIn is returned when an operation requires credentials
	// but the profile's keychain entry is absent. CLI layer formats this with a
	// "Run `auth login`" hint.
	ErrProfileNotLoggedIn = errors.New("profile has no credentials")

	// ErrNoActiveProfile is returned when an operation defaults to the active
	// profile but none is set. CLI layer formats this with a "Run `accounts use`"
	// hint.
	ErrNoActiveProfile = errors.New("no active profile")
)
