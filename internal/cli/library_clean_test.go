package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/library"
)

// =============================================================================
// helpers
// =============================================================================

func helperRunCleanCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := libraryCleanCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func helperMakeCleanDeps(t *testing.T, store interface{}, libStore library.Store, prompter interface{}) Deps {
	t.Helper()
	deps := Deps{
		Store:        helperDownloadStore(),
		LibraryStore: libStore,
		ConfigRoot:   t.TempDir(),
	}
	return deps
}

// helperCreateTempFile creates a real file in temp dir and returns its path.
func helperCreateTempFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("fake-ipa"), 0o644))
	return path
}

func cleanEntry(bundleID, version, path string) library.Entry {
	return library.Entry{
		BundleID:     bundleID,
		Version:      version,
		FilePath:     path,
		FileSize:     1024,
		DownloadedAt: time.Now().UTC(),
	}
}

// =============================================================================
// E2E-025 / AC-05-2: clean empty library
// =============================================================================

func TestLibraryClean_Empty(t *testing.T) {
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{entries: []library.Entry{}}, nil)
	output, err := helperRunCleanCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "already empty")
}

// =============================================================================
// E2E-028 / AC-05-4: clean non-existent bundle-id
// =============================================================================

func TestLibraryClean_BundleID_NotFound(t *testing.T) {
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{entries: []library.Entry{}}, nil)
	output, err := helperRunCleanCmd(t, deps, "com.nonexistent")
	require.NoError(t, err)
	assert.Contains(t, output, "no IPA")
}

// =============================================================================
// E2E-050 / AC-05-12: clean non-existent version
// =============================================================================

func TestLibraryClean_VersionNotFound(t *testing.T) {
	libStore := &mockLibraryStore{
		entries: []library.Entry{cleanEntry("com.test", "1.0", "/lib/test.ipa")},
	}
	deps := helperMakeCleanDeps(t, nil, libStore, nil)
	output, err := helperRunCleanCmd(t, deps, "com.test", "--version", "9.9.9")
	require.NoError(t, err)
	assert.Contains(t, output, "no IPA")
	assert.Contains(t, output, "9.9.9")
}

// =============================================================================
// E2E-034 / AC-05-9: clean non-interactive empty = no-op (exit 0)
// =============================================================================

func TestLibraryClean_NonInteractive_Empty(t *testing.T) {
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{entries: []library.Entry{}}, nil)
	output, err := helperRunCleanCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "already empty")
}

// =============================================================================
// E2E-033 / AC-05-9: clean non-interactive with entries = error
// =============================================================================

func TestLibraryClean_NonInteractive_Destructive(t *testing.T) {
	// checkInteractive defaults to false in test runner
	realFile := helperCreateTempFile(t, "test.ipa")
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{
		entries:       []library.Entry{cleanEntry("com.test", "1.0", realFile)},
		cleanAllCount: 1,
	}, nil)
	_, err := helperRunCleanCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirmation required")
}

// =============================================================================
// E2E-024 / AC-05-1: clean all (with interactive confirm yes)
// =============================================================================

func TestLibraryClean_All_ConfirmYes(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	f1 := helperCreateTempFile(t, "test.ipa")
	f2 := helperCreateTempFile(t, "other.ipa")
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{
		entries: []library.Entry{
			cleanEntry("com.test", "1.0", f1),
			cleanEntry("com.other", "2.0", f2),
		},
		cleanAllCount: 2,
	}, nil)
	deps.UI = &mockPrompter{confirm: true}

	output, err := helperRunCleanCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "Removed")
	assert.Contains(t, output, "2")
}

// =============================================================================
// E2E-024 cancel: clean all (with interactive confirm no)
// =============================================================================

func TestLibraryClean_All_ConfirmNo(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	f1 := helperCreateTempFile(t, "test.ipa")
	deps := helperMakeCleanDeps(t, nil, &mockLibraryStore{
		entries:       []library.Entry{cleanEntry("com.test", "1.0", f1)},
		cleanAllCount: 1,
	}, nil)
	deps.UI = &mockPrompter{confirm: false}

	output, err := helperRunCleanCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, output, "cancelled")
}

// =============================================================================
// E2E-049 / AC-05-11: clean specific version (--version flag)
// =============================================================================

func TestLibraryClean_SpecificVersion(t *testing.T) {
	origInteractive := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = origInteractive }()

	f1 := helperCreateTempFile(t, "v1.ipa")
	f2 := helperCreateTempFile(t, "v2.ipa")
	libStore := &mockLibraryStore{
		entries: []library.Entry{
			cleanEntry("com.test", "1.0", f1),
			cleanEntry("com.test", "2.0", f2),
		},
	}
	deps := helperMakeCleanDeps(t, nil, libStore, nil)
	deps.UI = &mockPrompter{confirm: true}

	output, err := helperRunCleanCmd(t, deps, "com.test", "--version", "1.0")
	require.NoError(t, err)
	assert.Contains(t, output, "Removed")
	assert.Contains(t, output, "1.0")
}
