// Package library manages the local, per-account-isolated IPA file store.
//
// Supports multiple versions per bundle-id (composite key: bundle_id + version).
// Each profile has an independent directory under <libraryRoot>/<profileID>/
// containing index.json (metadata) and *.ipa files.
package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrEntryNotFound is returned when a requested entry does not exist in the index.
var ErrEntryNotFound = fmt.Errorf("entry not found in library")

// Entry is a single IPA record in the library index.
// Unique by (bundle_id, version) composite key within a profile.
type Entry struct {
	BundleID     string    `json:"bundle_id"`
	AppID        int64     `json:"app_id"`
	Version      string    `json:"version"`
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

// indexFile is the JSON serialization target for index.json.
type indexFile struct {
	Entries []Entry `json:"entries"`
}

// Store manages the per-profile IPA library: file paths + metadata index.
// Supports multiple versions per bundle-id.
type Store interface {
	Add(profileID string, entry Entry) error
	List(profileID string) ([]Entry, error)
	Get(profileID, bundleID string) ([]Entry, error)
	GetVersion(profileID, bundleID, version string) (Entry, error)
	Remove(profileID, bundleID string) (int, error)
	RemoveVersion(profileID, bundleID, version string) error
	CleanAll(profileID string) (int, error)
}

type store struct {
	libraryRoot string
}

// NewStore constructs a Store backed by the given library root directory.
func NewStore(libraryRoot string) Store {
	return &store{libraryRoot: libraryRoot}
}

func (s *store) profileDir(profileID string) string {
	return filepath.Join(s.libraryRoot, profileID)
}

func (s *store) indexPath(profileID string) string {
	return filepath.Join(s.profileDir(profileID), "index.json")
}

// readIndex reads and parses index.json. Returns empty if file doesn't exist.
func (s *store) readIndex(profileID string) (indexFile, error) {
	data, err := os.ReadFile(s.indexPath(profileID))
	if err != nil {
		if os.IsNotExist(err) {
			return indexFile{Entries: []Entry{}}, nil
		}
		return indexFile{}, fmt.Errorf("failed to read index: %w", err)
	}
	var idx indexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return indexFile{}, fmt.Errorf("failed to parse index: %w", err)
	}
	if idx.Entries == nil {
		idx.Entries = []Entry{}
	}
	return idx, nil
}

// writeIndex atomically writes index.json (tmp + rename).
func (s *store) writeIndex(profileID string, idx indexFile) error {
	dir := s.profileDir(profileID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create library directory: %w", err)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	tmpPath := s.indexPath(profileID) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write index tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.indexPath(profileID)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename index: %w", err)
	}
	return nil
}

// Add inserts or replaces an entry by (bundle_id, version) composite key.
func (s *store) Add(profileID string, entry Entry) error {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return err
	}
	for i, e := range idx.Entries {
		if e.BundleID == entry.BundleID && e.Version == entry.Version {
			idx.Entries[i] = entry // replace existing
			return s.writeIndex(profileID, idx)
		}
	}
	idx.Entries = append(idx.Entries, entry)
	return s.writeIndex(profileID, idx)
}

// List returns all entries sorted by bundle_id then version descending.
func (s *store) List(profileID string) ([]Entry, error) {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(idx.Entries))
	copy(entries, idx.Entries)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].BundleID != entries[j].BundleID {
			return entries[i].BundleID < entries[j].BundleID
		}
		return entries[i].Version > entries[j].Version // descending version
	})
	return entries, nil
}

// Get returns ALL versions of a bundle-id (may be empty).
func (s *store) Get(profileID, bundleID string) ([]Entry, error) {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return nil, err
	}
	var result []Entry
	for _, e := range idx.Entries {
		if e.BundleID == bundleID {
			result = append(result, e)
		}
	}
	return result, nil
}

// GetVersion returns a specific version entry (or ErrEntryNotFound).
func (s *store) GetVersion(profileID, bundleID, version string) (Entry, error) {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range idx.Entries {
		if e.BundleID == bundleID && e.Version == version {
			return e, nil
		}
	}
	return Entry{}, ErrEntryNotFound
}

// Remove deletes ALL versions of a bundle-id + their files.
// Returns count of removed entries. ErrEntryNotFound if bundle-id not in index.
func (s *store) Remove(profileID, bundleID string) (int, error) {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return 0, err
	}
	var kept []Entry
	removed := 0
	for _, e := range idx.Entries {
		if e.BundleID == bundleID {
			if e.FilePath != "" {
				_ = os.Remove(e.FilePath) // best-effort; ignore not-exist
			}
			removed++
		} else {
			kept = append(kept, e)
		}
	}
	if removed == 0 {
		return 0, ErrEntryNotFound
	}
	idx.Entries = kept
	return removed, s.writeIndex(profileID, idx)
}

// RemoveVersion deletes a specific version + its file. ErrEntryNotFound if not found.
func (s *store) RemoveVersion(profileID, bundleID, version string) error {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return err
	}
	for i, e := range idx.Entries {
		if e.BundleID == bundleID && e.Version == version {
			if e.FilePath != "" {
				_ = os.Remove(e.FilePath) // best-effort
			}
			idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
			return s.writeIndex(profileID, idx)
		}
	}
	return ErrEntryNotFound
}

// CleanAll removes all IPA files + clears the index for the profile.
// Returns the count of removed entries.
func (s *store) CleanAll(profileID string) (int, error) {
	idx, err := s.readIndex(profileID)
	if err != nil {
		return 0, err
	}
	count := len(idx.Entries)
	for _, e := range idx.Entries {
		if e.FilePath != "" {
			_ = os.Remove(e.FilePath) // best-effort
		}
	}
	idx.Entries = []Entry{}
	return count, s.writeIndex(profileID, idx)
}
