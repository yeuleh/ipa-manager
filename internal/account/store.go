package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/yeuleh/ipa-manager/internal/apperr"

	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
)

// Store is the interface for profile CRUD + active pointer + credential state.
// Used as Deps.Store type in CLI layer (design DD-12).
type Store interface {
	// Lifecycle
	Load() error  // read config.json into memory; absent file = empty state (not error)
	Save() error  // persist in-memory state atomically (tmp + rename)

	// Read
	List() ([]Profile, error)
	Get(id string) (Profile, error)
	GetActiveID() (string, error)
	HasCredentials(id string) (bool, error)

	// Mutate (in-memory only; caller must Save to persist)
	Upsert(p Profile) error
	Remove(id string) error // enforces active-clearing invariant
	SetActive(id string) error
	ClearActive() error
}

// profileFile is the JSON serialization target for config.json.
type profileFile struct {
	ActiveProfileID string    `json:"active_profile_id,omitempty"`
	Profiles        []Profile `json:"profiles,omitempty"`
}

// store is the concrete implementation of Store.
type store struct {
	configPath   string
	baseKeychain ipakeychain.Keychain // shared base for HasCredentials probes
	state        profileFile
	loaded       bool
	mu           sync.Mutex
}

// NewStore constructs a Store backed by the given config path and base keychain.
// The base keychain is used for HasCredentials probes; it is wrapped in
// ProfileKeychain per-profile inside HasCredentials.
func NewStore(configPath string, baseKeychain ipakeychain.Keychain) Store {
	return &store{
		configPath:   configPath,
		baseKeychain: baseKeychain,
	}
}

// Load reads config.json into memory. If the file does not exist, initializes
// empty state (this is not an error — AC-03-1: empty list is valid).
func (s *store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.state = profileFile{}
			s.loaded = true
			return nil
		}
		return fmt.Errorf("failed to read config: %w", err)
	}

	var pf profileFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	s.state = pf
	s.loaded = true
	return nil
}

// Save persists in-memory state to config.json atomically (write tmp + rename).
// Returns an error if Load() has not been called — this prevents accidental
// overwrite of existing config with zero state (Spok finding 3).
func (s *store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.loaded {
		return fmt.Errorf("store has not been loaded; call Load() before Save()")
	}

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Atomic write: tmp file + rename (DD-10).
	tmpPath := s.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.configPath); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("failed to rename config: %w", err)
	}
	return nil
}

// List returns a copy of all profiles.
func (s *store) List() ([]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Profile, len(s.state.Profiles))
	copy(result, s.state.Profiles)
	return result, nil
}

// Get returns the profile with the given ID.
func (s *store) Get(id string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.state.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, apperr.ErrProfileNotFound
}

// GetActiveID returns the active profile ID (empty string if none).
func (s *store) GetActiveID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.ActiveProfileID, nil
}

// Upsert adds or updates a profile by ID.
func (s *store) Upsert(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.state.Profiles {
		if existing.ID == p.ID {
			s.state.Profiles[i] = p
			return nil
		}
	}
	s.state.Profiles = append(s.state.Profiles, p)
	return nil
}

// Remove deletes a profile by ID. Enforces invariant: if the removed profile
// was active, active is cleared (design DD-04).
func (s *store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.state.Profiles {
		if p.ID == id {
			s.state.Profiles = append(s.state.Profiles[:i], s.state.Profiles[i+1:]...)
			if s.state.ActiveProfileID == id {
				s.state.ActiveProfileID = ""
			}
			return nil
		}
	}
	return apperr.ErrProfileNotFound
}

// SetActive sets the active profile ID. Empty string is allowed (clears active).
func (s *store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ActiveProfileID = id
	return nil
}

// ClearActive clears the active profile ID.
func (s *store) ClearActive() error {
	return s.SetActive("")
}

// HasCredentials checks whether the profile has a keychain entry (logged-in).
// Constructs a ProfileKeychain wrapper around the shared base keychain and
// probes for the "account" key. Returns true only if the key exists AND the
// data is non-empty (design DD-06).
func (s *store) HasCredentials(id string) (bool, error) {
	pk := ProfileKeychain{Base: s.baseKeychain, ProfileID: id}
	data, err := pk.Get("account")
	return err == nil && len(data) > 0, nil
}
