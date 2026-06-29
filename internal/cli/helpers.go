package cli

import (
	"errors"
	"strings"

	"github.com/99designs/keyring"
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
