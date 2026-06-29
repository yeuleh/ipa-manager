package appstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/99designs/keyring"
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
// config (Spok finding 4). Uses keyringOpener injection to avoid real Keychain.
func TestNewProfileAppStore_WiringWithMockKeyring(t *testing.T) {
	// Pre-populate mock keyring with account data at the expected namespace key.
	// ProfileKeychain maps "account" → "profiles/<id>/account".
	mockKeyring := keyring.NewArrayKeyring([]keyring.Item{
		{
			Key:  "profiles/test_profile/account",
			Data: []byte(`{"email":"test@example.com","name":"Test User","directoryServicesIdentifier":"123","passwordToken":"tok"}`),
		},
	})

	// Capture config for assertions + inject mock keyring.
	var capturedConfig keyring.Config
	origOpener := keyringOpener
	defer func() { keyringOpener = origOpener }()
	keyringOpener = func(c keyring.Config) (keyring.Keyring, error) {
		capturedConfig = c
		return mockKeyring, nil
	}

	// Construct AppStore with test profile.
	dir := t.TempDir()
	appStore, err := NewProfileAppStore(
		account.Profile{ID: "test_profile", Email: "test@example.com"},
		dir,
	)
	require.NoError(t, err)
	require.NotNil(t, appStore)

	// Verify keyring config (DD-02).
	assert.Equal(t, "ipa-manager", capturedConfig.ServiceName)
	assert.Equal(t, []keyring.BackendType{keyring.KeychainBackend}, capturedConfig.AllowedBackends)
	assert.Equal(t, filepath.Join(dir, "keychain"), capturedConfig.FileDir)

	// Verify ProfileKeychain namespace: AccountInfo reads "account" which
	// ProfileKeychain maps to "profiles/test_profile/account" in the mock keyring.
	// If wiring is wrong (e.g., no ProfileKeychain), this would fail.
	output, err := appStore.AccountInfo()
	require.NoError(t, err, "AccountInfo should succeed via ProfileKeychain namespace")
	assert.Equal(t, "test@example.com", output.Account.Email)
	assert.Equal(t, "Test User", output.Account.Name)

	// Verify cookie jar directory was created (the file itself is created
	// on Save(), not on construction).
	cookieDir := filepath.Join(dir, "profiles", "test_profile")
	info, err := os.Stat(cookieDir)
	require.NoError(t, err, "cookie jar directory should exist at %s", cookieDir)
	assert.True(t, info.IsDir())
}

// TestNewProfileAppStore_NamespaceIsolation_VerifyDifferentProfiles tests
// that two AppStores constructed with different ProfileIDs access different
// keychain entries (core isolation guarantee, ADR 0002).
func TestNewProfileAppStore_NamespaceIsolation_VerifyDifferentProfiles(t *testing.T) {
	// Mock keyring with entries for two different profiles.
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

	// Construct AppStore for Alice.
	aliceStore, err := NewProfileAppStore(account.Profile{ID: "alice_test"}, dir)
	require.NoError(t, err)

	// Construct AppStore for Bob.
	bobStore, err := NewProfileAppStore(account.Profile{ID: "bob_test"}, dir)
	require.NoError(t, err)

	// Alice sees Alice's data, NOT Bob's.
	aliceInfo, err := aliceStore.AccountInfo()
	require.NoError(t, err)
	assert.Equal(t, "Alice", aliceInfo.Account.Name)

	// Bob sees Bob's data, NOT Alice's.
	bobInfo, err := bobStore.AccountInfo()
	require.NoError(t, err)
	assert.Equal(t, "Bob", bobInfo.Account.Name)
}
