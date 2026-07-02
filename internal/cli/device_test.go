package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/device"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// mockDeviceService implements device.Service for CLI tests. Extended in
// later tasks with Install/Uninstall.
type mockDeviceService struct {
	devices    []device.DeviceInfo
	listErr    error
	listCalls  int
	appsResult []device.InstalledApp
	appsErr    error
	appsCalls  int
	appsUDID   string // records udid passed to ListInstalledApps
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
