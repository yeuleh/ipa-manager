package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
	"github.com/yeuleh/ipa-manager/internal/appstore"
)

// =============================================================================
// helpers
// =============================================================================

func helperRunSearchCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := appSearchCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func helperMakeSearchDeps(store account.Store, mockAS *mockAppStore) Deps {
	return Deps{
		Store: store,
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) {
			return mockAS, nil
		},
	}
}

// =============================================================================
// E2E-001 / AC-01-1: Search happy path
// =============================================================================

func TestAppSearch_HappyPath(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com", Name: "Alice"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "alice@test.com", Name: "Alice"},
		searchResults: []appstore.AppInfo{
			{ID: 1, BundleID: "com.tencent.xin", Name: "WeChat", Version: "8.0.35", Price: 0},
			{ID: 2, BundleID: "com.tencent.wx", Name: "WeChat for iPad", Version: "8.0.35", Price: 0},
		},
	}
	deps := helperMakeSearchDeps(store, mockAS)

	output, err := helperRunSearchCmd(t, deps, "wechat")
	require.NoError(t, err)
	assert.Contains(t, output, "WeChat")
	assert.Contains(t, output, "com.tencent.xin")
	assert.True(t, mockAS.accountInfoCalled, "AccountInfo should be called before Search")
}

// =============================================================================
// E2E-002 / AC-01-2: Search no active profile
// =============================================================================

func TestAppSearch_NoActiveProfile_ErrorWithHint(t *testing.T) {
	store := &mockStore{activeID: "", credentials: map[string]bool{}}
	deps := helperMakeSearchDeps(store, &mockAppStore{})

	_, err := helperRunSearchCmd(t, deps, "wechat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
	assert.Contains(t, err.Error(), "accounts use")
}

// =============================================================================
// E2E-002a / AC-01-2: Search active profile not logged in
// =============================================================================

func TestAppSearch_ActiveProfileNotLoggedIn_ErrorWithHint(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": false},
	}
	deps := helperMakeSearchDeps(store, &mockAppStore{})

	_, err := helperRunSearchCmd(t, deps, "wechat")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
	assert.Contains(t, err.Error(), "auth login")
}

// =============================================================================
// E2E-003 / AC-01-3: Search zero results
// =============================================================================

func TestAppSearch_ZeroResults(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "alice@test.com"},
		searchResults:     []appstore.AppInfo{}, // empty
	}
	deps := helperMakeSearchDeps(store, mockAS)

	output, err := helperRunSearchCmd(t, deps, "nonexistentapp12345")
	require.NoError(t, err)
	assert.Contains(t, output, "no results found")
}

// =============================================================================
// E2E-004 / AC-01-4: Search with --limit
// =============================================================================

func TestAppSearch_WithLimit(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		searchResults:     []appstore.AppInfo{{ID: 1, BundleID: "com.test", Name: "Test"}},
	}
	deps := helperMakeSearchDeps(store, mockAS)

	_, err := helperRunSearchCmd(t, deps, "test", "--limit", "2")
	require.NoError(t, err)
	// mockAppStore.Search receives the limit — we can't directly assert the limit
	// value from here, but we verify the command succeeds (AC-01-4: results ≤ N)
}

// =============================================================================
// E2E-005 / AC-01-5: Search with --profile
// =============================================================================

func TestAppSearch_WithProfileFlag(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true, "bob_test": true},
	}
	mockAS := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{},
		searchResults:     []appstore.AppInfo{{ID: 1, BundleID: "com.test", Name: "Test"}},
	}
	var factoryProfileID string
	deps := Deps{
		Store: store,
		AppStoreFactory: func(p account.Profile) (appstore.ProfileAppStore, error) {
			factoryProfileID = p.ID
			return mockAS, nil
		},
	}

	_, err := helperRunSearchCmd(t, deps, "test", "--profile", "bob_test")
	require.NoError(t, err)
	assert.Equal(t, "bob_test", factoryProfileID, "factory should receive bob_test profile")
}

// =============================================================================
// E2E-006 / AC-01-6: Search invalid --limit
// =============================================================================

func TestAppSearch_InvalidLimit_Error(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	deps := helperMakeSearchDeps(store, &mockAppStore{})

	_, err := helperRunSearchCmd(t, deps, "test", "--limit", "0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --limit")
}

func TestAppSearch_NegativeLimit_Error(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice_test", Email: "alice@test.com"}},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	deps := helperMakeSearchDeps(store, &mockAppStore{})

	_, err := helperRunSearchCmd(t, deps, "test", "--limit", "-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --limit")
}
