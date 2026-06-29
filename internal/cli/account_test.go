package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// mockStore is a test double for account.Store with configurable state.
type mockStore struct {
	profiles    []account.Profile
	activeID    string
	credentials map[string]bool // profileID → has credentials
	loadErr     error
	listErr     error
}

func (m *mockStore) Load() error                  { return m.loadErr }
func (m *mockStore) Save() error                  { return nil }
func (m *mockStore) List() ([]account.Profile, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]account.Profile, len(m.profiles))
	copy(result, m.profiles)
	return result, nil
}
func (m *mockStore) Get(id string) (account.Profile, error) {
	for _, p := range m.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return account.Profile{}, apperr.ErrProfileNotFound
}
func (m *mockStore) GetActiveID() (string, error)  { return m.activeID, nil }
func (m *mockStore) HasCredentials(id string) (bool, error) {
	return m.credentials[id], nil
}
func (m *mockStore) Upsert(p account.Profile) error  { return nil }
func (m *mockStore) Remove(id string) error           { return nil }
func (m *mockStore) SetActive(id string) error        { return nil }
func (m *mockStore) ClearActive() error               { return nil }

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
