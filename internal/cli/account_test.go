package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/appstore"
)

// mockStore is a test double for account.Store with configurable state.
type mockStore struct {
	profiles    []account.Profile
	activeID    string
	credentials map[string]bool // profileID → has credentials
	loadErr     error
	listErr     error
	getErr      error // override Get error (nil = use profiles lookup)
	// Mutation tracking
	setActiveCalled string // captured by SetActive
	saved           bool   // set by Save
	upserted        *account.Profile
	removedID       string
}

func (m *mockStore) Load() error { return m.loadErr }
func (m *mockStore) Save() error {
	m.saved = true
	return nil
}
func (m *mockStore) List() ([]account.Profile, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]account.Profile, len(m.profiles))
	copy(result, m.profiles)
	return result, nil
}
func (m *mockStore) Get(id string) (account.Profile, error) {
	if m.getErr != nil {
		return account.Profile{}, m.getErr
	}
	for _, p := range m.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return account.Profile{}, apperr.ErrProfileNotFound
}
func (m *mockStore) GetActiveID() (string, error) { return m.activeID, nil }
func (m *mockStore) HasCredentials(id string) (bool, error) {
	return m.credentials[id], nil
}
func (m *mockStore) Upsert(p account.Profile) error {
	m.upserted = &p
	return nil
}
func (m *mockStore) Remove(id string) error {
	m.removedID = id
	// Actually remove from profiles slice (enables AC-04-6 testing).
	for i, p := range m.profiles {
		if p.ID == id {
			m.profiles = append(m.profiles[:i], m.profiles[i+1:]...)
			break
		}
	}
	// Simulate active-clearing invariant (matches real Store behavior, DD-04).
	if m.activeID == id {
		m.activeID = ""
	}
	// Remove credential tracking.
	delete(m.credentials, id)
	return nil
}
func (m *mockStore) SetActive(id string) error {
	m.setActiveCalled = id
	return nil
}
// --- E2E-031 / NFR-01: local commands < 500ms (mock-based, measures CLI overhead) ---

func TestLocalCommands_Performance_Under500ms(t *testing.T) {
	// Create a store with 10 profiles (5 logged-in, 5 logged-out).
	profiles := make([]account.Profile, 10)
	credentials := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("user%d_test", i)
		profiles[i] = account.Profile{ID: id, Email: fmt.Sprintf("user%d@test.com", i), Name: fmt.Sprintf("User%d", i)}
		credentials[id] = i < 5 // first 5 logged-in
	}
	store := &mockStore{
		profiles:    profiles,
		activeID:    "user0_test",
		credentials: credentials,
	}
	deps := Deps{
		Store: store,
		UI:    &mockPrompter{confirm: true},
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
			return &mockAppStore{}, nil
		},
		ConfigRoot: "/tmp/test",
	}

	// Test list performance.
	start := time.Now()
	cmd := accountsListCmd(deps)
	cmd.SetOut(&bytes.Buffer{})
	_ = cmd.RunE(cmd, nil)
	listElapsed := time.Since(start)
	assert.Less(t, listElapsed, 500*time.Millisecond, "accounts list should be < 500ms (NFR-01)")

	// Test use performance.
	start = time.Now()
	cmd = accountsUseCmd(deps)
	cmd.SetOut(&bytes.Buffer{})
	_ = cmd.RunE(cmd, []string{"user1_test"})
	useElapsed := time.Since(start)
	assert.Less(t, useElapsed, 500*time.Millisecond, "accounts use should be < 500ms (NFR-01)")

	// Test remove performance (mock Revoke is instant).
	start = time.Now()
	cmd = accountsRemoveCmd(deps)
	cmd.SetOut(&bytes.Buffer{})
	_ = cmd.RunE(cmd, []string{"user1_test"})
	removeElapsed := time.Since(start)
	assert.Less(t, removeElapsed, 500*time.Millisecond, "accounts remove should be < 500ms (NFR-01)")
}

func (m *mockStore) ClearActive() error { return nil }

// =============================================================================
// T6: accounts remove tests
// =============================================================================

// helperRunRemoveCmd creates an accountsRemoveCmd with the given Deps, captures
// output, runs the command with the given args, and returns output + error.
func helperRunRemoveCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := accountsRemoveCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

// helperRunRemoveCmdWithErr captures both stdout and stderr (for cascade error tests).
func helperRunRemoveCmdWithErr(t *testing.T, deps Deps, args ...string) (string, string, error) {
	t.Helper()
	cmd := accountsRemoveCmd(deps)
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	err := cmd.RunE(cmd, args)
	return outBuf.String(), errBuf.String(), err
}

func helperMakeRemoveDeps(store *mockStore, prompter *mockPrompter, mockAS *mockAppStore) Deps {
	return Deps{
		Store: store,
		UI:    prompter,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
			return mockAS, nil
		},
		ConfigRoot: "/tmp/test-config",
	}
}

// --- AC-04-1: remove existing with confirm ---

func TestAccountsRemove_ConfirmRemove_Success(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "bob_test", Email: "bob@test.com", Name: "Bob"}},
		credentials: map[string]bool{"bob_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	output, err := helperRunRemoveCmd(t, deps, "bob_test")
	require.NoError(t, err)
	assert.Contains(t, output, "removed")
	assert.Equal(t, "bob_test", store.removedID, "Remove should be called with bob_test")
	assert.True(t, mockAS.revokeCalled, "Revoke should be called")
	assert.True(t, store.saved, "Save should be called")
}

// --- AC-04-2: remove non-active doesn't affect active ---

func TestAccountsRemove_NonActive_KeepsActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"bob_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	_, err := helperRunRemoveCmd(t, deps, "bob_test")
	require.NoError(t, err)
	assert.Equal(t, "alice_test", store.activeID, "active should remain alice")
}

// --- AC-04-3: remove active clears active ---

func TestAccountsRemove_Active_ClearsActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	_, err := helperRunRemoveCmd(t, deps, "alice_test")
	require.NoError(t, err)
	assert.Equal(t, "", store.activeID, "active should be cleared after removing active profile")
}

// --- AC-04-4: reject confirm → no change ---

func TestAccountsRemove_RejectConfirm_NoChange(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		credentials: map[string]bool{"alice_test": true},
	}
	prompter := &mockPrompter{confirm: false} // user says "no"
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	output, err := helperRunRemoveCmd(t, deps, "alice_test")
	require.NoError(t, err, "rejection should exit 0")
	assert.Contains(t, output, "Cancelled")
	assert.Equal(t, "", store.removedID, "Remove should NOT be called")
	assert.False(t, mockAS.revokeCalled, "Revoke should NOT be called")
	assert.False(t, store.saved, "Save should NOT be called")
}

// --- AC-04-5 + AC-07-3: remove non-existent → fast fail, no confirm ---

func TestAccountsRemove_NonExistent_FastFail(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{confirm: true} // would confirm if asked
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	_, err := helperRunRemoveCmd(t, deps, "ghost_test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "accounts list")
	// AC-04-5: confirm NOT prompted
	assert.False(t, prompter.confirmCalled, "Confirm should NOT be called for non-existent profile")
	assert.False(t, mockAS.revokeCalled)
}

// --- AC-04-6: after remove, ID is truly gone from store ---

func TestAccountsRemove_AfterRemove_IDGone(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "bob_test", Email: "bob@test.com"}},
		credentials: map[string]bool{"bob_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	mockAS := &mockAppStore{}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	_, err := helperRunRemoveCmd(t, deps, "bob_test")
	require.NoError(t, err)

	// AC-04-6: profile is gone from List.
	profiles, _ := store.List()
	assert.Empty(t, profiles, "no profiles should remain after remove")

	// AC-04-6: Get returns not-found error.
	_, err = store.Get("bob_test")
	assert.Error(t, err, "Get should fail for removed profile")
}

// --- E2E-033 / NFR-04: cascade failure reporting (Revoke fails) ---

func TestAccountsRemove_RevokeFailure_ReportsError(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		credentials: map[string]bool{"alice_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	revokeErr := errors.New("keychain locked")
	mockAS := &mockAppStore{revokeErr: revokeErr}
	deps := helperMakeRemoveDeps(store, prompter, mockAS)

	_, stderr, err := helperRunRemoveCmdWithErr(t, deps, "alice_test")
	require.Error(t, err)
	assert.Contains(t, stderr, "error")
	assert.Contains(t, stderr, "keychain locked")
	// Metadata removal still happens (best-effort cascade)
	assert.Equal(t, "alice_test", store.removedID, "metadata removal should proceed despite Revoke failure")
}

// --- Factory failure reported ---

func TestAccountsRemove_FactoryFailure_ReportsError(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		credentials: map[string]bool{"alice_test": true},
	}
	prompter := &mockPrompter{confirm: true}
	factoryErr := errors.New("keyring unavailable")
	deps := Deps{
		Store: store,
		UI:    prompter,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
			return nil, factoryErr
		},
		ConfigRoot: "/tmp/test",
	}

	_, stderr, err := helperRunRemoveCmdWithErr(t, deps, "alice_test")
	require.Error(t, err)
	assert.Contains(t, stderr, "keyring", "factory error should appear in stderr")
}

// helperRunListCmd creates an accountsListCmd with the given mock Store,
// captures stdout, runs the command, and returns the output + error.
func helperRunListCmd(t *testing.T, store account.Store) (string, error) {
	t.Helper()
	deps := Deps{Store: store}
	cmd := accountsListCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.RunE(cmd, []string{})
	return buf.String(), err
}

// --- E2E-009: empty list (AC-03-1) ---

func TestAccountsList_Empty_PrintsNoProfiles(t *testing.T) {
	store := &mockStore{
		credentials: map[string]bool{},
	}
	output, err := helperRunListCmd(t, store)

	require.NoError(t, err, "exit 0 for empty list")
	assert.Contains(t, output, "No profiles")
}

// --- E2E-010: multiple profiles with different statuses (AC-03-2) ---

func TestAccountsList_MultipleProfiles_StatusCorrect(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_example_com", Email: "alice@example.com", Name: "Alice"},
			{ID: "bob_example_com", Email: "bob@example.com", Name: "Bob"},
			{ID: "charlie_example_com", Email: "charlie@example.com", Name: "Charlie"},
		},
		activeID: "alice_example_com",
		credentials: map[string]bool{
			"alice_example_com":   true,
			"bob_example_com":     true,
			"charlie_example_com": false,
		},
	}
	output, err := helperRunListCmd(t, store)

	require.NoError(t, err)
	// All three profiles present.
	assert.Contains(t, output, "alice_example_com")
	assert.Contains(t, output, "bob_example_com")
	assert.Contains(t, output, "charlie_example_com")
	// Active marker on Alice.
	assert.Contains(t, output, "*")
	// Logged-in status for Alice and Bob.
	assert.Contains(t, output, "logged-in")
	// Logged-out status for Charlie.
	assert.Contains(t, output, "logged-out")
}

// --- E2E-011: ID and email both visible (AC-03-3) ---

func TestAccountsList_ShowsIDAndEmail(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_example_com", Email: "alice@example.com", Name: "Alice"},
		},
		activeID: "alice_example_com",
		credentials: map[string]bool{
			"alice_example_com": true,
		},
	}
	output, err := helperRunListCmd(t, store)

	require.NoError(t, err)
	assert.Contains(t, output, "alice_example_com", "ID should be visible")
	assert.Contains(t, output, "alice@example.com", "email should be visible")
}

// --- Active marker is NOT on non-active profiles ---

func TestAccountsList_ActiveMarker_OnlyOnActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
			{ID: "bob_test", Email: "bob@test.com", Name: "Bob"},
		},
		activeID: "alice_test",
		credentials: map[string]bool{
			"alice_test": true,
			"bob_test":   true,
		},
	}
	output, err := helperRunListCmd(t, store)

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Find rows with alice and bob, verify only alice has *
	for _, line := range lines {
		if strings.Contains(line, "alice_test") {
			assert.Contains(t, line, "*", "alice (active) should have * marker")
		}
		if strings.Contains(line, "bob_test") {
			assert.NotContains(t, line, "*", "bob (non-active) should NOT have * marker")
		}
	}
}

// --- Load error propagates ---

func TestAccountsList_LoadError_ReturnsError(t *testing.T) {
	store := &mockStore{
		loadErr:     assertError("load failed"),
		credentials: map[string]bool{},
	}
	_, err := helperRunListCmd(t, store)
	assert.Error(t, err)
}

// assertError is a helper to create a simple error for mock configuration.
func assertError(msg string) error {
	return &simpleError{msg: msg}
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

// =============================================================================
// T3: accounts use tests
// =============================================================================

// helperRunUseCmd creates an accountsUseCmd with the given Deps, captures
// output, runs the command with the given args, and returns output + error.
func helperRunUseCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := accountsUseCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

// --- E2E-005: switch to logged-in profile (AC-02-1) ---

func TestAccountsUse_LoggedInProfile_SwitchesActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
			{ID: "bob_test", Email: "bob@test.com", Name: "Bob"},
		},
		activeID: "alice_test",
		credentials: map[string]bool{
			"alice_test": true,
			"bob_test":   true,
		},
	}
	deps := Deps{Store: store}
	output, err := helperRunUseCmd(t, deps, "bob_test")

	require.NoError(t, err)
	assert.Contains(t, output, "Active profile: bob_test")
	assert.Equal(t, "bob_test", store.setActiveCalled, "SetActive should be called with bob_test")
	assert.True(t, store.saved, "Save should be called")
}

// --- E2E-006: switch to non-existent profile (AC-02-2, AC-07-3) ---

func TestAccountsUse_NonExistent_ErrorWithHint(t *testing.T) {
	store := &mockStore{
		credentials: map[string]bool{},
	}
	deps := Deps{Store: store}
	_, err := helperRunUseCmd(t, deps, "ghost_test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "accounts list")
	// Active should NOT be changed
	assert.Equal(t, "", store.setActiveCalled)
	assert.False(t, store.saved)
}

// --- E2E-007: switch to logged-out profile (AC-02-3, AC-07-3) ---

func TestAccountsUse_LoggedOut_ErrorWithHint(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
		},
		activeID: "bob_test",
		credentials: map[string]bool{
			"alice_test": false, // logged out
		},
	}
	deps := Deps{Store: store}
	_, err := helperRunUseCmd(t, deps, "alice_test")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
	assert.Contains(t, err.Error(), "auth login")
	// Active should NOT be changed
	assert.Equal(t, "", store.setActiveCalled)
	assert.False(t, store.saved)
}

// --- E2E-008: use does NOT construct AppStore (AC-02-4, NFR-01) ---

func TestAccountsUse_DoesNotConstructAppStore(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
		},
		activeID: "",
		credentials: map[string]bool{
			"alice_test": true,
		},
	}
	deps := Deps{
		Store: store,
		// AppStoreFactory intentionally nil — if use calls it, Go panics
		// with nil pointer dereference, failing the test.
	}
	_, err := helperRunUseCmd(t, deps, "alice_test")

	// If we get here without panic, the factory was never called.
	require.NoError(t, err)
	assert.Equal(t, "alice_test", store.setActiveCalled)
}
