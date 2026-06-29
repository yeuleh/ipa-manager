package account

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ipakeychain "github.com/majd/ipatool/v2/pkg/keychain"
)

// mockKeychain is an in-memory implementation of ipakeychain.Keychain for testing.
type mockKeychain struct {
	data map[string][]byte
}

func newMockKeychain() *mockKeychain {
	return &mockKeychain{data: make(map[string][]byte)}
}

func (m *mockKeychain) Get(key string) ([]byte, error) {
	val, ok := m.data[key]
	if !ok {
		return nil, &mockKeychainError{msg: "key not found"}
	}
	return val, nil
}

func (m *mockKeychain) Set(key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *mockKeychain) Remove(key string) error {
	delete(m.data, key)
	return nil
}

type mockKeychainError struct{ msg string }

func (e *mockKeychainError) Error() string { return e.msg }

// Compile-time assertion that mockKeychain satisfies ipakeychain.Keychain.
var _ ipakeychain.Keychain = (*mockKeychain)(nil)

// helperNewStore creates a Store backed by a temp dir + mock keychain.
func helperNewStore(t *testing.T) (Store, string, *mockKeychain) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	kc := newMockKeychain()
	s := NewStore(configPath, kc)
	return s, configPath, kc
}

// --- Load tests ---

func TestStore_Load_AbsentFile_EmptyState(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())

	profiles, err := s.List()
	require.NoError(t, err)
	assert.Empty(t, profiles)

	active, err := s.GetActiveID()
	require.NoError(t, err)
	assert.Equal(t, "", active)
}

func TestStore_Load_ExistingFile_PopulatedState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	existing := profileFile{
		ActiveProfileID: "alice_example_com",
		Profiles: []Profile{
			{ID: "alice_example_com", Email: "alice@example.com", Name: "Alice"},
		},
	}
	data, _ := json.Marshal(existing)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	s := NewStore(configPath, newMockKeychain())
	require.NoError(t, s.Load())

	active, _ := s.GetActiveID()
	assert.Equal(t, "alice_example_com", active)

	profiles, _ := s.List()
	require.Len(t, profiles, 1)
	assert.Equal(t, "alice@example.com", profiles[0].Email)
}

func TestStore_Load_CorruptedFile_Error(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte("{not json"), 0o600))

	s := NewStore(configPath, newMockKeychain())
	err := s.Load()
	assert.Error(t, err)
}

// --- Save tests ---

func TestStore_Save_CreatesFile(t *testing.T) {
	s, configPath, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "alice_example_com", Email: "alice@example.com"}))
	require.NoError(t, s.SetActive("alice_example_com"))
	require.NoError(t, s.Save())

	// File exists and contains correct JSON.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var pf profileFile
	require.NoError(t, json.Unmarshal(data, &pf))
	assert.Equal(t, "alice_example_com", pf.ActiveProfileID)
	require.Len(t, pf.Profiles, 1)
	assert.Equal(t, "alice@example.com", pf.Profiles[0].Email)
}

func TestStore_Save_Atomic_NoTmpLeftBehind(t *testing.T) {
	s, configPath, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "x_test"}))
	require.NoError(t, s.Save())

	// Temp file should not exist after successful Save.
	_, err := os.Stat(configPath + ".tmp")
	assert.True(t, os.IsNotExist(err), "tmp file should not remain after Save")
}

func TestStore_Save_Load_RoundTrip(t *testing.T) {
	s, configPath, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "alice_example_com", Name: "Alice", Email: "alice@example.com", StoreFront: "143441"}))
	require.NoError(t, s.Upsert(Profile{ID: "bob_example_com", Name: "Bob", Email: "bob@example.com"}))
	require.NoError(t, s.SetActive("alice_example_com"))
	require.NoError(t, s.Save())

	// Load into a fresh Store.
	s2 := NewStore(configPath, newMockKeychain())
	require.NoError(t, s2.Load())

	active, _ := s2.GetActiveID()
	assert.Equal(t, "alice_example_com", active)

	profiles, _ := s2.List()
	require.Len(t, profiles, 2)
	assert.Equal(t, "143441", profiles[0].StoreFront)
}

// --- CRUD tests ---

func TestStore_Upsert_New(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "x_test", Name: "X"}))
	require.NoError(t, s.Upsert(Profile{ID: "y_test", Name: "Y"}))

	profiles, _ := s.List()
	assert.Len(t, profiles, 2)
}

func TestStore_Upsert_Existing_UpdatesInPlace(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "x_test", Name: "Old"}))
	require.NoError(t, s.Upsert(Profile{ID: "x_test", Name: "New"}))

	profiles, _ := s.List()
	require.Len(t, profiles, 1)
	assert.Equal(t, "New", profiles[0].Name)
}

func TestStore_Remove_Existing(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "x_test"}))
	require.NoError(t, s.Upsert(Profile{ID: "y_test"}))
	require.NoError(t, s.Remove("x_test"))

	profiles, _ := s.List()
	require.Len(t, profiles, 1)
	assert.Equal(t, "y_test", profiles[0].ID)
}

func TestStore_Remove_NonExistent_Error(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	err := s.Remove("ghost")
	assert.Error(t, err)
}

func TestStore_Remove_Active_ClearsActive(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "alice_test"}))
	require.NoError(t, s.SetActive("alice_test"))
	require.NoError(t, s.Remove("alice_test"))

	active, _ := s.GetActiveID()
	assert.Equal(t, "", active, "active should be cleared when active profile is removed")
}

func TestStore_Remove_NonActive_DoesNotChangeActive(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "alice_test"}))
	require.NoError(t, s.Upsert(Profile{ID: "bob_test"}))
	require.NoError(t, s.SetActive("alice_test"))
	require.NoError(t, s.Remove("bob_test"))

	active, _ := s.GetActiveID()
	assert.Equal(t, "alice_test", active)
}

// --- Get/SetActive tests ---

func TestStore_Get_Found(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.Upsert(Profile{ID: "x_test", Name: "X"}))

	p, err := s.Get("x_test")
	require.NoError(t, err)
	assert.Equal(t, "X", p.Name)
}

func TestStore_Get_NotFound(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	_, err := s.Get("ghost")
	assert.Error(t, err)
}

func TestStore_SetActive_Empty_Allowed(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.SetActive("x"))
	require.NoError(t, s.SetActive(""))

	active, _ := s.GetActiveID()
	assert.Equal(t, "", active)
}

func TestStore_ClearActive(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())
	require.NoError(t, s.SetActive("x"))
	require.NoError(t, s.ClearActive())

	active, _ := s.GetActiveID()
	assert.Equal(t, "", active)
}

// --- HasCredentials tests ---

func TestStore_HasCredentials_WithEntry_True(t *testing.T) {
	s, _, kc := helperNewStore(t)
	require.NoError(t, s.Load())

	// Simulate keychain entry for profile "alice_test".
	// ProfileKeychain maps "account" → "profiles/alice_test/account".
	kc.data["profiles/alice_test/account"] = []byte(`{"email":"alice@test.com"}`)

	has, err := s.HasCredentials("alice_test")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestStore_HasCredentials_WithoutEntry_False(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())

	has, err := s.HasCredentials("nobody_test")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestStore_HasCredentials_OtherProfileEntry_False(t *testing.T) {
	s, _, kc := helperNewStore(t)
	require.NoError(t, s.Load())

	// Alice has entry, Bob does not.
	kc.data["profiles/alice_test/account"] = []byte(`{}`)

	hasAlice, _ := s.HasCredentials("alice_test")
	assert.True(t, hasAlice)

	hasBob, _ := s.HasCredentials("bob_test")
	assert.False(t, hasBob)
}

func TestStore_HasCredentials_EmptyData_False(t *testing.T) {
	s, _, kc := helperNewStore(t)
	require.NoError(t, s.Load())

	// Empty byte slice in keychain → should be treated as no credentials (Spok finding 1).
	kc.data["profiles/alice_test/account"] = []byte{}

	has, _ := s.HasCredentials("alice_test")
	assert.False(t, has, "empty keychain data should be treated as not logged-in")
}

func TestStore_Save_WithoutLoad_Error(t *testing.T) {
	s, configPath, _ := helperNewStore(t)
	// Pre-create config.json with data.
	require.NoError(t, os.WriteFile(configPath, []byte(`{"active_profile_id":"x","profiles":[{"id":"x"}]}`), 0o600))

	// Don't call Load — Save should refuse (Spok finding 3).
	err := s.Save()
	assert.Error(t, err, "Save without Load should error to prevent overwrite")

	// Original file should be untouched.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"active_profile_id":"x"`)
}

// --- State machine integration test (E2E-036) ---

func TestStore_StateMachine_FullLifecycle(t *testing.T) {
	s, _, _ := helperNewStore(t)
	require.NoError(t, s.Load())

	p1 := Profile{ID: "alice_test", Name: "Alice"}
	p2 := Profile{ID: "bob_test", Name: "Bob"}

	// Upsert both.
	require.NoError(t, s.Upsert(p1))
	require.NoError(t, s.Upsert(p2))

	// Set active to p1.
	require.NoError(t, s.SetActive(p1.ID))
	active, _ := s.GetActiveID()
	assert.Equal(t, p1.ID, active)

	// Remove p1 (active) → active should clear.
	require.NoError(t, s.Remove(p1.ID))
	active, _ = s.GetActiveID()
	assert.Equal(t, "", active)

	// p1 no longer exists.
	_, err := s.Get(p1.ID)
	assert.Error(t, err)

	// Only p2 remains.
	profiles, _ := s.List()
	require.Len(t, profiles, 1)
	assert.Equal(t, p2.ID, profiles[0].ID)
}
