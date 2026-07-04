package appstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/keyring"
	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

func TestKeychainServiceName(t *testing.T) {
	if KeychainServiceName != "ipa-manager" {
		t.Errorf("KeychainServiceName = %q, want %q", KeychainServiceName, "ipa-manager")
	}
}

func TestNewAppStoreFactory_ReturnsCallableFactory(t *testing.T) {
	factory := NewAppStoreFactory("/tmp/test-config-root")
	if factory == nil {
		t.Fatal("NewAppStoreFactory returned nil")
	}
}

func TestAppStoreFactory_Type(t *testing.T) {
	var _ AppStoreFactory = NewAppStoreFactory("/tmp/test")
}

// TestNewProfileAppStore_WiringWithMockKeyring verifies that NewProfileAppStore
// correctly wires ProfileKeychain (namespace isolation) + cookie jar + keyring
// config. Uses keyringOpener injection + white-box access to adapter.inner.
func TestNewProfileAppStore_WiringWithMockKeyring(t *testing.T) {
	// Pre-populate mock keyring with account data at the expected namespace key.
	mockKeyring := keyring.NewArrayKeyring([]keyring.Item{
		{
			Key:  "profiles/test_profile/account",
			Data: []byte(`{"email":"test@example.com","name":"Test User","directoryServicesIdentifier":"123","passwordToken":"tok"}`),
		},
	})

	var capturedConfig keyring.Config
	origOpener := keyringOpener
	defer func() { keyringOpener = origOpener }()
	keyringOpener = func(c keyring.Config) (keyring.Keyring, error) {
		capturedConfig = c
		return mockKeyring, nil
	}

	dir := t.TempDir()
	store, err := NewProfileAppStore(
		account.Profile{ID: "test_profile", Email: "test@example.com"},
		dir,
	)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Verify keyring config (DD-02).
	assert.Equal(t, "ipa-manager", capturedConfig.ServiceName)
	assert.Equal(t, []keyring.BackendType{keyring.KeychainBackend}, capturedConfig.AllowedBackends)
	assert.Equal(t, filepath.Join(dir, "keychain"), capturedConfig.FileDir)

	// White-box: access adapter.inner to verify ProfileKeychain namespace.
	// ProfileKeychain maps "account" → "profiles/test_profile/account".
	adapter, ok := store.(*profileAppStoreAdapter)
	require.True(t, ok, "store should be *profileAppStoreAdapter")
	output, err := adapter.inner.AccountInfo()
	require.NoError(t, err, "AccountInfo should succeed via ProfileKeychain namespace")
	assert.Equal(t, "test@example.com", output.Account.Email)
	assert.Equal(t, "Test User", output.Account.Name)

	// Verify cookie jar directory was created.
	cookieDir := filepath.Join(dir, "profiles", "test_profile")
	info, err := os.Stat(cookieDir)
	require.NoError(t, err, "cookie jar directory should exist at %s", cookieDir)
	assert.True(t, info.IsDir())
}

// TestNewProfileAppStore_NamespaceIsolation_VerifyDifferentProfiles tests
// that two ProfileAppStores constructed with different ProfileIDs access
// different keychain entries (core isolation guarantee, ADR 0002).
func TestNewProfileAppStore_NamespaceIsolation_VerifyDifferentProfiles(t *testing.T) {
	mockKeyring := keyring.NewArrayKeyring([]keyring.Item{
		{
			Key:  "profiles/alice_test/account",
			Data: []byte(`{"email":"alice@test.com","name":"Alice","directoryServicesIdentifier":"1","passwordToken":"a"}`),
		},
		{
			Key:  "profiles/bob_test/account",
			Data: []byte(`{"email":"bob@test.com","name":"Bob","directoryServicesIdentifier":"2","passwordToken":"b"}`),
		},
	})

	origOpener := keyringOpener
	defer func() { keyringOpener = origOpener }()
	keyringOpener = func(c keyring.Config) (keyring.Keyring, error) {
		return mockKeyring, nil
	}

	dir := t.TempDir()

	aliceStore, err := NewProfileAppStore(account.Profile{ID: "alice_test"}, dir)
	require.NoError(t, err)
	bobStore, err := NewProfileAppStore(account.Profile{ID: "bob_test"}, dir)
	require.NoError(t, err)

	// White-box: verify each adapter sees only its own namespace.
	aliceAdapter := aliceStore.(*profileAppStoreAdapter)
	bobAdapter := bobStore.(*profileAppStoreAdapter)

	aliceInfo, err := aliceAdapter.inner.AccountInfo()
	require.NoError(t, err)
	assert.Equal(t, "Alice", aliceInfo.Account.Name)

	bobInfo, err := bobAdapter.inner.AccountInfo()
	require.NoError(t, err)
	assert.Equal(t, "Bob", bobInfo.Account.Name)
}

// Compile-time assertion that ProfileAppStore does NOT expose ipatool types.
// If this compiles, the public interface is clean.
func TestProfileAppStore_Interface(t *testing.T) {
	var _ ProfileAppStore = (*profileAppStoreAdapter)(nil)
	// LoginInput and LoginResult have only string fields — no ipatool leak.
	input := LoginInput{Email: "e", Password: "p", AuthCode: "a", Endpoint: "ep"}
	result := LoginResult{Name: "n", Email: "e", StoreFront: "s"}
	_ = input
	_ = result
	_ = ipaappstore.AppStore(nil) // ipatool import only used in adapter tests (white-box)
}

// mockIPatoolAppStore is a minimal mock of ipaappstore.AppStore (11 methods)
// for adapter unit tests. Only methods exercised by tests have real behavior;
// others panic so unintended calls surface during test development.
//
// Used by T1 tests (E2E-004/005) of mission fix-purchase-token-expired.
type mockIPatoolAppStore struct {
	// Purchase
	purchaseErr   error
	purchaseCalls int
	// AccountInfo (needed because adapter requires a.account != nil before Purchase)
	accountInfoResult ipaappstore.AccountInfoOutput
	accountInfoErr    error
}

func (m *mockIPatoolAppStore) Purchase(input ipaappstore.PurchaseInput) error {
	m.purchaseCalls++
	return m.purchaseErr
}

func (m *mockIPatoolAppStore) AccountInfo() (ipaappstore.AccountInfoOutput, error) {
	return m.accountInfoResult, m.accountInfoErr
}

// All other 9 methods panic by default (not exercised by adapter Purchase tests).
func (m *mockIPatoolAppStore) Login(ipaappstore.LoginInput) (ipaappstore.LoginOutput, error) {
	panic("Login: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) Revoke() error {
	panic("Revoke: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) Lookup(ipaappstore.LookupInput) (ipaappstore.LookupOutput, error) {
	panic("Lookup: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) Search(ipaappstore.SearchInput) (ipaappstore.SearchOutput, error) {
	panic("Search: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) Download(ipaappstore.DownloadInput) (ipaappstore.DownloadOutput, error) {
	panic("Download: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) ReplicateSinf(ipaappstore.ReplicateSinfInput) error {
	panic("ReplicateSinf: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) ListVersions(ipaappstore.ListVersionsInput) (ipaappstore.ListVersionsOutput, error) {
	panic("ListVersions: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) GetVersionMetadata(ipaappstore.GetVersionMetadataInput) (ipaappstore.GetVersionMetadataOutput, error) {
	panic("GetVersionMetadata: not expected in adapter Purchase test")
}
func (m *mockIPatoolAppStore) Bag(ipaappstore.BagInput) (ipaappstore.BagOutput, error) {
	panic("Bag: not expected in adapter Purchase test")
}

// Compile-time assertion that mockIPatoolAppStore satisfies ipaappstore.AppStore.
var _ ipaappstore.AppStore = (*mockIPatoolAppStore)(nil)

// TestPurchase_TokenExpired_ConvertsToApperrSentinel verifies the fix for
// mission fix-purchase-token-expired (E2E-004): when ipatool's Purchase
// returns ErrPasswordTokenExpired, the adapter MUST convert it to
// apperr.ErrPasswordTokenExpired so the CLI layer's errors.Is match succeeds
// and the auto-refresh recovery path in handleLicenseRequired is triggered.
func TestPurchase_TokenExpired_ConvertsToApperrSentinel(t *testing.T) {
	inner := &mockIPatoolAppStore{
		purchaseErr: ipaappstore.ErrPasswordTokenExpired,
	}
	adapter := &profileAppStoreAdapter{
		inner:   inner,
		account: &ipaappstore.Account{Email: "test@example.com"},
	}

	err := adapter.Purchase("com.test", 123, 0)

	require.Error(t, err, "Purchase with token-expired error must return error")
	assert.True(t, errors.Is(err, apperr.ErrPasswordTokenExpired),
		"Purchase should convert ipatool ErrPasswordTokenExpired → apperr.ErrPasswordTokenExpired; got: %v", err)
	// E2E-004 reverse contract: raw ipatool sentinel must NO LONGER be visible
	// through errors.Is (otherwise the adapter is leaking implementation detail
	// and a future change might accidentally expose both sentinels).
	assert.False(t, errors.Is(err, ipaappstore.ErrPasswordTokenExpired),
		"adapter must not expose raw ipatool ErrPasswordTokenExpired after conversion; got: %v", err)
	assert.Equal(t, 1, inner.purchaseCalls, "inner.Purchase should be called exactly once")
}

// TestPurchase_NonSentinelError_Passthrough verifies E2E-005: when ipatool's
// Purchase returns a non-sentinel error (e.g. network failure, Apple 500),
// the adapter passes it through unchanged — no false conversion to apperr
// sentinel. This is AC-01-3's adapter-side contract (non-token errors keep
// their identity for the CLI's "license acquisition failed:" path).
func TestPurchase_NonSentinelError_Passthrough(t *testing.T) {
	originalErr := errors.New("apple 500 internal error")
	inner := &mockIPatoolAppStore{
		purchaseErr: originalErr,
	}
	adapter := &profileAppStoreAdapter{
		inner:   inner,
		account: &ipaappstore.Account{Email: "test@example.com"},
	}

	err := adapter.Purchase("com.test", 123, 0)

	require.Error(t, err, "Purchase with non-sentinel error must return error")
	assert.False(t, errors.Is(err, apperr.ErrPasswordTokenExpired),
		"non-sentinel errors must NOT be converted to apperr.ErrPasswordTokenExpired")
	// Identity preservation: mapAppStoreError returns unknown errors unchanged
	// (same pointer), not a copy. assert.Same verifies interface identity.
	assert.Same(t, originalErr, err,
		"non-sentinel error should pass through with identity preserved (same pointer)")
	assert.Equal(t, "apple 500 internal error", err.Error(),
		"error message should match original")
	assert.Equal(t, 1, inner.purchaseCalls, "inner.Purchase should be called exactly once")
}

// TestPurchase_NilAccount_ReturnsErrorWithoutCallingInner verifies a defensive
// boundary: if AccountInfo was never called (account == nil), Purchase must
// fail fast WITHOUT calling inner.Purchase (avoiding a nil-dereference panic
// inside ipatool when constructing PurchaseInput with a nil account deref).
func TestPurchase_NilAccount_ReturnsErrorWithoutCallingInner(t *testing.T) {
	inner := &mockIPatoolAppStore{}
	adapter := &profileAppStoreAdapter{
		inner:   inner,
		account: nil, // explicitly nil
	}

	err := adapter.Purchase("com.test", 123, 0)

	require.Error(t, err, "Purchase with nil account must return error")
	assert.Contains(t, err.Error(), "AccountInfo must be called",
		"error should explain the precondition")
	assert.Equal(t, 0, inner.purchaseCalls,
		"inner.Purchase must NOT be called when account is nil (defensive)")
}

// TestPurchase_Success_ReturnsNil verifies the happy path: when inner.Purchase
// returns nil, the adapter returns nil (no spurious conversion). This is a
// regression guard ensuring the fix doesn't accidentally convert success.
func TestPurchase_Success_ReturnsNil(t *testing.T) {
	inner := &mockIPatoolAppStore{
		purchaseErr: nil, // success
	}
	adapter := &profileAppStoreAdapter{
		inner:   inner,
		account: &ipaappstore.Account{Email: "test@example.com"},
	}

	err := adapter.Purchase("com.test", 123, 0)

	assert.NoError(t, err, "Purchase with nil error must return nil")
	assert.Equal(t, 1, inner.purchaseCalls)
}
