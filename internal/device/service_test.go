package device

import (
	"testing"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBackend is a test Backend. It returns canned device entries and a
// per-UDID map of lockdown results.
type mockBackend struct {
	entries     []ios.DeviceEntry
	lockdown    map[string]lockdownResult // keyed by UDID (SerialNumber)
	listErr     error
	openProxy   installationProxyMock // T2: OpenInstallationProxy behavior
	openProxyID string                // records UDID passed to GetDeviceEntry
}

type installationProxyMock struct {
	conn *mockProxyConn
	err  error // connect-stage error
}

// mockProxyConn implements ProxyConn for tests.
type mockProxyConn struct {
	apps      []installationproxy.AppInfo
	browseErr error
	closed    bool
}

func (c *mockProxyConn) BrowseUserApps() ([]installationproxy.AppInfo, error) {
	return c.apps, c.browseErr
}
func (c *mockProxyConn) Close() { c.closed = true }

type lockdownResult struct {
	name    string
	version string
	err     error
}

func (m *mockBackend) ListDeviceEntries() ([]ios.DeviceEntry, error) {
	return m.entries, m.listErr
}
func (m *mockBackend) GetDeviceEntry(udid string) (ios.DeviceEntry, error) {
	m.openProxyID = udid
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
		return "", "", nil // no entry → empty (treated as unavailable)
	}
	return r.name, r.version, r.err
}
func (m *mockBackend) OpenInstallationProxy(ios.DeviceEntry) (ProxyConn, error) {
	if m.openProxy.err != nil {
		return nil, m.openProxy.err
	}
	return m.openProxy.conn, nil
}

func entry(udid, connType string) ios.DeviceEntry {
	return ios.DeviceEntry{Properties: ios.DeviceProperties{SerialNumber: udid, ConnectionType: connType}}
}

// =============================================================================
// ListConnected (AC-01-1, AC-07-1)
// =============================================================================

func TestListConnected_LockdownSuccess_PopulatesDeviceInfo(t *testing.T) {
	backend := mockBackend{
		entries: []ios.DeviceEntry{entry("udid1", "USB")},
		lockdown: map[string]lockdownResult{
			"udid1": {name: "iPhone 15", version: "16.5"},
		},
	}
	svc := NewService(&backend)

	devices, err := svc.ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 1)
	d := devices[0]
	assert.Equal(t, "udid1", d.UDID)
	assert.Equal(t, "iPhone 15", d.Name)
	assert.Equal(t, "16.5", d.IOSVersion)
	assert.Equal(t, "USB", d.ConnectionType)
	assert.False(t, d.NeedsTunnel, "iOS 16 does not need tunnel")
}

func TestListConnected_iOS17_SetsNeedsTunnel(t *testing.T) {
	backend := mockBackend{
		entries: []ios.DeviceEntry{entry("udid17", "USB")},
		lockdown: map[string]lockdownResult{
			"udid17": {name: "iPhone 16", version: "17.5"},
		},
	}
	svc := NewService(&backend)

	devices, err := svc.ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.True(t, devices[0].NeedsTunnel, "iOS 17+ needs tunnel")
}

func TestListConnected_LockdownFailure_StillListsDevice(t *testing.T) {
	// AC-01-1 / AC-07-1: untrusted device (lockdown fails) is still listed via
	// usbmuxd; Name/IOSVersion empty, NeedsTunnel false (version unknown).
	backend := mockBackend{
		entries: []ios.DeviceEntry{entry("udid2", "Network")},
		lockdown: map[string]lockdownResult{
			"udid2": {err: errLockdownUntrusted},
		},
	}
	svc := NewService(&backend)

	devices, err := svc.ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 1)
	d := devices[0]
	assert.Equal(t, "udid2", d.UDID)
	assert.Equal(t, "Network", d.ConnectionType)
	assert.Empty(t, d.Name, "untrusted → name empty")
	assert.Empty(t, d.IOSVersion, "untrusted → version empty")
	assert.False(t, d.NeedsTunnel, "version unknown → NeedsTunnel false")
}

func TestListConnected_MultipleDevices_AllListed(t *testing.T) {
	backend := mockBackend{
		entries: []ios.DeviceEntry{
			entry("a", "USB"),
			entry("b", "Network"),
			entry("c", "USB"),
		},
		lockdown: map[string]lockdownResult{
			"a": {name: "A", version: "16.0"},
			"b": {err: errLockdownUntrusted},
			"c": {name: "C", version: "17.0"},
		},
	}
	svc := NewService(&backend)

	devices, err := svc.ListConnected()
	require.NoError(t, err)
	require.Len(t, devices, 3)
	udids := []string{devices[0].UDID, devices[1].UDID, devices[2].UDID}
	assert.ElementsMatch(t, []string{"a", "b", "c"}, udids)
}

func TestListConnected_ListError_Propagates(t *testing.T) {
	backend := mockBackend{listErr: errUsbmuxdDown}
	svc := NewService(&backend)

	_, err := svc.ListConnected()
	require.Error(t, err)
}

// test errors for mock realism
var (
	errLockdownUntrusted = newErr("lockdown failed: device not trusted")
	errUsbmuxdDown       = newErr("usbmuxd unavailable")
	errDeviceNotFound    = newErr("device not found")
)

func newErr(msg string) error { return &simpleError{msg: msg} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

// =============================================================================
// ListInstalledApps (AC-05-1; DD-02 connect/operate layering)
// =============================================================================

func appInfo(bundleID, name, version string) installationproxy.AppInfo {
	return installationproxy.AppInfo{
		"CFBundleIdentifier":            bundleID,
		"CFBundleName":                  name,
		"CFBundleShortVersionString":    version,
	}
}

func TestListInstalledApps_Happy_MapsToInstalledApp(t *testing.T) {
	backend := &mockBackend{
		entries: []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openProxy: installationProxyMock{conn: &mockProxyConn{apps: []installationproxy.AppInfo{
			appInfo("com.x", "X", "1.0"),
			appInfo("com.y", "Y", "2.0"),
		}}},
	}
	svc := NewService(backend)

	apps, err := svc.ListInstalledApps("u1")
	require.NoError(t, err)
	require.Len(t, apps, 2)
	assert.Equal(t, "com.x", apps[0].BundleID)
	assert.Equal(t, "X", apps[0].Name)
	assert.Equal(t, "1.0", apps[0].Version)
	assert.True(t, backend.openProxy.conn.closed, "ProxyConn must be closed")
}

func TestListInstalledApps_ConnectFail_iOS17_Tunnel(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{err: errConnectFailed},
	}
	svc := NewService(backend)

	_, err := svc.ListInstalledApps("u1")
	require.ErrorIs(t, err, ErrTunnelRequired, "iOS 17+ connect failure → tunnel")
}

func TestListInstalledApps_ConnectFail_iOS16_RawError(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openProxy: installationProxyMock{err: errConnectFailed},
	}
	svc := NewService(backend)

	_, err := svc.ListInstalledApps("u1")
	require.NotErrorIs(t, err, ErrTunnelRequired, "iOS 16 connect failure must NOT be tunnel")
	assert.Equal(t, errConnectFailed, err, "raw error surfaced")
}

func TestListInstalledApps_OperateError_NotTunnel(t *testing.T) {
	// operate-stage (Browse) error on iOS 17+ must surface raw, never tunnel.
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
		lockdown: map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{conn: &mockProxyConn{browseErr: errBrowseFailed}},
	}
	svc := NewService(backend)

	_, err := svc.ListInstalledApps("u1")
	require.NotErrorIs(t, err, ErrTunnelRequired, "operate error must never be tunnel")
	assert.Equal(t, errBrowseFailed, err)
}

func TestListInstalledApps_GetDeviceNotFound_HardError(t *testing.T) {
	backend := &mockBackend{entries: []ios.DeviceEntry{entry("u1", "USB")}}
	svc := NewService(backend)

	_, err := svc.ListInstalledApps("ghost")
	require.Error(t, err)
}

// additional test errors
var (
	errConnectFailed = newErr("connect failed")
	errBrowseFailed  = newErr("browse failed")
)
