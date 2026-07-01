package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/appstore"
)

// =============================================================================
// AC-10-1: --output saves to custom path
// =============================================================================

func TestDownload_Output_CustomPath(t *testing.T) {
	configRoot := t.TempDir()
	customPath := filepath.Join(configRoot, "custom.ipa")

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: customPath, Version: "unknown"}},
		downloadErrors:    []error{nil},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, configRoot)

	output, err := helperRunDownloadCmd(t, deps, "com.test", "--output", customPath)
	require.NoError(t, err)
	assert.Contains(t, output, "Downloaded")
	assert.Contains(t, output, customPath)
}

// =============================================================================
// AC-10-3: --output already exists (no --force)
// =============================================================================

func TestDownload_Output_Exists_Skips(t *testing.T) {
	configRoot := t.TempDir()
	customPath := filepath.Join(configRoot, "existing.ipa")
	require.NoError(t, os.WriteFile(customPath, []byte("exists"), 0o644))

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, configRoot)

	output, err := helperRunDownloadCmd(t, deps, "com.test", "--output", customPath)
	require.NoError(t, err)
	assert.Contains(t, output, "already exists")
	assert.Equal(t, 0, mockAS.downloadCalls, "Download should NOT be called")
}

// =============================================================================
// AC-10-4: --output parent dir missing
// =============================================================================

func TestDownload_Output_ParentMissing_Error(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test", "--output", "/nonexistent/dir/app.ipa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output directory does not exist")
}

// =============================================================================
// AC-10-5: --output is a directory
// =============================================================================

func TestDownload_Output_IsDirectory_Error(t *testing.T) {
	dir := t.TempDir()

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test", "--output", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output path is a directory")
}

// =============================================================================
// AC-09-1: --external-version-id downloads specific version
// =============================================================================

func TestDownload_ExternalVersionID_BypassesSkip(t *testing.T) {
	configRoot := t.TempDir()
	// Create a file that WOULD match the latest version skip path
	skipDir := filepath.Join(configRoot, "library", "alice_test")
	require.NoError(t, os.MkdirAll(skipDir, 0o700))
	skipFile := filepath.Join(skipDir, "com.test_123_1.0.0.ipa")
	require.NoError(t, os.WriteFile(skipFile, []byte("latest"), 0o644))

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0.0"},
		// Lookup returns latest (1.0.0) but we're requesting old version via --external-version-id
		downloadResults: []appstore.DownloadResult{{DestinationPath: "/tmp/old.ipa", Version: "0.9.0"}},
		downloadErrors:  []error{nil},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, configRoot)

	output, err := helperRunDownloadCmd(t, deps, "com.test", "--external-version-id", "abc123")
	require.NoError(t, err)
	// Skip should be BYPASSED — download proceeds even though latest version file exists
	assert.Contains(t, output, "Downloaded")
	assert.Equal(t, 1, mockAS.downloadCalls, "Download should proceed (skip bypassed for --external-version-id)")
}

// =============================================================================
// AC-09-2: --external-version-id invalid version
// =============================================================================

func TestDownload_ExternalVersionID_Invalid_Error(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
		downloadErrors:    []error{assertError("invalid version id")},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test", "--external-version-id", "invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version")
}
