package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func helperNewStore(t *testing.T) (Store, string) {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir), dir
}

func helperEntry(bundleID, version, path string) Entry {
	return Entry{
		BundleID:     bundleID,
		AppID:        123,
		Version:      version,
		FilePath:     path,
		FileSize:     1024,
		DownloadedAt: time.Now().UTC(),
	}
}

// --- Add + List ---

func TestStore_AddAndList(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("profile1", helperEntry("com.test", "1.0.0", "/path/a.ipa")))

	entries, err := s.List("profile1")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "com.test", entries[0].BundleID)
}

func TestStore_AddSameVersion_Replaces(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/old.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/new.ipa")))

	entries, err := s.List("p1")
	require.NoError(t, err)
	assert.Len(t, entries, 1, "same (bid,ver) should replace, not append")
	assert.Equal(t, "/new.ipa", entries[0].FilePath)
}

func TestStore_AddDifferentVersion_Coexists(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/v1.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "2.0.0", "/v2.ipa")))

	entries, err := s.List("p1")
	require.NoError(t, err)
	assert.Len(t, entries, 2, "different versions should coexist")
}

func TestStore_List_SortedByVersionDesc(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/v1.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "2.0.0", "/v2.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.5.0", "/v15.ipa")))

	entries, err := s.List("p1")
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", entries[0].Version) // newest first
	assert.Equal(t, "1.5.0", entries[1].Version)
	assert.Equal(t, "1.0.0", entries[2].Version)
}

func TestStore_List_EmptyProfile(t *testing.T) {
	s, _ := helperNewStore(t)
	entries, err := s.List("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// --- Get ---

func TestStore_Get_MultipleVersions(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/v1.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "2.0.0", "/v2.ipa")))

	entries, err := s.Get("p1", "com.test")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestStore_Get_NotFound_ReturnsEmpty(t *testing.T) {
	s, _ := helperNewStore(t)
	entries, err := s.Get("p1", "com.nonexistent")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// --- GetVersion ---

func TestStore_GetVersion_Found(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/v1.ipa")))

	entry, err := s.GetVersion("p1", "com.test", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "/v1.ipa", entry.FilePath)
}

func TestStore_GetVersion_NotFound(t *testing.T) {
	s, _ := helperNewStore(t)
	_, err := s.GetVersion("p1", "com.test", "9.9.9")
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

// --- Remove (all versions) ---

func TestStore_Remove_DeletesAllVersionsAndFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Create actual files to verify deletion
	ipa1 := filepath.Join(dir, "p1", "v1.ipa")
	ipa2 := filepath.Join(dir, "p1", "v2.ipa")
	require.NoError(t, os.MkdirAll(filepath.Dir(ipa1), 0o700))
	require.NoError(t, os.WriteFile(ipa1, []byte("fake"), 0o644))
	require.NoError(t, os.WriteFile(ipa2, []byte("fake"), 0o644))

	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", ipa1)))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "2.0.0", ipa2)))
	require.NoError(t, s.Add("p1", helperEntry("com.other", "1.0.0", filepath.Join(dir, "p1", "other.ipa"))))

	count, err := s.Remove("p1", "com.test")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.NoFileExists(t, ipa1, "file should be deleted")
	assert.NoFileExists(t, ipa2, "file should be deleted")

	// com.other should remain
	entries, _ := s.List("p1")
	assert.Len(t, entries, 1)
	assert.Equal(t, "com.other", entries[0].BundleID)
}

func TestStore_Remove_NotFound(t *testing.T) {
	s, _ := helperNewStore(t)
	_, err := s.Remove("p1", "com.nonexistent")
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

// --- RemoveVersion ---

func TestStore_RemoveVersion_PreservesOtherVersions(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0.0", "/v1.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.test", "2.0.0", "/v2.ipa")))

	require.NoError(t, s.RemoveVersion("p1", "com.test", "1.0.0"))

	entries, _ := s.List("p1")
	assert.Len(t, entries, 1, "only 1 version should remain")
	assert.Equal(t, "2.0.0", entries[0].Version)
}

func TestStore_RemoveVersion_NotFound(t *testing.T) {
	s, _ := helperNewStore(t)
	err := s.RemoveVersion("p1", "com.test", "9.9.9")
	assert.ErrorIs(t, err, ErrEntryNotFound)
}

// --- CleanAll ---

func TestStore_CleanAll_RemovesEverything(t *testing.T) {
	s, _ := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.a", "1.0", "/a.ipa")))
	require.NoError(t, s.Add("p1", helperEntry("com.b", "2.0", "/b.ipa")))

	count, err := s.CleanAll("p1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	entries, _ := s.List("p1")
	assert.Empty(t, entries)
}

func TestStore_CleanAll_EmptyProfile(t *testing.T) {
	s, _ := helperNewStore(t)
	count, err := s.CleanAll("nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// --- Atomic write ---

func TestStore_AtomicWrite_NoTmpFile(t *testing.T) {
	s, dir := helperNewStore(t)
	require.NoError(t, s.Add("p1", helperEntry("com.test", "1.0", "/test.ipa")))

	// No .tmp file should remain
	_, err := os.Stat(filepath.Join(dir, "p1", "index.json.tmp"))
	assert.True(t, os.IsNotExist(err), "tmp file should not remain after atomic write")
}
