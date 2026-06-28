// Package library manages the local, per-account-isolated IPA file store.
//
// Per the Stage 4 design, the IPA inventory lives here (NOT in config), so
// that global config stays small and library concerns are independently testable.
package library

import "github.com/yeuleh/ipa-manager/internal/apperr"

// IPAStore owns the on-disk layout of downloaded IPAs, isolated per account.
type IPAStore struct{}

// Path returns the per-account IPA directory.
func (s *IPAStore) Path(profileID, configRoot string) string {
	// TODO(mission): finalize layout, e.g. <configRoot>/library/<profileID>/
	return profileID + "/" + configRoot
}

// Add registers a downloaded IPA in the profile's store.
func (s *IPAStore) Add(profileID, path string) error { return apperr.ErrNotImplemented }

// List lists IPAs belonging to an account.
func (s *IPAStore) List(profileID string) ([]string, error) {
	return nil, apperr.ErrNotImplemented
}
