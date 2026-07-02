// Package ui provides interactive prompts (huh) and styled output (lipgloss).
package ui

import (
	"errors"

	"charm.land/huh/v2"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// prompter is the production implementation of Prompter using huh v2.
type prompter struct{}

// NewPrompter returns a production Prompter backed by huh v2.
func NewPrompter() Prompter {
	return &prompter{}
}

func (p *prompter) InputEmail() (string, error) {
	var email string
	err := huh.NewInput().
		Title("Apple ID email").
		Value(&email).
		Run()
	return email, err
}

func (p *prompter) InputPassword() (string, error) {
	var password string
	err := huh.NewInput().
		Title("Apple ID password").
		EchoMode(huh.EchoModePassword).
		Value(&password).
		Run()
	return password, err
}

func (p *prompter) InputAuthCode() (string, error) {
	var code string
	err := huh.NewInput().
		Title("2FA verification code").
		Value(&code).
		Run()
	return code, err
}

func (p *prompter) Confirm(title string) (bool, error) {
	var result bool
	err := huh.NewConfirm().
		Title(title).
		Value(&result).
		Run()
	return result, err
}

func (p *prompter) SelectProfile(profiles []account.Profile, activeID string) (string, error) {
	var selected string
	opts := make([]huh.Option[string], 0, len(profiles))
	for _, prof := range profiles {
		label := prof.Email
		if prof.Name != "" {
			label = prof.Name + " <" + prof.Email + ">"
		}
		if prof.ID == activeID {
			label += " (active)"
		}
		opts = append(opts, huh.NewOption(label, prof.ID))
	}
	err := huh.NewSelect[string]().
		Title("Select profile").
		Options(opts...).
		Value(&selected).
		Run()
	return selected, err
}

// SelectDevice shows an interactive device picker. User abort (Ctrl+C/Esc)
// maps to apperr.ErrCancelled so callers can treat it as exit 0.
func (p *prompter) SelectDevice(options []DeviceOption) (string, error) {
	var selected string
	opts := make([]huh.Option[string], 0, len(options))
	for _, o := range options {
		opts = append(opts, huh.NewOption(o.Label, o.UDID))
	}
	err := huh.NewSelect[string]().
		Title("Select device").
		Options(opts...).
		Value(&selected).
		Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", apperr.ErrCancelled
		}
		return "", err
	}
	return selected, nil
}
