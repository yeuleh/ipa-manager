package account

import "github.com/yeuleh/ipa-manager/internal/apperr"

// Store reads and writes account profiles.
//
// TODO(mission): back by ~/.ipa-manager/config.json.
type Store struct{}

// List returns all configured profiles.
func (s *Store) List() ([]Profile, error) { return nil, apperr.ErrNotImplemented }

// Get returns a profile by ID.
func (s *Store) Get(id string) (Profile, error) { return Profile{}, apperr.ErrNotImplemented }

// Add creates a profile.
func (s *Store) Add(p Profile) error { return apperr.ErrNotImplemented }

// Remove deletes a profile and MUST also revoke its keychain namespace
// and delete its per-profile cookie jar (consistency requirement).
func (s *Store) Remove(id string) error { return apperr.ErrNotImplemented }
