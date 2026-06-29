package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// =============================================================================
// Test doubles
// =============================================================================

// mockPrompter implements ui.Prompter with configurable return values.
type mockPrompter struct {
	email       string
	password    string
	authCode    string
	confirm     bool
	emailErr    error
	passwordErr error
	authCodeErr error
}

func (m *mockPrompter) InputEmail() (string, error)                              { return m.email, m.emailErr }
func (m *mockPrompter) InputPassword() (string, error)                           { return m.password, m.passwordErr }
func (m *mockPrompter) InputAuthCode() (string, error)                           { return m.authCode, m.authCodeErr }
func (m *mockPrompter) Confirm(string) (bool, error)                             { return m.confirm, nil }
func (m *mockPrompter) SelectProfile([]account.Profile, string) (string, error) { return "", nil }

// mockAppStore implements ipaappstore.AppStore by embedding the interface
// (nil) and overriding only Bag, Login, and Revoke. Other methods panic if called.
type mockAppStore struct {
	ipaappstore.AppStore // nil embed; non-overridden methods panic if called
	bagOutput    ipaappstore.BagOutput
	bagErr       error
	loginOutputs []ipaappstore.LoginOutput
	loginErrors  []error
	loginCalls   int
	revokeErr    error
	revokeCalled bool
}

func (m *mockAppStore) Bag(ipaappstore.BagInput) (ipaappstore.BagOutput, error) {
	return m.bagOutput, m.bagErr
}

func (m *mockAppStore) Login(ipaappstore.LoginInput) (ipaappstore.LoginOutput, error) {
	idx := m.loginCalls
	m.loginCalls++
	var output ipaappstore.LoginOutput
	var err error
	if idx < len(m.loginOutputs) {
		output = m.loginOutputs[idx]
	}
	if idx < len(m.loginErrors) {
		err = m.loginErrors[idx]
	}
	return output, err
}

func (m *mockAppStore) Revoke() error {
	m.revokeCalled = true
	return m.revokeErr
}

// helperRunLoginCmd creates an authLoginCmd with the given Deps, captures
// stdout, runs the command, and returns the output + error.
func helperRunLoginCmd(t *testing.T, deps Deps) (string, error) {
	t.Helper()
	cmd := authLoginCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.RunE(cmd, []string{})
	return buf.String(), err
}

// helperMakeLoginDeps constructs Deps with mock Store, mock UI, and mock factory.
func helperMakeLoginDeps(store account.Store, prompter ui.Prompter, mockAS *mockAppStore) Deps {
	return Deps{
		Store: store,
		UI:    prompter,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) {
			return mockAS, nil
		},
	}
}

// =============================================================================
// E2E-001 / AC-01-1 + AC-01-3 + AC-06-1: New profile + 2FA + auto-active
// =============================================================================

func TestAuthLogin_NewProfile_2FA_AutoActive(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "alice@example.com",
		password: "correct-password",
		authCode: "123456",
	}
	mockAS := &mockAppStore{
		bagOutput: ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginOutputs: []ipaappstore.LoginOutput{
			{}, // first call: ErrAuthCodeRequired (output discarded)
			{Account: ipaappstore.Account{Name: "Alice", Email: "alice@example.com", StoreFront: "143441"}}, // second call: success
		},
		loginErrors: []error{ipaappstore.ErrAuthCodeRequired, nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// AC-01-1: profile created
	assert.Contains(t, output, "Logged in")
	assert.Contains(t, output, "Alice")
	assert.NotNil(t, store.upserted)
	assert.Equal(t, "alice_example_com", store.upserted.ID)

	// AC-01-3: first profile auto-active
	assert.Equal(t, "alice_example_com", store.setActiveCalled)

	// AC-06-1: 2FA prompt shown
	assert.Contains(t, output, "2FA")

	// Saved
	assert.True(t, store.saved)

	// NFR-09: no password in output
	assert.NotContains(t, output, "correct-password")

	// Two login calls (first with empty AuthCode, second with code)
	assert.Equal(t, 2, mockAS.loginCalls)
}

// =============================================================================
// E2E-027 / AC-06-3: No 2FA needed (direct success)
// =============================================================================

func TestAuthLogin_No2FA_DirectSuccess(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "alice@example.com",
		password: "correct",
		authCode: "", // should not be used
	}
	mockAS := &mockAppStore{
		bagOutput:    ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginOutputs: []ipaappstore.LoginOutput{{Account: ipaappstore.Account{Name: "Alice", Email: "alice@example.com"}}},
		loginErrors:  []error{nil}, // first call succeeds directly
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// AC-06-3: no 2FA prompt
	assert.NotContains(t, output, "2FA")
	assert.Contains(t, output, "Logged in")

	// Only one login call (no retry)
	assert.Equal(t, 1, mockAS.loginCalls)
}

// =============================================================================
// E2E-003 / AC-01-4: Second profile doesn't replace active
// =============================================================================

func TestAuthLogin_SecondProfile_DoesNotReplaceActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_example_com", Email: "alice@example.com", Name: "Alice"},
		},
		activeID:    "alice_example_com",
		credentials: map[string]bool{"alice_example_com": true},
	}
	prompter := &mockPrompter{
		email:    "bob@example.com",
		password: "bob-pass",
	}
	mockAS := &mockAppStore{
		bagOutput:    ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginOutputs: []ipaappstore.LoginOutput{{Account: ipaappstore.Account{Name: "Bob", Email: "bob@example.com"}}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// AC-01-4: active unchanged (still alice, not bob)
	assert.Equal(t, "", store.setActiveCalled, "SetActive should NOT be called for second profile")
}

// =============================================================================
// E2E-004: Refresh existing profile
// =============================================================================

func TestAuthLogin_RefreshExisting_UpdatesInPlace(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_example_com", Email: "alice@example.com", Name: "Old Name"},
		},
		activeID:    "alice_example_com",
		credentials: map[string]bool{"alice_example_com": true},
	}
	prompter := &mockPrompter{
		email:    "alice@example.com", // same email → same derived ID → refresh
		password: "new-pass",
	}
	mockAS := &mockAppStore{
		bagOutput:    ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginOutputs: []ipaappstore.LoginOutput{{Account: ipaappstore.Account{Name: "New Name", Email: "alice@example.com"}}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// Upsert called with updated name
	require.NotNil(t, store.upserted)
	assert.Equal(t, "alice_example_com", store.upserted.ID)
	assert.Equal(t, "New Name", store.upserted.Name)

	// Active unchanged (was already set)
	assert.Equal(t, "", store.setActiveCalled, "SetActive should NOT be called on refresh")
}

// =============================================================================
// E2E-026 / AC-06-2: Wrong 2FA code
// =============================================================================

func TestAuthLogin_Wrong2FA_FailsWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "alice@example.com",
		password: "correct",
		authCode: "000000", // wrong code
	}
	appleErr := errors.New("invalid verification code")
	mockAS := &mockAppStore{
		bagOutput:   ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginErrors: []error{ipaappstore.ErrAuthCodeRequired, appleErr},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)

	// AC-06-2: error contains Apple message + hint
	assert.Contains(t, err.Error(), "invalid verification code")
	assert.Contains(t, err.Error(), "2FA")

	// No profile created
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
}

// =============================================================================
// E2E-028 / AC-07-1: Wrong password
// =============================================================================

func TestAuthLogin_WrongPassword_FailsWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "alice@example.com",
		password: "wrong-password",
	}
	appleErr := errors.New("invalid credentials")
	mockAS := &mockAppStore{
		bagOutput:   ipaappstore.BagOutput{AuthEndpoint: "https://auth.example.com"},
		loginErrors: []error{appleErr}, // non-ErrAuthCodeRequired → direct failure
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)

	// AC-07-1: error contains Apple message + hint
	assert.Contains(t, err.Error(), "invalid credentials")
	assert.Contains(t, err.Error(), "verify your credentials")

	// No profile created
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
}

// =============================================================================
// E2E-029 / AC-07-2: Ctrl-C at email prompt
// =============================================================================

func TestAuthLogin_CtrlC_NoSideEffects(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		emailErr: errors.New("interrupt"), // simulate Ctrl-C
	}
	mockAS := &mockAppStore{}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)

	// AC-07-2: no side effects
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
	assert.Equal(t, 0, mockAS.loginCalls, "Login should not be called")
}

// =============================================================================
// E2E-002 / AC-01-2: Derived ID correct
// =============================================================================

func TestAuthLogin_DerivedID_Correct(t *testing.T) {
	testCases := []struct {
		email string
		want  string
	}{
		{"alice@example.com", "alice_example_com"},
		{"Bob@Example.Com", "bob_example_com"},
		{"user+tag@domain.org", "user_tag_domain_org"},
	}
	for _, tc := range testCases {
		t.Run(tc.email, func(t *testing.T) {
			store := &mockStore{credentials: map[string]bool{}}
			prompter := &mockPrompter{email: tc.email, password: "x"}
			mockAS := &mockAppStore{
				bagOutput:    ipaappstore.BagOutput{AuthEndpoint: "https://e.test"},
				loginOutputs: []ipaappstore.LoginOutput{{Account: ipaappstore.Account{Name: "X", Email: tc.email}}},
				loginErrors:  []error{nil},
			}
			deps := helperMakeLoginDeps(store, prompter, mockAS)

			_, err := helperRunLoginCmd(t, deps)
			require.NoError(t, err)
			require.NotNil(t, store.upserted)
			assert.Equal(t, tc.want, store.upserted.ID)
		})
	}
}

// =============================================================================
// NFR-02: CLI overhead after Login return < 1s
// =============================================================================

func TestAuthLogin_CLIOverhead_UnderOneSecond(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{email: "x@y.com", password: "p"}
	mockAS := &mockAppStore{
		bagOutput:    ipaappstore.BagOutput{AuthEndpoint: "https://e.test"},
		loginOutputs: []ipaappstore.LoginOutput{{Account: ipaappstore.Account{Name: "X", Email: "x@y.com"}}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	start := time.Now()
	_, err := helperRunLoginCmd(t, deps)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, time.Second, "CLI overhead (with mock AppStore) should be < 1s (NFR-02)")
}

// =============================================================================
// NFR-09: Progress output contains key stages, no password/token
// =============================================================================

func TestAuthLogin_ProgressOutput_NoSecrets(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{email: "alice@example.com", password: "secret-pass-123", authCode: "654321"}
	mockAS := &mockAppStore{
		bagOutput: ipaappstore.BagOutput{AuthEndpoint: "https://e.test"},
		loginOutputs: []ipaappstore.LoginOutput{
			{}, // first call: ErrAuthCodeRequired (output discarded)
			{Account: ipaappstore.Account{Name: "Alice", Email: "a@b.com", PasswordToken: "tok-abc"}}, // second: success with token
		},
		loginErrors: []error{ipaappstore.ErrAuthCodeRequired, nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// Contains progress messages
	assert.True(t, strings.Contains(output, "Contacting") || strings.Contains(output, "Apple"),
		"should contain progress message, got: %s", output)

	// NFR-05/09: no password or token in output
	assert.NotContains(t, output, "secret-pass-123", "password must not appear in output")
	assert.NotContains(t, output, "tok-abc", "token must not appear in output")
	assert.NotContains(t, output, "654321", "auth code must not appear in output after use")
}

// =============================================================================
// AC-07-2 (extended): Ctrl-C at password and auth-code prompts
// =============================================================================

func TestAuthLogin_CtrlC_AtPassword_NoSideEffects(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:       "alice@example.com",
		passwordErr: errors.New("interrupt"),
	}
	mockAS := &mockAppStore{}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
	assert.Equal(t, 0, mockAS.loginCalls)
}

func TestAuthLogin_CtrlC_AtAuthCode_NoSideEffects(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:       "alice@example.com",
		password:    "correct",
		authCodeErr: errors.New("interrupt"), // Ctrl-C at 2FA prompt
	}
	mockAS := &mockAppStore{
		bagOutput:   ipaappstore.BagOutput{AuthEndpoint: "https://e.test"},
		loginErrors: []error{ipaappstore.ErrAuthCodeRequired}, // first call triggers 2FA
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
	assert.Equal(t, 1, mockAS.loginCalls, "first Login called, then Ctrl-C at 2FA prompt")
}

// =============================================================================
// T5: auth logout tests
// =============================================================================

func helperRunLogoutCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := authLogoutCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	err := cmd.RunE(cmd, args)
	return buf.String(), err
}

// --- AC-05-1: logout defaults to active ---

func TestAuthLogout_DefaultActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
		},
		activeID: "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test-config",
	}

	output, err := helperRunLogoutCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "Logged out")
	assert.Contains(t, output, "alice_test")
	assert.True(t, mockAS.revokeCalled, "Revoke should be called")
}

// --- AC-05-2: logout explicit profile ---

func TestAuthLogout_ExplicitProfile(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID: "alice_test",
		credentials: map[string]bool{"alice_test": true, "bob_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test",
	}

	output, err := helperRunLogoutCmd(t, deps, "bob_test")
	require.NoError(t, err)
	assert.Contains(t, output, "bob_test")
	assert.True(t, mockAS.revokeCalled)
}

// --- AC-05-3: logout non-existent ---

func TestAuthLogout_NonExistent_ErrorWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	deps := Deps{Store: store, ConfigRoot: "/tmp/test"}

	_, err := helperRunLogoutCmd(t, deps, "ghost_test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "accounts list")
}

// --- AC-05-4: logout no active ---

func TestAuthLogout_NoActive_ErrorWithHint(t *testing.T) {
	store := &mockStore{
		activeID:    "",
		credentials: map[string]bool{},
	}
	deps := Deps{Store: store, ConfigRoot: "/tmp/test"}

	_, err := helperRunLogoutCmd(t, deps) // no args, no active
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
	assert.Contains(t, err.Error(), "accounts use")
}

// --- AC-05-5: logout already logged-out (idempotent) ---

func TestAuthLogout_AlreadyLoggedOut_Idempotent(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		credentials: map[string]bool{"alice_test": false}, // logged out
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test",
	}

	output, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.NoError(t, err, "idempotent logout should exit 0")
	assert.Contains(t, output, "already logged out")
	assert.False(t, mockAS.revokeCalled, "Revoke should NOT be called for already-logged-out")
}

// --- AC-05-6: logout preserves metadata (implicit — we don't remove profile) ---

func TestAuthLogout_PreservesMetadata(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
		},
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test",
	}

	_, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.NoError(t, err)
	// Profile NOT removed from store (metadata retained)
	assert.Equal(t, "", store.removedID, "Remove should NOT be called")
	// Profile still in list
	profiles, _ := store.List()
	assert.Len(t, profiles, 1)
}

// --- AC-05-7: active→logged-out contract (double logout is idempotent) ---

func TestAuthLogout_DoubleLogout_Idempotent(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		activeID: "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test",
	}

	// First logout — succeeds, Revoke called
	_, err := helperRunLogoutCmd(t, deps)
	require.NoError(t, err)
	assert.True(t, mockAS.revokeCalled)

	// Simulate keychain now has no credentials (Revoke removed them)
	store.credentials["alice_test"] = false

	// Second logout — idempotent (AC-05-5 behavior)
	_, err = helperRunLogoutCmd(t, deps)
	require.NoError(t, err, "second logout should be idempotent exit 0")
}

// --- Revoke failure reports error (NFR-04 spirit) ---

func TestAuthLogout_RevokeFailure_ReportsError(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		credentials: map[string]bool{"alice_test": true},
	}
	revokeErr := errors.New("keychain locked")
	mockAS := &mockAppStore{revokeErr: revokeErr}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (ipaappstore.AppStore, error) { return mockAS, nil },
		ConfigRoot: "/tmp/test",
	}

	_, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error")
}
