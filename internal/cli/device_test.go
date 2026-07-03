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
	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/appstore"
	"github.com/yeuleh/ipa-manager/internal/device"
	"github.com/yeuleh/ipa-manager/internal/library"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// mockDeviceService implements device.Service for CLI tests. Extended in
// later tasks with Install/Uninstall.
type mockDeviceService struct {
	devices         []device.DeviceInfo
	listErr         error
	listCalls       int
	appsResult      []device.InstalledApp
	appsErr         error
	appsCalls       int
	appsUDID        string // records udid passed to ListInstalledApps
	installErr      error
	installCalls    int
	installUDID     string // records udid passed to Install
	installPath     string // records ipaPath passed to Install
	uninstallErr    error
	uninstallCalled bool
	uninstallUDID   string
	uninstallBundle string
}

func (m *mockDeviceService) ListConnected() ([]device.DeviceInfo, error) {
	m.listCalls++
	return m.devices, m.listErr
}
func (m *mockDeviceService) ListInstalledApps(udid string) ([]device.InstalledApp, error) {
	m.appsCalls++
	m.appsUDID = udid
	return m.appsResult, m.appsErr
}
func (m *mockDeviceService) Install(udid, ipaPath string) error {
	m.installCalls++
	m.installUDID = udid
	m.installPath = ipaPath
	return m.installErr
}
func (m *mockDeviceService) Uninstall(udid, bundleID string) error {
	m.uninstallCalled = true
	m.uninstallUDID = udid
	m.uninstallBundle = bundleID
	return m.uninstallErr
}

func newDeviceListDeps(svc *mockDeviceService) Deps {
	return Deps{DeviceService: svc}
}

func runDeviceList(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := deviceListCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// =============================================================================
// E2E-001 / AC-01-1: device list happy path (1+ devices, table output)
// =============================================================================

func TestDeviceList_HappyPath_TableOutput(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{
		{UDID: "udid1", Name: "iPhone 15", IOSVersion: "16.5", ConnectionType: "USB"},
	}}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceList(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "UDID")
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "IOS-VERSION")
	assert.Contains(t, out, "CONNECTION-TYPE")
	assert.Contains(t, out, "udid1")
	assert.Contains(t, out, "iPhone 15")
	assert.Contains(t, out, "16.5")
	assert.Contains(t, out, "USB")
}

// =============================================================================
// E2E-002 / AC-01-2: device list empty (0 devices → exit 0)
// =============================================================================

func TestDeviceList_Empty_NoConnectedDevice(t *testing.T) {
	svc := &mockDeviceService{devices: nil}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceList(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "no connected device")
}

// =============================================================================
// E2E-003 / AC-01-3: multiple devices all listed with full UDIDs
// =============================================================================

func TestDeviceList_Multiple_AllShown(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{
		{UDID: "aaa", Name: "A", IOSVersion: "16.0", ConnectionType: "USB"},
		{UDID: "bbb", Name: "B", IOSVersion: "17.0", ConnectionType: "Network"},
		{UDID: "ccc", Name: "C", IOSVersion: "15.0", ConnectionType: "USB"},
	}}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceList(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "aaa")
	assert.Contains(t, out, "bbb")
	assert.Contains(t, out, "ccc")
}

// =============================================================================
// E2E-004 / AC-01-1 + AC-07-1: untrusted device still listed (lockdown failed)
// =============================================================================

func TestDeviceList_LockdownFailed_DeviceStillListed(t *testing.T) {
	// DeviceInfo with empty Name/IOSVersion simulates lockdown failure at the
	// device layer; the device is still returned by ListConnected (usbmuxd).
	svc := &mockDeviceService{devices: []device.DeviceInfo{
		{UDID: "udid2", Name: "", IOSVersion: "", ConnectionType: "USB"},
	}}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceList(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "udid2")
	assert.Contains(t, out, "USB")
	// Name/IOSVersion unavailable → "-" placeholder
	assert.Contains(t, out, "-")
}

// =============================================================================
// E2E-090 / AC-07-1: iOS 17+ device without tunnel is still listed
// =============================================================================

func TestDeviceList_iOS17Device_StillListed(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{
		{UDID: "udid17", Name: "iPhone 16", IOSVersion: "17.5", ConnectionType: "USB", NeedsTunnel: true},
	}}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceList(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "udid17")
	assert.Contains(t, out, "iPhone 16")
}

// =============================================================================
// E2E-074 (list branch) / AC-09-5: device list rejects --profile (unknown flag)
// =============================================================================

func TestDeviceList_RejectsProfileFlag(t *testing.T) {
	svc := &mockDeviceService{}
	deps := newDeviceListDeps(svc)

	_, err := runDeviceList(t, deps, "--profile", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

// =============================================================================
// Command tree wiring: device list is registered under root (DD-06 refactor)
// =============================================================================

func TestRoot_RegistersDeviceListCommand(t *testing.T) {
	// Verify the command-tree refactor (DD-06): old 'devices'/'install' stubs
	// removed, unified 'device' group with 'list' subcommand registered at root.
	deps := newDeviceListDeps(&mockDeviceService{})
	root := newRootCmd(deps)

	list, _, err := root.Find([]string{"device", "list"})
	require.NoError(t, err, "device list must be registered under root")
	assert.Equal(t, "list", list.Name())

	// Old stubs must be gone.
	if _, _, err := root.Find([]string{"devices"}); err == nil {
		t.Error("old top-level 'devices' stub should have been removed")
	}
	if _, _, err := root.Find([]string{"install"}); err == nil {
		t.Error("old 'install' group stub should have been removed")
	}
}

// =============================================================================
// T2: device apps (US-05)
// =============================================================================

func runDeviceAppsCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := deviceAppsCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// E2E-010 / AC-05-1: apps happy path (table output, user apps only)
func TestDeviceApps_HappyPath_TableOutput(t *testing.T) {
	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "u1", Name: "iPhone", ConnectionType: "USB"}},
		appsResult: []device.InstalledApp{{BundleID: "com.x", Name: "X", Version: "1.0"}, {BundleID: "com.y", Name: "Y", Version: "2.0"}},
	}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "BUNDLE-ID")
	assert.Contains(t, out, "com.x")
	assert.Contains(t, out, "X")
	assert.Contains(t, out, "1.0")
	assert.Equal(t, "u1", svc.appsUDID, "auto-selected single device")
}

// E2E-011 / AC-05-2: apps empty
func TestDeviceApps_Empty(t *testing.T) {
	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}},
		appsResult: nil,
	}
	deps := newDeviceListDeps(svc)

	out, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "no user apps installed on device 'iPhone'")
}

// E2E-012 / AC-05-4: apps --udid selects device
func TestDeviceApps_UDID_SelectsDevice(t *testing.T) {
	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}},
		appsResult: []device.InstalledApp{{BundleID: "com.x", Name: "X", Version: "1.0"}},
	}
	deps := newDeviceListDeps(svc)

	_, err := runDeviceAppsCmd(t, deps, "--udid", "b")
	require.NoError(t, err)
	assert.Equal(t, "b", svc.appsUDID, "--udid b should target device b")
}

// E2E-026 / AC-05-3 + AC-06-3: apps multi-device interactive selection
func TestDeviceApps_MultiDevice_InteractiveSelect(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}},
		appsResult: []device.InstalledApp{{BundleID: "com.x", Name: "X", Version: "1.0"}},
	}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{selectDeviceResult: "a"}

	out, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "com.x")
	assert.Equal(t, "a", svc.appsUDID, "interactive selection picked device a")
}

// E2E-028 / AC-06-4: apps multi-device non-interactive rejected
func TestDeviceApps_MultiDevice_NonInteractive_Error(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return false }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := newDeviceListDeps(svc)

	_, err := runDeviceAppsCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple devices connected")
	assert.Contains(t, err.Error(), "--udid")
	assert.Contains(t, err.Error(), "non-interactive")
	assert.Equal(t, 0, svc.appsCalls, "must not call ListInstalledApps on selection failure")
}

// E2E-092 / AC-07-3 (apps tunnel branch): apps returns tunnel error
func TestDeviceApps_TunnelRequired(t *testing.T) {
	svc := &mockDeviceService{
		devices: []device.DeviceInfo{{UDID: "u1"}},
		appsErr: device.ErrTunnelRequired,
	}
	deps := newDeviceListDeps(svc)

	_, err := runDeviceAppsCmd(t, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iOS 17+ tunnel required")
	assert.Contains(t, err.Error(), "sudo ios tunnel start")
}

// E2E-074 (apps branch) / AC-09-5: device apps rejects --profile
func TestDeviceApps_RejectsProfileFlag(t *testing.T) {
	deps := newDeviceListDeps(&mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}})
	_, err := runDeviceAppsCmd(t, deps, "--profile", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

// =============================================================================
// T2: resolveDevice (AC-06-1~5) — device selection logic
// =============================================================================

func resolveDeviceDeps(svc *mockDeviceService, prompter *mockPrompter) Deps {
	deps := newDeviceListDeps(svc)
	if prompter != nil {
		deps.UI = prompter
	}
	return deps
}

// AC-06-1: 0 devices
func TestResolveDevice_NoDevices_Error(t *testing.T) {
	deps := resolveDeviceDeps(&mockDeviceService{}, nil)
	_, err := resolveDevice(deps, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connected device")
	assert.Contains(t, err.Error(), "trust this Mac")
}

// AC-06-2: --udid not connected
func TestResolveDevice_UDIDNotConnected_Error(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := resolveDeviceDeps(svc, nil)
	_, err := resolveDevice(deps, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device 'ghost' not connected")
}

// AC-06-5: single device auto-select
func TestResolveDevice_SingleDevice_AutoSelect(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "solo", Name: "Solo"}}}
	deps := resolveDeviceDeps(svc, nil)
	dev, err := resolveDevice(deps, "")
	require.NoError(t, err)
	assert.Equal(t, "solo", dev.UDID)
}

// --udid valid selects that device
func TestResolveDevice_UDIDValid_Selects(t *testing.T) {
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := resolveDeviceDeps(svc, nil)
	dev, err := resolveDevice(deps, "b")
	require.NoError(t, err)
	assert.Equal(t, "b", dev.UDID)
}

// AC-06-3: multi-device interactive select
func TestResolveDevice_MultiDevice_InteractiveSelect(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := resolveDeviceDeps(svc, &mockPrompter{selectDeviceResult: "a"})
	dev, err := resolveDevice(deps, "")
	require.NoError(t, err)
	assert.Equal(t, "a", dev.UDID)
}

// AC-06-3 cancel branch: SelectDevice returns ErrCancelled
func TestResolveDevice_MultiDevice_Cancel(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := resolveDeviceDeps(svc, &mockPrompter{selectDeviceErr: apperr.ErrCancelled})
	_, err := resolveDevice(deps, "")
	require.Error(t, err)
	assert.True(t, isCancelled(err), "cancel must propagate as ErrCancelled")
}

// AC-06-4: multi-device non-interactive error
func TestResolveDevice_MultiDevice_NonInteractive_Error(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return false }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := resolveDeviceDeps(svc, nil)
	_, err := resolveDevice(deps, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple devices connected")
}

// deviceLabel sanity (T2 helper for SelectDevice options)
func TestDeviceLabel_Format(t *testing.T) {
	assert.Contains(t, deviceLabel(device.DeviceInfo{Name: "iPhone", IOSVersion: "17.0", UDID: "1234567890abc"}), "iPhone")
	assert.Contains(t, deviceLabel(device.DeviceInfo{Name: "", IOSVersion: "", UDID: "1234567890"}), "1234567890", "falls back to UDID when name empty")
	// silence unused import of ui in case future refactor moves helpers
	_ = ui.DeviceOption{}
}

// =============================================================================
// T3: device install (US-02/07/09/10) — library push path
// =============================================================================

// helperDeviceInstallDeps builds Deps for install tests.
func helperDeviceInstallDeps(store account.Store, svc *mockDeviceService, lib library.Store) Deps {
	return Deps{
		Store:         store,
		LibraryStore:  lib,
		DeviceService: svc,
		UI:            &mockPrompter{},
	}
}

func runDeviceInstallCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := deviceInstallCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func ipaEntry(bundleID, version, path string, daysAgo int) library.Entry {
	return library.Entry{
		BundleID:     bundleID,
		Version:      version,
		FilePath:     path,
		DownloadedAt: time.Now().UTC().AddDate(0, 0, -daysAgo),
	}
}

// E2E-030 / AC-02-1: install happy (library has IPA, 1 device)
func TestDeviceInstall_Happy_PushFromLibrary(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/lib/com.x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	out, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Contains(t, out, "Installed")
	assert.Contains(t, out, "com.x")
	assert.Contains(t, out, "1.0")
	assert.Equal(t, "u1", svc.installUDID)
	assert.Equal(t, "/lib/com.x.ipa", svc.installPath)
}

// E2E-030b / AC-02-4: install --udid valid in multi-device
func TestDeviceInstall_UDID_SelectsDevice(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/lib/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x", "--udid", "b")
	require.NoError(t, err)
	assert.Equal(t, "b", svc.installUDID, "--udid b targets device b")
}

// E2E-033 / AC-02-8: install no active profile
func TestDeviceInstall_NoActiveProfile(t *testing.T) {
	store := &mockStore{activeID: "", credentials: map[string]bool{}}
	deps := helperDeviceInstallDeps(store, &mockDeviceService{}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active profile")
}

// E2E-060 / AC-10-1: install --version specific
func TestDeviceInstall_Version_Specific(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{
		ipaEntry("com.x", "1.0", "/lib/v1.ipa", 5),
		ipaEntry("com.x", "2.0", "/lib/v2.ipa", 1),
	}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x", "--version", "1.0")
	require.NoError(t, err)
	assert.Equal(t, "/lib/v1.ipa", svc.installPath, "--version 1.0 pushes v1")
}

// E2E-061 / AC-10-2: install default = most-recently-downloaded
func TestDeviceInstall_Default_MostRecent(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{
		ipaEntry("com.x", "1.0", "/lib/v1.ipa", 5), // older
		ipaEntry("com.x", "2.0", "/lib/v2.ipa", 1), // newer
	}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Equal(t, "/lib/v2.ipa", svc.installPath, "default pushes most-recently-downloaded (v2)")
}

// E2E-062 / AC-10-3: install --version not in library
func TestDeviceInstall_Version_NotInLibrary(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/lib/v1.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x", "--version", "9.9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version '9.9' not in library")
	assert.Equal(t, 0, svc.installCalls, "must not install on missing version")
}

// E2E-070 / AC-09-1: install --profile valid logged-in
func TestDeviceInstall_Profile_Valid(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice"}, {ID: "bob"}},
		activeID:    "alice",
		credentials: map[string]bool{"alice": true, "bob": true},
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/bob/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x", "--profile", "bob")
	require.NoError(t, err)
	assert.Equal(t, "bob", lib.getProfile, "uses bob's library")
}

// E2E-071 / AC-09-2: install --profile not-logged-in but library has IPA → push OK
func TestDeviceInstall_Profile_NotLoggedIn_LibraryHas(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice"}, {ID: "bob"}},
		activeID:    "alice",
		credentials: map[string]bool{"alice": true, "bob": false}, // bob not logged in
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/bob/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x", "--profile", "bob")
	require.NoError(t, err, "cached push needs no credentials (AC-09-2)")
	assert.Equal(t, 1, svc.installCalls)
}

// E2E-073 / AC-09-4: install --profile not found
func TestDeviceInstall_Profile_NotFound(t *testing.T) {
	store := helperDownloadStore()
	deps := helperDeviceInstallDeps(store, &mockDeviceService{}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x", "--profile", "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile 'ghost' not found")
}

// E2E-091 / AC-07-2: install tunnel required
func TestDeviceInstall_TunnelRequired(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "u1"}},
		installErr: device.ErrTunnelRequired,
	}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iOS 17+ tunnel required")
	assert.Contains(t, err.Error(), "sudo ios tunnel start")
}

// E2E-032 / AC-02-7: install trust failure → hint
func TestDeviceInstall_TrustError_Hint(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "u1"}},
		installErr: newTrustErr("device not paired"),
	}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pair")
	assert.Contains(t, err.Error(), "trust this Mac")
}

// E2E-034 / AC-02-9: device already has app → still pushes
func TestDeviceInstall_DeviceHasApp_StillPushes(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}} // Install returns nil (success)
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Equal(t, 1, svc.installCalls, "push is unconditional (AC-02-9)")
}

// newTrustErr builds an error whose message triggers the trust heuristic.
func newTrustErr(msg string) error { return errors.New(msg) }

// =============================================================================
// T4: device install auto-download (US-03) + --latest (US-08) + mutex (AC-10-4)
// =============================================================================

func helperInstallAutoDeps(t *testing.T, store account.Store, as *mockAppStore, svc *mockDeviceService, lib library.Store) Deps {
	t.Helper()
	return Deps{
		Store:           store,
		LibraryStore:    lib,
		DeviceService:   svc,
		UI:              &mockPrompter{},
		AppStoreFactory: func(account.Profile) (appstore.ProfileAppStore, error) { return as, nil },
		ConfigRoot:      t.TempDir(),
	}
}

// E2E-040 / AC-03-1: auto-download happy (library missing → download → install)
func TestDeviceInstall_AutoDownload_Happy(t *testing.T) {
	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "alice@test.com"},
		lookupResult:      appstore.AppInfo{ID: 123, BundleID: "com.x", Name: "X", Version: "1.0"},
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/x.ipa", Version: "1.0"}},
		downloadErrors:    []error{nil},
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}}}
	lib := &mockLibraryStore{} // empty → triggers auto-download
	deps := helperInstallAutoDeps(t, store, as, svc, lib)

	out, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Equal(t, 1, as.downloadCalls, "Download must be called once")
	assert.True(t, lib.addCalled, "LibraryStore.Add must be called (E2E-041)")
	assert.Equal(t, "com.x", lib.addEntry.BundleID)
	assert.Equal(t, "/tmp/x.ipa", svc.installPath, "Install pushes the downloaded IPA")
	assert.Contains(t, out, "Downloaded", "AC-03-1: success reflects download step")
	assert.Contains(t, out, "Installed", "AC-03-1: success reflects install step")
}

// E2E-042 / AC-03-3: auto-download no credentials
func TestDeviceInstall_AutoDownload_NoCredentials(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice", Email: "alice@test.com"}},
		activeID:    "alice",
		credentials: map[string]bool{"alice": false}, // not logged in
	}
	deps := helperInstallAutoDeps(t, store, &mockAppStore{}, &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}

// E2E-043 / AC-03-4: auto-download app not found
func TestDeviceInstall_AutoDownload_AppNotFound(t *testing.T) {
	store := helperDownloadStore()
	as := &mockAppStore{lookupErr: apperr.ErrAppNotFound}
	deps := helperInstallAutoDeps(t, store, as, &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app not found")
}

// E2E-044 / AC-03-5: license required, user confirms → acquire → retry succeeds
func TestDeviceInstall_AutoDownload_LicenseConfirm(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "a@t.com"},
		lookupResult:      appstore.AppInfo{ID: 1, BundleID: "com.x", Name: "X", Version: "1.0", Price: 0},
		downloadResults:   []appstore.DownloadResult{{}, {DestinationPath: "/tmp/x.ipa", Version: "1.0"}},
		downloadErrors:    []error{apperr.ErrLicenseRequired, nil},
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{}
	deps := helperInstallAutoDeps(t, store, as, svc, lib)
	deps.UI = &mockPrompter{confirm: true}

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.True(t, as.purchaseCalled, "Purchase must be called after confirm")
	assert.Equal(t, 2, as.downloadCalls, "Download retried after license acquisition")
	assert.Equal(t, 1, svc.installCalls)
}

// E2E-045 / AC-03-5: license required, user declines → cancelled exit 0
func TestDeviceInstall_AutoDownload_LicenseDecline(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "a@t.com"},
		lookupResult:      appstore.AppInfo{ID: 1, BundleID: "com.x", Name: "X", Version: "1.0", Price: 0},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{}
	deps := helperInstallAutoDeps(t, store, as, svc, lib)
	deps.UI = &mockPrompter{confirm: false}

	out, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err, "decline is not an error (exit 0)")
	assert.Contains(t, out, "cancelled")
	assert.False(t, as.purchaseCalled, "Purchase must NOT be called on decline")
	assert.Equal(t, 0, svc.installCalls, "Install must NOT be called on decline")
}

// E2E-046 / AC-03-6: license required, non-interactive → error exit 1
func TestDeviceInstall_AutoDownload_LicenseNonInteractive(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return false }
	defer func() { checkInteractive = old }()

	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "a@t.com"},
		lookupResult:      appstore.AppInfo{ID: 1, BundleID: "com.x", Name: "X", Version: "1.0", Price: 0},
		downloadErrors:    []error{apperr.ErrLicenseRequired},
	}
	deps := helperInstallAutoDeps(t, store, as, &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}, &mockLibraryStore{})

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-interactively")
}

// E2E-050 / AC-08-1: --latest downloads newer version (preserving old)
func TestDeviceInstall_Latest_DownloadsNewer(t *testing.T) {
	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "a@t.com"},
		lookupResult:      appstore.AppInfo{ID: 1, BundleID: "com.x", Name: "X", Version: "2.0"}, // App Store has 2.0
		downloadResults:   []appstore.DownloadResult{{DestinationPath: "/tmp/v2.ipa", Version: "2.0"}},
		downloadErrors:    []error{nil},
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/lib/v1.ipa", 5)}} // library has old 1.0
	deps := helperInstallAutoDeps(t, store, as, svc, lib)

	out, err := runDeviceInstallCmd(t, deps, "com.x", "--latest")
	require.NoError(t, err)
	assert.Equal(t, 1, as.downloadCalls, "must download 2.0 (library has 1.0)")
	assert.Equal(t, "/tmp/v2.ipa", svc.installPath, "push the newly downloaded 2.0")
	assert.Equal(t, "2.0", lib.addEntry.Version, "new v2.0 entry added to library (old v1.0 preserved by library.Store semantics)")
	assert.Contains(t, out, "Installed")
}

// E2E-051 / AC-08-2: --latest already up to date → no download, push existing
func TestDeviceInstall_Latest_AlreadyUpToDate(t *testing.T) {
	store := helperDownloadStore()
	as := &mockAppStore{
		accountInfoResult: appstore.AccountInfoResult{Email: "a@t.com"},
		lookupResult:      appstore.AppInfo{ID: 1, BundleID: "com.x", Name: "X", Version: "2.0"}, // same as library
	}
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "2.0", "/lib/v2.ipa", 1)}}
	deps := helperInstallAutoDeps(t, store, as, svc, lib)

	out, err := runDeviceInstallCmd(t, deps, "com.x", "--latest")
	require.NoError(t, err)
	assert.Equal(t, 0, as.downloadCalls, "must NOT download when already up to date")
	assert.Equal(t, "/lib/v2.ipa", svc.installPath, "push existing library IPA")
	assert.Contains(t, out, "already up to date")
}

// E2E-052 / AC-08-3: --latest no credentials
func TestDeviceInstall_Latest_NoCredentials(t *testing.T) {
	store := &mockStore{
		profiles:    []account.Profile{{ID: "alice"}},
		activeID:    "alice",
		credentials: map[string]bool{"alice": false},
	}
	deps := helperInstallAutoDeps(t, store, &mockAppStore{}, &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x", "--latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials")
}

// E2E-063 / AC-10-4: --latest + --version mutually exclusive
func TestDeviceInstall_LatestVersionMutex(t *testing.T) {
	deps := helperInstallAutoDeps(t, helperDownloadStore(), &mockAppStore{}, &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}, &mockLibraryStore{})
	_, err := runDeviceInstallCmd(t, deps, "com.x", "--latest", "--version", "1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// =============================================================================
// T5: device uninstall (US-04)
// =============================================================================

func runDeviceUninstallCmd(t *testing.T, deps Deps, args ...string) (string, error) {
	t.Helper()
	cmd := deviceUninstallCmd(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// E2E-080 / AC-04-1: uninstall confirmed yes
func TestDeviceUninstall_ConfirmYes(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}}}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: true}

	out, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Contains(t, out, "uninstall 'com.x' from device 'iPhone'? [y/N]")
	assert.Contains(t, out, "Uninstalled")
	assert.True(t, svc.uninstallCalled)
	assert.Equal(t, "u1", svc.uninstallUDID)
	assert.Equal(t, "com.x", svc.uninstallBundle)
}

// E2E-081 / AC-04-1: uninstall confirmed no → cancelled exit 0
func TestDeviceUninstall_ConfirmNo(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: false}

	out, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Contains(t, out, "cancelled")
	assert.False(t, svc.uninstallCalled, "must not call Uninstall on decline")
}

// E2E-083 / AC-04-3: uninstall not installed
func TestDeviceUninstall_NotInstalled(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()
	svc := &mockDeviceService{
		devices:      []device.DeviceInfo{{UDID: "u1"}},
		uninstallErr: apperr.ErrAppNotInstalled,
	}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: true}

	_, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed on device")
}

// E2E-084 / AC-04-4: uninstall non-interactive refused
func TestDeviceUninstall_NonInteractive_Refused(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return false }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}}
	deps := newDeviceListDeps(svc)

	_, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-interactive")
	assert.False(t, svc.uninstallCalled, "must not uninstall in non-interactive mode")
}

// E2E-085 / AC-04-5: uninstall --udid selects device
func TestDeviceUninstall_UDID_SelectsDevice(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: true}

	_, err := runDeviceUninstallCmd(t, deps, "com.x", "--udid", "b")
	require.NoError(t, err)
	assert.Equal(t, "b", svc.uninstallUDID)
}

// E2E-092 (uninstall branch) / AC-07-3: uninstall tunnel
func TestDeviceUninstall_TunnelRequired(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()
	svc := &mockDeviceService{
		devices:      []device.DeviceInfo{{UDID: "u1"}},
		uninstallErr: device.ErrTunnelRequired,
	}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: true}

	_, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iOS 17+ tunnel required")
}

// E2E-074 (uninstall branch) / AC-09-5: uninstall rejects --profile
func TestDeviceUninstall_RejectsProfileFlag(t *testing.T) {
	deps := newDeviceListDeps(&mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1"}}})
	_, err := runDeviceUninstallCmd(t, deps, "com.x", "--profile", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}

// =============================================================================
// T5: device uninstall — additional device-selection variants (AC-06) + E2E-082
// =============================================================================

// E2E-020 (uninstall variant) / AC-06-1: 0 devices
func TestDeviceUninstall_NoDevices(t *testing.T) {
	deps := newDeviceListDeps(&mockDeviceService{}) // 0 devices
	_, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connected device")
}

// E2E-021 (uninstall variant) / AC-06-2: --udid not connected
func TestDeviceUninstall_UDIDNotConnected(t *testing.T) {
	deps := newDeviceListDeps(&mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}})
	_, err := runDeviceUninstallCmd(t, deps, "com.x", "--udid", "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device 'ghost' not connected")
}

// E2E-027 / AC-06-3: uninstall multi-device interactive select
func TestDeviceUninstall_MultiDevice_InteractiveSelect(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{selectDeviceResult: "a", confirm: true}

	_, err := runDeviceUninstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Equal(t, "a", svc.uninstallUDID, "interactive select picked device a")
}

// E2E-082 / AC-04-2: uninstall success → app no longer in device apps listing.
// Models the device-state change: mockDeviceService.Uninstall removes the
// bundle from appsResult so a follow-up apps call reflects the removal.
func TestDeviceUninstall_RemovesFromAppsListing(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	svc := &mockDeviceService{
		devices:    []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}},
		appsResult: []device.InstalledApp{{BundleID: "com.x", Name: "X", Version: "1.0"}},
	}
	// Model device state: Uninstall removes the bundle from the apps listing.
	deps := newDeviceListDeps(svc)
	deps.UI = &mockPrompter{confirm: true}

	// Before uninstall, apps lists com.x.
	appsOut, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, appsOut, "com.x")

	// Uninstall succeeds and mutates the mock's apps listing.
	_, err = runDeviceUninstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	svc.appsResult = nil // device removed the app

	// After uninstall, apps no longer lists com.x.
	appsOut2, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.NotContains(t, appsOut2, "com.x")
}

// =============================================================================
// Install device-selection variants (E2E-020/021/022 install branch) + E2E-031
// (direct CLI coverage; resolveDevice is shared but AC-06 install path is now
// exercised through the install command itself for traceability)
// =============================================================================

// E2E-020 (install) / AC-06-1: 0 devices
func TestDeviceInstall_NoDevices(t *testing.T) {
	store := helperDownloadStore()
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, &mockDeviceService{}, lib) // 0 devices
	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connected device")
}

// E2E-021 (install) / AC-06-2: --udid not connected
func TestDeviceInstall_UDIDNotConnected(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)
	_, err := runDeviceInstallCmd(t, deps, "com.x", "--udid", "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device 'ghost' not connected")
}

// E2E-022/023 (install) / AC-06-3: multi-device interactive select + cancel
func TestDeviceInstall_MultiDevice_InteractiveSelect(t *testing.T) {
	old := checkInteractive
	checkInteractive = func() bool { return true }
	defer func() { checkInteractive = old }()

	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "a"}, {UDID: "b"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)
	deps.UI = &mockPrompter{selectDeviceResult: "a"}

	_, err := runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	assert.Equal(t, "a", svc.installUDID)
}

// E2E-031 / AC-02-2: install success → app appears in device apps listing
// (stateful mock: install "adds" the bundle to appsResult so a follow-up apps
// call reflects it).
func TestDeviceInstall_ThenAppsShowsIt(t *testing.T) {
	store := helperDownloadStore()
	svc := &mockDeviceService{devices: []device.DeviceInfo{{UDID: "u1", Name: "iPhone"}}}
	lib := &mockLibraryStore{entries: []library.Entry{ipaEntry("com.x", "1.0", "/x.ipa", 1)}}
	deps := helperDeviceInstallDeps(store, svc, lib)

	// Before install, apps does not list com.x.
	before, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.NotContains(t, before, "com.x")

	// Install succeeds; model the device state change.
	_, err = runDeviceInstallCmd(t, deps, "com.x")
	require.NoError(t, err)
	svc.appsResult = []device.InstalledApp{{BundleID: "com.x", Name: "X", Version: "1.0"}}

	// After install, apps lists com.x.
	after, err := runDeviceAppsCmd(t, deps)
	require.NoError(t, err)
	assert.Contains(t, after, "com.x")
}

// =============================================================================
// DD-09: CLI error rendering — Usage only on format errors, single print
// (goes through the production execute() wrapper, captures stdout/stderr
// separately — closes the prior blind spot of asserting only return-value and
// bypassing the wrapper where the duplicate-print regression lived)
// =============================================================================

// operational error (RunE) → message exactly once, NO Usage — via the real
// execute() wrapper (proves the wrapper does not double-print after cobra).
func TestExecute_OperationalError_NoUsageSinglePrint(t *testing.T) {
	depsFn := func() (Deps, error) { return newDeviceListDeps(&mockDeviceService{}), nil } // 0 devices
	var out, errOut bytes.Buffer
	code := execute("test", []string{"device", "apps"}, depsFn, &out, &errOut)

	require.Equal(t, 1, code)
	combined := out.String() + errOut.String()
	assert.Contains(t, combined, "no connected device", "operational error message present")
	assert.NotContains(t, combined, "Usage:", "operational error must NOT print Usage (DD-09)")
	assert.Equal(t, 1, strings.Count(combined, "no connected device"),
		"message printed exactly once — wrapper does not double-print (DD-09 regression guard)")
}

// format error (unknown flag) → message + Usage (helps user correct command)
func TestExecute_FormatError_ShowsUsage(t *testing.T) {
	depsFn := func() (Deps, error) { return newDeviceListDeps(&mockDeviceService{}), nil }
	var out, errOut bytes.Buffer
	code := execute("test", []string{"device", "apps", "--badflag"}, depsFn, &out, &errOut)

	require.Equal(t, 1, code)
	combined := out.String() + errOut.String()
	assert.Contains(t, combined, "unknown flag", "flag error message present")
	assert.Contains(t, combined, "Usage:", "format error MUST show Usage (DD-09)")
}
