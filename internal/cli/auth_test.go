package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// =============================================================================
// Test doubles
// =============================================================================

// mockPrompter implements ui.Prompter with configurable return values.
type mockPrompter struct {
	email         string
	password      string
	authCode      string
	confirm       bool
	emailErr      error
	passwordErr   error
	authCodeErr   error
	confirmCalled bool
}

func (m *mockPrompter) InputEmail() (string, error) { return m.email, m.emailErr }
func (m *mockPrompter) InputPassword() (string, error) { return m.password, m.passwordErr }
func (m *mockPrompter) InputAuthCode() (string, error) { return m.authCode, m.authCodeErr }
func (m *mockPrompter) Confirm(string) (bool, error) {
	m.confirmCalled = true
	return m.confirm, nil
}
func (m *mockPrompter) SelectProfile([]account.Profile, string) (string, error) { return "", nil }

// mockAppStore implements appstore.ProfileAppStore with configurable behavior.
// No ipatool types — pure our-own types (adapter isolation verified).
type mockAppStore struct {
	endpoint     string
	endpointErr  error
	loginResults []appstore.LoginResult
	loginErrors  []error
	loginCalls   int
	revokeErr    error
	revokeCalled bool

	// NEW (T1): query fields — zero-value defaults so auth tests are unaffected
	accountInfoResult appstore.AccountInfoResult
	accountInfoErr    error
	accountInfoCalled bool
	searchResults     []appstore.AppInfo
	searchErr         error
}

func (m *mockAppStore) GetAuthEndpoint() (string, error) {
	return m.endpoint, m.endpointErr
}

func (m *mockAppStore) Login(appstore.LoginInput) (appstore.LoginResult, error) {
	idx := m.loginCalls
	m.loginCalls++
	var result appstore.LoginResult
	var err error
	if idx < len(m.loginResults) {
		result = m.loginResults[idx]
	}
	if idx < len(m.loginErrors) {
		err = m.loginErrors[idx]
	}
	return result, err
}

func (m *mockAppStore) Revoke() error {
	m.revokeCalled = true
	return m.revokeErr
}

// NEW (T1): query methods — zero-value defaults, auth tests don't call these
func (m *mockAppStore) AccountInfo() (appstore.AccountInfoResult, error) {
	m.accountInfoCalled = true
	return m.accountInfoResult, m.accountInfoErr
}

func (m *mockAppStore) Search(string, int64) ([]appstore.AppInfo, error) {
	return m.searchResults, m.searchErr
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
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
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
		endpoint: "https://auth.example.com",
		loginResults: []appstore.LoginResult{
			{}, // first call: ErrAuthCodeRequired (result discarded)
			{Name: "Alice", Email: "alice@example.com", StoreFront: "143441"},
		},
		loginErrors: []error{appstore.ErrAuthCodeRequired, nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	assert.Contains(t, output, "Logged in")
	assert.Contains(t, output, "Alice")
	assert.NotNil(t, store.upserted)
	assert.Equal(t, "alice_example_com", store.upserted.ID)
	assert.Equal(t, "alice_example_com", store.setActiveCalled)
	assert.Contains(t, output, "2FA")
	assert.True(t, store.saved)
	assert.NotContains(t, output, "correct-password")
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
	}
	mockAS := &mockAppStore{
		endpoint:     "https://auth.example.com",
		loginResults: []appstore.LoginResult{{Name: "Alice", Email: "alice@example.com"}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	assert.NotContains(t, output, "2FA")
	assert.Contains(t, output, "Logged in")
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
	prompter := &mockPrompter{email: "bob@example.com", password: "bob-pass"}
	mockAS := &mockAppStore{
		endpoint:     "https://auth.example.com",
		loginResults: []appstore.LoginResult{{Name: "Bob", Email: "bob@example.com"}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)
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
	prompter := &mockPrompter{email: "alice@example.com", password: "new-pass"}
	mockAS := &mockAppStore{
		endpoint:     "https://auth.example.com",
		loginResults: []appstore.LoginResult{{Name: "New Name", Email: "alice@example.com"}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	require.NotNil(t, store.upserted)
	assert.Equal(t, "alice_example_com", store.upserted.ID)
	assert.Equal(t, "New Name", store.upserted.Name)
	assert.Equal(t, "", store.setActiveCalled, "SetActive should NOT be called on refresh")
}

// =============================================================================
// E2E-018 / AC-04-7: After remove, same-email login is FRESH (not refresh)
// =============================================================================

func TestAuthLogin_AfterRemove_FreshLogin(t *testing.T) {
	// Start: bob existed but was removed. Store is empty (no profiles, no active).
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "bob@example.com",
		password: "bob-pass",
	}
	mockAS := &mockAppStore{
		endpoint:     "https://auth.example.com",
		loginResults: []appstore.LoginResult{{Name: "Bob New", Email: "bob@example.com", StoreFront: "143441"}},
		loginErrors:  []error{nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	// AC-04-7: profile is created fresh (Upsert called with correct ID)
	require.NotNil(t, store.upserted)
	assert.Equal(t, "bob_example_com", store.upserted.ID)
	assert.Equal(t, "Bob New", store.upserted.Name)

	// AC-01-3: since this is the first profile (active was ""), auto-active
	assert.Equal(t, "bob_example_com", store.setActiveCalled,
		"first profile after remove should auto-active (fresh, not refresh)")
}

// =============================================================================
// E2E-026 / AC-06-2: Wrong 2FA code
// =============================================================================

func TestAuthLogin_Wrong2FA_FailsWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{
		email:    "alice@example.com",
		password: "correct",
		authCode: "000000",
	}
	appleErr := errors.New("invalid verification code")
	mockAS := &mockAppStore{
		endpoint:    "https://auth.example.com",
		loginErrors: []error{appstore.ErrAuthCodeRequired, appleErr},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid verification code")
	assert.Contains(t, err.Error(), "2FA")
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
		endpoint:    "https://auth.example.com",
		loginErrors: []error{appleErr},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
	assert.Contains(t, err.Error(), "verify your credentials")
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
}

// =============================================================================
// E2E-029 / AC-07-2: Ctrl-C at email prompt
// =============================================================================

func TestAuthLogin_CtrlC_NoSideEffects(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	prompter := &mockPrompter{emailErr: errors.New("interrupt")}
	mockAS := &mockAppStore{}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
	assert.Equal(t, 0, mockAS.loginCalls)
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
				endpoint:     "https://e.test",
				loginResults: []appstore.LoginResult{{Name: "X", Email: tc.email}},
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
		endpoint:     "https://e.test",
		loginResults: []appstore.LoginResult{{Name: "X", Email: "x@y.com"}},
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
		endpoint: "https://e.test",
		loginResults: []appstore.LoginResult{
			{},
			{Name: "Alice", Email: "a@b.com"},
		},
		loginErrors: []error{appstore.ErrAuthCodeRequired, nil},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	output, err := helperRunLoginCmd(t, deps)
	require.NoError(t, err)

	assert.True(t, strings.Contains(output, "Contacting") || strings.Contains(output, "Apple"),
		"should contain progress message, got: %s", output)
	assert.NotContains(t, output, "secret-pass-123", "password must not appear in output")
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
		authCodeErr: errors.New("interrupt"),
	}
	mockAS := &mockAppStore{
		endpoint:    "https://e.test",
		loginErrors: []error{appstore.ErrAuthCodeRequired},
	}
	deps := helperMakeLoginDeps(store, prompter, mockAS)

	_, err := helperRunLoginCmd(t, deps)
	require.Error(t, err)
	assert.Nil(t, store.upserted)
	assert.False(t, store.saved)
	assert.Equal(t, 1, mockAS.loginCalls)
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

func TestAuthLogout_DefaultActive(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com", Name: "Alice"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test-config",
	}

	output, err := helperRunLogoutCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "Logged out")
	assert.Contains(t, output, "alice_test")
	assert.True(t, mockAS.revokeCalled)
}

func TestAuthLogout_ExplicitProfile(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true, "bob_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test",
	}

	output, err := helperRunLogoutCmd(t, deps, "bob_test")
	require.NoError(t, err)
	assert.Contains(t, output, "bob_test")
	assert.True(t, mockAS.revokeCalled)
}

func TestAuthLogout_NonExistent_ErrorWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	deps := Deps{Store: store, ConfigRoot: "/tmp/test"}

	_, err := helperRunLogoutCmd(t, deps, "ghost_test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "accounts list")
}

func TestAuthLogout_NoActive_ErrorWithHint(t *testing.T) {
	store := &mockStore{activeID: "", credentials: map[string]bool{}}
	deps := Deps{Store: store, ConfigRoot: "/tmp/test"}

	_, err := helperRunLogoutCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
	assert.Contains(t, err.Error(), "accounts use")
}

func TestAuthLogout_AlreadyLoggedOut_Idempotent(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		credentials: map[string]bool{"alice_test": false},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test",
	}

	output, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.NoError(t, err)
	assert.Contains(t, output, "already logged out")
	assert.False(t, mockAS.revokeCalled)
}

func TestAuthLogout_PreservesMetadata(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com", Name: "Alice"}},
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test",
	}

	_, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.NoError(t, err)
	assert.Equal(t, "", store.removedID, "Remove should NOT be called")

	// AC-05-6: metadata preserved with name/email unchanged (Spok validate finding).
	profiles, _ := store.List()
	require.Len(t, profiles, 1)
	assert.Equal(t, "alice_test", profiles[0].ID)
	assert.Equal(t, "alice@test.com", profiles[0].Email, "email should be unchanged")
	assert.Equal(t, "Alice", profiles[0].Name, "name should be unchanged")
}

func TestAuthLogout_DoubleLogout_Idempotent(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test",
	}

	_, err := helperRunLogoutCmd(t, deps)
	require.NoError(t, err)
	assert.True(t, mockAS.revokeCalled)

	store.credentials["alice_test"] = false
	_, err = helperRunLogoutCmd(t, deps)
	require.NoError(t, err)
}

func TestAuthLogout_RevokeFailure_ReportsError(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{revokeErr: errors.New("keychain locked")}
	deps := Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return mockAS, nil },
		ConfigRoot:      "/tmp/test",
	}

	_, err := helperRunLogoutCmd(t, deps, "alice_test")
	require.Error(t, err)
}
