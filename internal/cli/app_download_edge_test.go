package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/library"
)

// =============================================================================
// E2E-012 / AC-02-6: Download already exists with --force
// =============================================================================

func TestDownload_Force_Overwrites(t *testing.T) {
	configRoot := t.TempDir()
	skipDir := filepath.Join(configRoot, "library", "alice_test")
	require.NoError(t, os.MkdirAll(skipDir, 0o700))
	skipFile := filepath.Join(skipDir, "com.test_123_1.0.0.ipa")
	require.NoError(t, os.WriteFile(skipFile, []byte("old"), 0o644))

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0.0"},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/test.ipa", Version: "1.0.0"}},
		downloadErrors:    []error{nil},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, configRoot)

	output, err := helperRunDownloadCmd(t, deps, "com.test", "--force")
	require.NoError(t, err)
	assert.Contains(t, output, "Downloaded")
	assert.Equal(t, 1, mockAS.downloadCalls, "Download should be called with --force")
}

// =============================================================================
// E2E-013 / AC-02-7: License required (free, interactive, yes)
// =============================================================================

func TestDownload_LicenseRequired_FreeApp_UserYes(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.free", Name: "Free", Version: "1.0", Price: 0},
		downloadResults:   []appstore.DownloadResult{{}, {DestinationPath: "/tmp/free.ipa", Version: "1.0"}},
		downloadErrors:    []error{apperr.ErrLicenseRequired, nil},
	}
	libStore := &mockLibraryStore{}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, t.TempDir())
	deps.UI = &mockPrompter{confirm: true}

	_, err := helperRunDownloadCmd(t, deps, "com.free")
	require.NoError(t, err)
	assert.True(t, mockAS.purchaseCalled, "Purchase should be called after user confirms")
	assert.Equal(t, 2, mockAS.downloadCalls, "Download should retry after license acquisition")
}

// =============================================================================
// E2E-014 / AC-02-7: License required (free, interactive, no)
// =============================================================================

func TestDownload_LicenseRequired_FreeApp_UserNo(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.free", Name: "Free", Version: "1.0", Price: 0},
		downloadResults:   []appstore.DownloadResult{{}},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())
	deps.UI = &mockPrompter{confirm: false}

	output, err := helperRunDownloadCmd(t, deps, "com.free")
	require.NoError(t, err, "user decline is not an error")
	assert.Contains(t, output, "cancelled")
	assert.False(t, mockAS.purchaseCalled, "Purchase should NOT be called")
	assert.Equal(t, 1, mockAS.downloadCalls, "Download should not retry")
}

// =============================================================================
// E2E-015 / AC-02-8: License required (paid app)
// =============================================================================

func TestDownload_LicenseRequired_PaidApp_Error(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.paid", Name: "Paid", Version: "1.0", Price: 9.99},
		downloadResults:   []appstore.DownloadResult{{}},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())
	deps.UI = &mockPrompter{confirm: true}

	_, err := helperRunDownloadCmd(t, deps, "com.paid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paid apps")
}

// =============================================================================
// E2E-017 / AC-02-10: Token expired (auto re-login)
// =============================================================================

func TestDownload_TokenExpired_AutoRelogin(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
		// First Download fails with token expired, second succeeds
		downloadResults: []appstore.DownloadResult{{}, {DestinationPath: "/tmp/test.ipa", Version: "1.0"}},
		downloadErrors:  []error{apperr.ErrPasswordTokenExpired, nil},
	}
	libStore := &mockLibraryStore{}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, t.TempDir())

	output, err := helperRunDownloadCmd(t, deps, "com.test")
	require.NoError(t, err)
	assert.Contains(t, output, "Downloaded")
	assert.True(t, mockAS.refreshSessionCalled, "RefreshSession should be called for token expiry")
	assert.Equal(t, 2, mockAS.downloadCalls, "Download should retry after refresh")
}

func TestDownload_TokenExpired_ReloginFails(t *testing.T) {
	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.test", Name: "Test", Version: "1.0"},
		downloadErrors:    []error{apperr.ErrPasswordTokenExpired},
		refreshSessionErr: assertError("re-login failed"),
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())

	_, err := helperRunDownloadCmd(t, deps, "com.test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-login failed")
}

// Ensure library import is used (for mockLibraryStore in test helpers)
var _ library.Store = (*mockLibraryStore)(nil)
