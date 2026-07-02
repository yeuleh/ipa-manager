package appstore

import (
	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"

	"github.com/99designs/keyring"
)

// NewBaseKeychain constructs an ipatool keychain from a keyring.
// This is the ONLY place outside client_impl.go that touches ipatool keychain.
// Exposed so cli/deps.go can construct the keychain without importing ipatool (NFR-08).
func NewBaseKeychain(ring keyring.Keyring) ipakeychain.Keychain {
	return ipakeychain.New(ipakeychain.Args{Keyring: ring})
}
