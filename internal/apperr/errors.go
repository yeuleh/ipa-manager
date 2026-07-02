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

	// Download sentinels (mission: download-ipa-by-account).
	ErrAppNotFound            = errors.New("app not found")
	ErrReplicateSinfFailed    = errors.New("failed to apply DRM keys")
	ErrLicenseRequired        = errors.New("license is required")
	ErrPasswordTokenExpired   = errors.New("password token is expired")
	ErrPaidAppNotSupported    = errors.New("paid apps are not supported")
	ErrDownloadNonInteractive = errors.New("interactive confirmation required")
)

// Device sentinels (mission: ios-device-manage).
var (
	// ErrCancelled is returned when the user aborts an interactive prompt
	// (device selection, license confirmation). Treated as exit 0 (not an
	// error outcome).
	ErrCancelled = errors.New("cancelled")

	// ErrDeviceNotConnected indicates no connected device, or a --udid that
	// does not match any connected device.
	ErrDeviceNotConnected = errors.New("no connected device")

	// ErrMultipleDevices indicates more than one device is connected and none
	// was selected via --udid (and interactive selection is unavailable).
	ErrMultipleDevices = errors.New("multiple devices connected")

	// ErrAppNotInstalled indicates an uninstall was attempted for a bundle-id
	// that is not installed on the device (operate-stage; never a tunnel issue).
	ErrAppNotInstalled = errors.New("app not installed on device")
)
