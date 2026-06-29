package appstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/keyring"
	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
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
