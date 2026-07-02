package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/device"
)

// mockDeviceService implements device.Service for CLI tests. Extended in
// later tasks with ListInstalledApps/Install/Uninstall.
type mockDeviceService struct {
	devices   []device.DeviceInfo
	listErr   error
	listCalls int
}

func (m *mockDeviceService) ListConnected() ([]device.DeviceInfo, error) {
	m.listCalls++
	return m.devices, m.listErr
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
