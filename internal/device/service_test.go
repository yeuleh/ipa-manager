package device

import (
	"testing"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// mockBackend is a test Backend.
type mockBackend struct {
	entries       []ios.DeviceEntry
	lockdown      map[string]lockdownResult
	listErr       error
	openProxy     installationProxyMock
	openInstaller installerMock
}

type installationProxyMock struct {
	conn *mockProxyConn
	err  error
}

type installerMock struct {
	conn *mockInstallerConn
	err  error
}

type mockProxyConn struct {
	apps            []installationproxy.AppInfo
	browseErr       error
	uninstallErr    error
	uninstallCalled bool
	uninstallBundle string
	closed          bool
}

func (c *mockProxyConn) BrowseUserApps() ([]installationproxy.AppInfo, error) {
	return c.apps, c.browseErr
}
func (c *mockProxyConn) Uninstall(bundleID string) error {
	c.uninstallCalled = true
	c.uninstallBundle = bundleID
	return c.uninstallErr
}
func (c *mockProxyConn) Close() { c.closed = true }

type mockInstallerConn struct {
	sendErr    error
	sendCalled bool
	sendPath   string
	closed     bool
}

func (c *mockInstallerConn) SendFile(path string) error {
	c.sendCalled = true
	c.sendPath = path
	return c.sendErr
}
func (c *mockInstallerConn) Close() error { c.closed = true; return nil }

type lockdownResult struct {
	name    string
	version string
	err     error
}

func (m *mockBackend) ListDeviceEntries() ([]ios.DeviceEntry, error) {
	return m.entries, m.listErr
}
func (m *mockBackend) GetDeviceEntry(udid string) (ios.DeviceEntry, error) {
	for _, e := range m.entries {
		if e.Properties.SerialNumber == udid {
			return e, nil
		}
	}
	return ios.DeviceEntry{}, errDeviceNotFound
}
func (m *mockBackend) GetLockdownInfo(entry ios.DeviceEntry) (string, string, error) {
	r, ok := m.lockdown[entry.Properties.SerialNumber]
	if !ok {
		return "", "", nil
	}
	return r.name, r.version, r.err
}
func (m *mockBackend) OpenInstallationProxy(ios.DeviceEntry) (ProxyConn, error) {
	if m.openProxy.err != nil {
		return nil, m.openProxy.err
	}
	return m.openProxy.conn, nil
}
func (m *mockBackend) OpenInstaller(ios.DeviceEntry) (InstallerConn, error) {
	if m.openInstaller.err != nil {
		return nil, m.openInstaller.err
	}
	return m.openInstaller.conn, nil
}

func entry(udid, connType string) ios.DeviceEntry {
	return ios.DeviceEntry{Properties: ios.DeviceProperties{SerialNumber: udid, ConnectionType: connType}}
}

func appInfo(bundleID, name, version string) installationproxy.AppInfo {
	return installationproxy.AppInfo{
		"CFBundleIdentifier":         bundleID,
		"CFBundleName":               name,
		"CFBundleShortVersionString": version,
	}
}

// test errors
var (
	errDeviceNotFound    = newErr("device not found")
	errConnectFailed     = newErr("connect failed")
	errBrowseFailed      = newErr("browse failed")
	errSendFailed        = newErr("send failed")
	errUsbmuxdDown       = newErr("usbmuxd unavailable")
	errLockdownUntrusted = newErr("lockdown failed: device not trusted")
)

func newErr(msg string) error { return &simpleError{msg: msg} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

// =============================================================================
// ListConnected (AC-01-1)
// =============================================================================

func TestListConnected_LockdownSuccess_PopulatesDeviceInfo(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone 15", version: "16.5"}},
	}
	devices, err := NewService(backend).ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 1)
	d := devices[0]
	assert.Equal(t, "u1", d.UDID)
	assert.Equal(t, "iPhone 15", d.Name)
	assert.Equal(t, "16.5", d.IOSVersion)
	assert.Equal(t, "USB", d.ConnectionType)
}

func TestListConnected_LockdownFailure_StillListsDevice(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u2", "Network")},
		lockdown: map[string]lockdownResult{"u2": {err: errLockdownUntrusted}},
	}
	devices, err := NewService(backend).ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 1)
	d := devices[0]
	assert.Equal(t, "u2", d.UDID)
	assert.Equal(t, "Network", d.ConnectionType)
	assert.Empty(t, d.Name)
	assert.Empty(t, d.IOSVersion)
}

func TestListConnected_MultipleDevices_AllListed(t *testing.T) {
	backend := &mockBackend{
		entries: []ios.DeviceEntry{entry("a", "USB"), entry("b", "Network")},
		lockdown: map[string]lockdownResult{
			"a": {name: "A", version: "16.0"},
			"b": {err: errLockdownUntrusted},
		},
	}
	devices, err := NewService(backend).ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 2)
	assert.ElementsMatch(t, []string{"a", "b"}, []string{devices[0].UDID, devices[1].UDID})
}

func TestListConnected_ListError_Propagates(t *testing.T) {
	_, err := NewService(&mockBackend{listErr: errUsbmuxdDown}).ListConnected()
	require.Error(t, err)
}

// =============================================================================
// ListInstalledApps
// =============================================================================

func TestListInstalledApps_Happy_MapsToInstalledApp(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openProxy: installationProxyMock{conn: &mockProxyConn{apps: []installationproxy.AppInfo{
			appInfo("com.x", "X", "1.0"), appInfo("com.y", "Y", "2.0"),
		}}},
	}
	apps, err := NewService(backend).ListInstalledApps("u1")
	require.NoError(t, err)
	require.Len(t, apps, 2)
	assert.Equal(t, "com.x", apps[0].BundleID)
	assert.True(t, backend.openProxy.conn.closed)
}

func TestListInstalledApps_ConnectFail_RawError(t *testing.T) {
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		openProxy: installationProxyMock{err: errConnectFailed},
	}
	_, err := NewService(backend).ListInstalledApps("u1")
	require.Error(t, err)
	assert.Equal(t, errConnectFailed, err, "connect failure surfaces raw")
}

func TestListInstalledApps_GetDeviceNotFound_HardError(t *testing.T) {
	_, err := NewService(&mockBackend{entries: []ios.DeviceEntry{entry("u1", "USB")}}).ListInstalledApps("ghost")
	require.Error(t, err)
}

// =============================================================================
// Install
// =============================================================================

func TestInstall_Happy_PushesIPA(t *testing.T) {
	conn := &mockInstallerConn{}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		openInstaller: installerMock{conn: conn},
	}
	require.NoError(t, NewService(backend).Install("u1", "/path/app.ipa"))
	assert.True(t, conn.sendCalled)
	assert.Equal(t, "/path/app.ipa", conn.sendPath)
	assert.True(t, conn.closed)
}

func TestInstall_ConnectFail_RawError(t *testing.T) {
	conn := &mockInstallerConn{}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		openInstaller: installerMock{err: errConnectFailed},
	}
	err := NewService(backend).Install("u1", "/x.ipa")
	require.Error(t, err)
	assert.Equal(t, errConnectFailed, err, "connect failure surfaces raw")
	assert.False(t, conn.sendCalled, "SendFile not called on connect failure")
}

func TestInstall_OperateError_Raw(t *testing.T) {
	conn := &mockInstallerConn{sendErr: errSendFailed}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		openInstaller: installerMock{conn: conn},
	}
	err := NewService(backend).Install("u1", "/x.ipa")
	require.Error(t, err)
	assert.Equal(t, errSendFailed, err)
}

// =============================================================================
// Uninstall
// =============================================================================

func TestUninstall_Happy(t *testing.T) {
	conn := &mockProxyConn{apps: []installationproxy.AppInfo{appInfo("com.x", "X", "1.0")}}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		openProxy: installationProxyMock{conn: conn},
	}
	require.NoError(t, NewService(backend).Uninstall("u1", "com.x"))
	assert.True(t, conn.uninstallCalled)
	assert.Equal(t, "com.x", conn.uninstallBundle)
}

func TestUninstall_NotInstalled_ErrAppNotInstalled(t *testing.T) {
	// Pre-check: bundle absent → ErrAppNotInstalled, Uninstall not called.
	conn := &mockProxyConn{apps: []installationproxy.AppInfo{appInfo("com.other", "Other", "1.0")}}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		openProxy: installationProxyMock{conn: conn},
	}
	err := NewService(backend).Uninstall("u1", "com.x")
	require.ErrorIs(t, err, apperr.ErrAppNotInstalled)
	assert.False(t, conn.uninstallCalled)
}

func TestUninstall_OperateError_Raw(t *testing.T) {
	conn := &mockProxyConn{
		apps:         []installationproxy.AppInfo{appInfo("com.x", "X", "1.0")},
		uninstallErr: errSendFailed,
	}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		openProxy: installationProxyMock{conn: conn},
	}
	err := NewService(backend).Uninstall("u1", "com.x")
	require.Error(t, err)
	require.NotErrorIs(t, err, apperr.ErrAppNotInstalled)
}
