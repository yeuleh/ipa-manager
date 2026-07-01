package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/library"
)

func helperRunDownloadCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := appDownloadCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func helperMakeDownloadDeps(store account.Store, mockAS *mockAppStore, libStore library.Store, configRoot string) Deps {
	return Deps{
		Store:        store,
		LibraryStore: libStore,
		ConfigRoot:   configRoot,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
			return mockAS, nil
		},
	}
}

func helperDownloadStore() *mockStore {
	return &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com", Name: "Alice"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
}

// =============================================================================
// E2E-007 / AC-02-1: Download happy path
// =============================================================================

func TestDownload_HappyPath(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "alice@test.com"},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "TestApp", Version: "1.0.0", Price: 0},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/test.ipa", Version: "1.0.0", Sinfs: []appstore.Sinf{}}},
		downloadErrors:    []error{nil},
	}
	libStore := &mockLibraryStore{}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, t.TempDir())

	output, err := helperRunDownloadCmd(t, deps, "com.test")
	require.NoError(t, err)
	assert.Contains(t, output, "Downloaded")
	assert.Contains(t, output, "TestApp")
	assert.True(t, libStore.addCalled, "LibraryStore.Add should be called")
	assert.Equal(t, "alice_test", libStore.addProfile)
	assert.Equal(t, "com.test", libStore.addEntry.BundleID)
}

// =============================================================================
// E2E-009 / AC-02-3: Download no active profile
// =============================================================================

func TestDownload_NoActiveProfile(t *testing.T) {
	store := &mockStore{activeID: "", credentials: map[string]bool{}}
	deps := helperMakeDownloadDeps(store, &mockAppStore{}, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
}

// =============================================================================
// E2E-010 / AC-02-4: Download app not found
// =============================================================================

func TestDownload_AppNotFound(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupErr:         assertError("app not found"),
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app not found")
	assert.Contains(t, err.Error(), "com.nonexistent")
}

// =============================================================================
// E2E-016 / AC-02-9: Download with --profile
// =============================================================================

func TestDownload_WithProfileFlag(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true, "bob_test": true},
	}
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/test.ipa", Version: "1.0"}},
		downloadErrors:    []error{nil},
	}
	libStore := &mockLibraryStore{}
	var factoryProfile string
	deps := Deps{
		Store:        store,
		LibraryStore: libStore,
		ConfigRoot:   t.TempDir(),
		AppStoreFactory: func(p account.Profile) (appstore.ProfileAppStore, error) {
			factoryProfile = p.ID
			return mockAS, nil
		},
	}

	_, err := helperRunDownloadCmd(t, deps, "com.test", "--profile", "bob_test")
	require.NoError(t, err)
	assert.Equal(t, "bob_test", factoryProfile)
	assert.Equal(t, "bob_test", libStore.addProfile)
}

// =============================================================================
// E2E-035 / AC-08-1: Download profile not found
// =============================================================================

func TestDownload_ProfileNotFound(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	deps := helperMakeDownloadDeps(store, &mockAppStore{}, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test", "--profile", "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// E2E-046 / AC-11-1: Download new version keeps old version
// =============================================================================

func TestDownload_NewVersion_KeepsOldVersion(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "8.0.35"},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/v835.ipa", Version: "8.0.35"}},
		downloadErrors:    []error{nil},
	}
	// Library already has 8.0.34
	libStore := &mockLibraryStore{
		entries: []library.Entry{
			{BundleID: "com.test", Version: "8.0.34", FilePath: "/tmp/v834.ipa"},
		},
	}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test")
	require.NoError(t, err)
	// Add should be called (adds 8.0.35, doesn't touch 8.0.34)
	assert.True(t, libStore.addCalled)
	assert.Equal(t, "8.0.35", libStore.addEntry.Version)
	// Existing entry should still be there (mockLibraryStore doesn't actually remove)
	assert.Len(t, libStore.entries, 1, "old entry should still be in mock state")
}

// =============================================================================
// E2E-047 / AC-11-2 / AC-02-5: Download same version skips (physical file exists)
// =============================================================================

func TestDownload_SameVersion_Skips(t *testing.T) {
	configRoot := t.TempDir()
	// Pre-create the skip-path file (simulates same version already downloaded)
	skipDir := filepath.Join(configRoot, "library", "alice_test")
	require.NoError(t, os.MkdirAll(skipDir, 0o700))
	skipFile := filepath.Join(skipDir, "com.test_123_1.0.0.ipa")
	require.NoError(t, os.WriteFile(skipFile, []byte("fake"), 0o644))

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0.0"},
	}
	libStore := &mockLibraryStore{}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, configRoot)

	output, err := helperRunDownloadCmd(t, deps, "com.test")
	require.NoError(t, err)
	assert.Contains(t, output, "already exists")
	assert.Equal(t, 0, mockAS.downloadCalls, "Download should NOT be called for same version")
}
