package account

import (
	"fmt"

	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
)

// ProfileKeychain wraps an ipatool keychain, namespacing the fixed "account"
// key per profile so multiple Apple accounts can coexist in one keyring.
//
// Rationale: ipatool's AppStore hardcodes keychain key "account"; without
// namespacing, logging in a second account overwrites the first. Mapping
// "account" -> "profiles/<id>/account" per profile is what enables
// multi-account support.
type ProfileKeychain struct {
	Base      ipakeychain.Keychain
	ProfileID string
}

// Compile-time assertion that ProfileKeychain satisfies ipatool's Keychain
// interface. If ipatool changes the interface, this fails to compile.
var _ ipakeychain.Keychain = ProfileKeychain{}

func (k ProfileKeychain) mapKey(key string) string {
	return fmt.Sprintf("profiles/%s/%s", k.ProfileID, key)
}

func (k ProfileKeychain) Get(key string) ([]byte, error) {
	return k.Base.Get(k.mapKey(key))
}

func (k ProfileKeychain) Set(key string, data []byte) error {
	return k.Base.Set(k.mapKey(key), data)
}

func (k ProfileKeychain) Remove(key string) error {
	return k.Base.Remove(k.mapKey(key))
}
