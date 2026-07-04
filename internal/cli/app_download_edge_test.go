package cli

import (
	"errors"
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

// =============================================================================
// Mission fix-purchase-token-expired (T2 / E2E-001/002/003 / AC-01-1/2/3 / NFR-05)
//
// 这些测试覆盖 handleLicenseRequired 中 Purchase token-expired 重试路径
// (app_download.go:244-254 的 inline retry 分支)。
//
// 与既有 TestDownload_TokenExpired_*(走 handleTokenExpired 路径)的区别:
// - 既有测试:Download 失败返回 ErrPasswordTokenExpired → 调 handleTokenExpired
// - 这些测试:Download 失败返回 ErrLicenseRequired → 进 handleLicenseRequired →
//   Purchase 失败返回 ErrPasswordTokenExpired → inline RefreshSession + Purchase retry
//
// 该 inline retry 分支**在本 mission 之前从未被测试执行过**(代码审查发现),
// 因为 fix 前 adapter Purchase 返回 ipatool 原始 sentinel,导致 CLI 的
// errors.Is(apperr.ErrPasswordTokenExpired) 永远 false,该分支永远走 else。
// =============================================================================

// TestHandleLicenseRequired_PurchaseTokenExpired_Retries 验证 E2E-001 / AC-01-1:
// Purchase 失败(token-expired)→ RefreshSession 成功 → Purchase 重试成功 → install 成功。
// 用户无需任何手动干预,stderr 不暴露 Apple 内部术语。
func TestHandleLicenseRequired_PurchaseTokenExpired_Retries(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.free", Name: "Free", Version: "1.0", Price: 0},
		// Download:第一次 ErrLicenseRequired(触发 handleLicenseRequired),第二次成功
		downloadResults: []appstore.DownloadResult{{}, {DestinationPath: "/tmp/free.ipa", Version: "1.0"}},
		downloadErrors:  []error{apperr.ErrLicenseRequired, nil},
		// Purchase:第一次 ErrPasswordTokenExpired(模拟 fix 后 adapter 转换),第二次成功
		purchaseErrors: []error{apperr.ErrPasswordTokenExpired, nil},
		// RefreshSession 成功(nil)
	}
	libStore := &mockLibraryStore{}
	deps := helperMakeDownloadDeps(store, mockAS, libStore, t.TempDir())
	deps.UI = &mockPrompter{confirm: true} // 用户确认 acquire

	output, err := helperRunDownloadCmd(t, deps, "com.free")

	require.NoError(t, err, "happy path: refresh + retry should succeed; got err: %v", err)
	assert.Equal(t, 2, mockAS.purchaseCalls, "Purchase should be called twice (initial fail + retry after refresh)")
	assert.Equal(t, 1, mockAS.refreshSessionCalls, "RefreshSession should be called once between the two Purchase calls")
	assert.Equal(t, 2, mockAS.downloadCalls, "Download should be called twice (initial license-required + retry after acquisition)")
	assert.True(t, libStore.addCalled, "library.Add should be called (IPA 入库)")
	assert.Contains(t, output, "license acquired, retrying download...",
		"handleLicenseRequired's success message should be printed (app_download.go:256)")
}

// TestHandleLicenseRequired_PurchaseTokenExpired_RefreshFails 验证 E2E-002 /
// AC-01-2 / NFR-05:Purchase token-expired → RefreshSession 也失败 → 友好错误,
// 不暴露 Apple 内部术语(STDQ / password token is expired)。
func TestHandleLicenseRequired_PurchaseTokenExpired_RefreshFails(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	store := helperDownloadStore()
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.free", Name: "Free", Version: "1.0", Price: 0},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
		purchaseErrors:    []error{apperr.ErrPasswordTokenExpired},
		// RefreshSession 失败:模拟用户在 Apple ID 后台修改了密码,keychain 缓存的旧密码失效
		refreshSessionErr: assertError("simulated keychain password invalid"),
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())
	deps.UI = &mockPrompter{confirm: true}

	_, err := helperRunDownloadCmd(t, deps, "com.free")

	require.Error(t, err, "refresh failure should propagate as error")
	// AC-01-2: error 应包含 "re-login failed:" 前缀(fmt.Errorf("re-login failed: %w", err))
	assert.Contains(t, err.Error(), "re-login failed:",
		"error should contain 're-login failed:' prefix")
	assert.Contains(t, err.Error(), "simulated keychain password invalid",
		"underlying refresh error should be wrapped and traceable")
	// NFR-05: 不应暴露 Apple 内部术语或底层 sentinel 字符串
	assert.NotContains(t, err.Error(), "STDQ",
		"NFR-05: Apple internal term 'STDQ' must NOT be exposed to user")
	assert.NotContains(t, err.Error(), "password token is expired",
		"NFR-05: raw Apple sentinel string must NOT be exposed (it's already converted to apperr sentinel before reaching CLI)")
	// Refresh 失败后不应重试 Purchase
	assert.Equal(t, 1, mockAS.purchaseCalls, "Purchase should be called once (no retry after refresh failure)")
	assert.Equal(t, 1, mockAS.refreshSessionCalls, "RefreshSession should be called once")
}

// TestHandleLicenseRequired_PurchaseNonTokenError_NoRefresh 验证 E2E-003 /
// AC-01-3:Purchase 失败但**非** token-expired → 不触发 RefreshSession,行为与
// fix 前完全一致(stderr 格式 "license acquisition failed: <原因>")。
func TestHandleLicenseRequired_PurchaseNonTokenError_NoRefresh(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	store := helperDownloadStore()
	networkErr := errors.New("network timeout") // 非 sentinel
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.free", Name: "Free", Version: "1.0", Price: 0},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
		purchaseErrors:    []error{networkErr},
		// refreshSessionErr 留 nil(若意外触发 refresh,会被检测到)
	}
	deps := helperMakeDownloadDeps(store, mockAS, &mockLibraryStore{}, t.TempDir())
	deps.UI = &mockPrompter{confirm: true}

	_, err := helperRunDownloadCmd(t, deps, "com.free")

	require.Error(t, err, "non-token Purchase error should propagate")
	// AC-01-3: 错误格式与 fix 前完全一致
	assert.Contains(t, err.Error(), "license acquisition failed:",
		"error format should be unchanged from pre-fix behavior (app_download.go:253)")
	assert.Contains(t, err.Error(), "network timeout",
		"original error should be preserved through %w wrapping")
	// 证明未触发 refresh(核心契约:非 token 错误走 else 分支)
	assert.Equal(t, 0, mockAS.refreshSessionCalls,
		"RefreshSession must NOT be called for non-token errors (proves AC-01-3 boundary)")
	assert.Equal(t, 1, mockAS.purchaseCalls,
		"Purchase should be called once (no retry for non-token errors)")
}
