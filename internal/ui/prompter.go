// Package ui provides interactive prompts (huh) and styled output (lipgloss).
package ui

import "github.com/yeuleh/ipa-manager/internal/account"

// DeviceOption is a selectable device in SelectDevice.
type DeviceOption struct {
	UDID  string
	Label string // human-readable, e.g. "iPhone 16 (iOS 17.5) — a1b2…"
}

// Prompter is the interface for all user interactions (design DD-12).
// Production implementation uses huh; tests inject mock implementations.
type Prompter interface {
	// SelectProfile shows an interactive account picker and returns the chosen ID.
	SelectProfile(profiles []account.Profile, activeID string) (string, error)
	// SelectDevice shows an interactive device picker and returns the chosen UDID.
	// Returns ErrCancelled if the user aborts.
	SelectDevice(options []DeviceOption) (string, error)
	// Confirm shows a yes/no prompt.
	Confirm(title string) (bool, error)
	// InputAuthCode prompts the user for a 2FA verification code.
	InputAuthCode() (string, error)
	// InputEmail prompts the user for an Apple ID email.
	InputEmail() (string, error)
	// InputPassword prompts the user for a password (masked input).
	InputPassword() (string, error)
}
