package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/library"
)

// =============================================================================
// mockLibraryStore implements library.Store with configurable state
// =============================================================================

type mockLibraryStore struct {
	entries []library.Entry
	listErr error

	// Call tracking
	addCalled   bool
	addProfile  string
	addEntry    library.Entry
	listProfile string
	getProfile  string
	getBundle   string

	// For Remove/RemoveVersion/CleanAll (T6)
	removeProfile  string
	removeBundle   string
	removeCount    int
	removeErr      error
	removeVerErr   error
	cleanAllCount  int
	cleanAllErr    error
}

func (m *mockLibraryStore) Add(profileID string, entry library.Entry) error {
	m.addCalled = true
	m.addProfile = profileID
	m.addEntry = entry
	return nil
}

func (m *mockLibraryStore) List(profileID string) ([]library.Entry, error) {
	m.listProfile = profileID
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]library.Entry, len(m.entries))
	copy(result, m.entries)
	return result, nil
}

func (m *mockLibraryStore) Get(profileID, bundleID string) ([]library.Entry, error) {
	m.getProfile = profileID
	m.getBundle = bundleID
	var result []library.Entry
	for _, e := range m.entries {
		if e.BundleID == bundleID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockLibraryStore) GetVersion(profileID, bundleID, version string) (library.Entry, error) {
	for _, e := range m.entries {
		if e.BundleID == bundleID && e.Version == version {
			return e, nil
		}
	}
	return library.Entry{}, library.ErrEntryNotFound
}

func (m *mockLibraryStore) Remove(profileID, bundleID string) (int, error) {
	m.removeProfile = profileID
	m.removeBundle = bundleID
	return m.removeCount, m.removeErr
}

func (m *mockLibraryStore) RemoveVersion(profileID, bundleID, version string) error {
	return m.removeVerErr
}

func (m *mockLibraryStore) CleanAll(profileID string) (int, error) {
	return m.cleanAllCount, m.cleanAllErr
}

// =============================================================================
// helpers
// =============================================================================

func helperRunLibraryListCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := libraryListCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func helperMakeLibraryDeps(store account.Store, libStore library.Store) Deps {
	return Deps{
		Store:        store,
		LibraryStore: libStore,
	}
}

// =============================================================================
// E2E-021 / AC-04-1: Library list happy path (multi-version)
// =============================================================================

func TestLibraryList_HappyPath(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": false}, // library doesn't need creds
	}
	libStore := &mockLibraryStore{
		entries: []library.Entry{
			{BundleID: "com.tencent.xin", Version: "8.0.35", FileSize: 240111222, FilePath: "/lib/alice/com.tencent.xin_123_8.0.35.ipa", DownloadedAt: time.Now().UTC()},
			{BundleID: "com.tencent.xin", Version: "8.0.34", FileSize: 234567890, FilePath: "/lib/alice/com.tencent.xin_123_8.0.34.ipa", DownloadedAt: time.Now().UTC()},
			{BundleID: "com.example.app", Version: "1.2.3", FileSize: 45678901, FilePath: "/tmp/custom/app.ipa", DownloadedAt: time.Now().UTC()},
		},
	}
	deps := helperMakeLibraryDeps(store, libStore)

	output, err := helperRunLibraryListCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "com.tencent.xin")
	assert.Contains(t, output, "8.0.35")
	assert.Contains(t, output, "8.0.34")
	assert.Contains(t, output, "com.example.app")
	assert.Contains(t, output, "PATH") // T1-01 fix: header includes PATH column
}

// =============================================================================
// E2E-022 / AC-04-2: Library list empty
// =============================================================================

func TestLibraryList_Empty(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{},
	}
	libStore := &mockLibraryStore{entries: []library.Entry{}}
	deps := helperMakeLibraryDeps(store, libStore)

	output, err := helperRunLibraryListCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "no IPAs in library")
	assert.Contains(t, output, "alice_test")
}

// =============================================================================
// E2E-023 / AC-04-3: Library list with --profile
// =============================================================================

func TestLibraryList_WithProfileFlag(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{},
	}
	libStore := &mockLibraryStore{entries: []library.Entry{}}
	deps := helperMakeLibraryDeps(store, libStore)

	_, err := helperRunLibraryListCmd(t, deps, "--profile", "bob_test")
	require.NoError(t, err)
	assert.Equal(t, "bob_test", libStore.listProfile, "List should be called with bob_test")
}
