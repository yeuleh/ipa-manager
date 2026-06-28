// Package ui provides interactive prompts (huh) and styled output (lipgloss).
package ui

import (
	"charm.land/huh/v2"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// SelectProfile shows an interactive account picker and returns the chosen id.
//
// TODO(mission): build options from the profile store.
func SelectProfile(labels []string) (string, error) {
	_ = huh.NewSelect[string]()
	return "", apperr.ErrNotImplemented
}

// Confirm shows a yes/no prompt.
func Confirm(title string) (bool, error) {
	_ = huh.NewConfirm()
	return false, apperr.ErrNotImplemented
}

// InputAuthCode prompts the user for a 2FA verification code.
func InputAuthCode() (string, error) {
	_ = huh.NewInput()
	return "", apperr.ErrNotImplemented
}
