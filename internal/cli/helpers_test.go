package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/account"
)

// =============================================================================
// resolveProfile unit tests (AC-08-1 ~ AC-08-3, cross-command coverage)
// =============================================================================

func TestResolveProfile_NoProfileFlag_UsesActive(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com", Name: "Alice"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true},
	}
	deps := Deps{Store: store}

	profile, err := resolveProfile(deps, "", true)
	require.NoError(t, err)
	assert.Equal(t, "alice_test", profile.ID)
}

func TestResolveProfile_ProfileFlag_UsesExplicit(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
			{ID: "bob_test", Email: "bob@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": true, "bob_test": true},
	}
	deps := Deps{Store: store}

	profile, err := resolveProfile(deps, "bob_test", true)
	require.NoError(t, err)
	assert.Equal(t, "bob_test", profile.ID)
}

func TestResolveProfile_ProfileNotFound_ErrorWithHint(t *testing.T) {
	store := &mockStore{credentials: map[string]bool{}}
	deps := Deps{Store: store}

	_, err := resolveProfile(deps, "ghost_test", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "accounts list")
}

func TestResolveProfile_NoActiveProfile_ErrorWithHint(t *testing.T) {
	store := &mockStore{activeID: "", credentials: map[string]bool{}}
	deps := Deps{Store: store}

	_, err := resolveProfile(deps, "", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
	assert.Contains(t, err.Error(), "accounts use")
}

func TestResolveProfile_NotLoggedIn_ErrorWithHint(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": false},
	}
	deps := Deps{Store: store}

	_, err := resolveProfile(deps, "", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
	assert.Contains(t, err.Error(), "auth login")
}

func TestResolveProfile_RequireCredentialsFalse_SkipsCheck(t *testing.T) {
	store := &mockStore{
		profiles: []account.Profile{
			{ID: "alice_test", Email: "alice@test.com"},
		},
		activeID:    "alice_test",
		credentials: map[string]bool{"alice_test": false}, // logged out
	}
	deps := Deps{Store: store}

	// requireCredentials=false → should succeed even when not logged in
	profile, err := resolveProfile(deps, "", false)
	require.NoError(t, err)
	assert.Equal(t, "alice_test", profile.ID)
}
