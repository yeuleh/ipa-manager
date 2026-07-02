package device

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// mockBackend is a test Backend. It returns canned device entries and a
// per-UDID map of lockdown results.
type mockBackend struct {
	entries            []ios.DeviceEntry
	lockdown           map[string]lockdownResult // keyed by UDID (SerialNumber)
	listErr            error
	openProxy          installationProxyMock // T2: OpenInstallationProxy behavior
	openInstaller      installerMock         // T3: OpenInstaller behavior
	lookupTunnel       tunnelLookupMock      // T3: LookupTunnelInfo behavior
	withRsdEntry       ios.DeviceEntry       // T3: WithRsd return value
	withRsdCalled      bool
	openInstallerEntry ios.DeviceEntry // T3: records entry passed to OpenInstaller
}

type installationProxyMock struct {
	conn *mockProxyConn
	err  error // connect-stage error
}

type installerMock struct {
	conn *mockInstallerConn
	err  error // connect-stage error
}

type tunnelLookupMock struct {
	addr string
	port int
	err  error
}

// mockProxyConn implements ProxyConn for tests.
type mockProxyConn struct {
	apps            []installationproxy.AppInfo
	browseErr       error
	closed          bool
	uninstallErr    error
	uninstallCalled bool
	uninstallBundle string
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

// mockInstallerConn implements InstallerConn for tests.
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
func (m *mockBackend) OpenInstaller(entry ios.DeviceEntry) (InstallerConn, error) {
	m.openInstallerEntry = entry
	if m.openInstaller.err != nil {
		return nil, m.openInstaller.err
	}
	return m.openInstaller.conn, nil
}
func (m *mockBackend) LookupTunnelInfo(string) (string, int, error) {
	return m.lookupTunnel.addr, m.lookupTunnel.port, m.lookupTunnel.err
}
func (m *mockBackend) WithRsd(entry ios.DeviceEntry, _, _ string, _ int) (ios.DeviceEntry, error) {
	m.withRsdCalled = true
	return m.withRsdEntry, nil
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
		"CFBundleIdentifier":         bundleID,
		"CFBundleName":               name,
		"CFBundleShortVersionString": version,
	}
}

func TestListInstalledApps_Happy_MapsToInstalledApp(t *testing.T) {
	backend := &mockBackend{
		entries:  []ios.DeviceEntry{entry("u1", "USB")},
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
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{err: errConnectFailed},
	}
	svc := NewService(backend)

	_, err := svc.ListInstalledApps("u1")
	require.ErrorIs(t, err, ErrTunnelRequired, "iOS 17+ connect failure → tunnel")
}

func TestListInstalledApps_ConnectFail_iOS16_RawError(t *testing.T) {
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
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
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
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

// =============================================================================
// Install (AC-02-9 push; DD-02 connect/operate layering + tunnel reuse)
// =============================================================================

func TestInstall_Happy_PushesIPA(t *testing.T) {
	conn := &mockInstallerConn{}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:      map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openInstaller: installerMock{conn: conn},
	}
	svc := NewService(backend)

	err := svc.Install("u1", "/path/app.ipa")
	require.NoError(t, err)
	assert.True(t, conn.sendCalled, "SendFile must be called")
	assert.Equal(t, "/path/app.ipa", conn.sendPath)
	assert.True(t, conn.closed, "conn must be closed")
}

func TestInstall_ConnectFail_iOS17_NoTunnel_Tunnel(t *testing.T) {
	conn := &mockInstallerConn{} // should NOT be used
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:      map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openInstaller: installerMock{err: errConnectFailed},
		lookupTunnel:  tunnelLookupMock{err: errTunnelMissing}, // no tunnel
	}
	svc := NewService(backend)

	err := svc.Install("u1", "/path/app.ipa")
	require.ErrorIs(t, err, ErrTunnelRequired, "iOS 17+ connect failure without tunnel → ErrTunnelRequired")
	assert.False(t, conn.sendCalled, "SendFile must NOT be called on connect failure")
}

func TestInstall_ConnectFail_iOS16_RawError(t *testing.T) {
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:      map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openInstaller: installerMock{err: errConnectFailed},
	}
	svc := NewService(backend)

	err := svc.Install("u1", "/path/app.ipa")
	require.NotErrorIs(t, err, ErrTunnelRequired, "iOS 16 connect failure must NOT be tunnel")
	assert.Equal(t, errConnectFailed, err)
}

func TestInstall_OperateError_NotTunnel(t *testing.T) {
	// operate-stage (SendFile) error on iOS 17+ → raw, never tunnel.
	conn := &mockInstallerConn{sendErr: errSendFailed}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:      map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openInstaller: installerMock{conn: conn},
	}
	svc := NewService(backend)

	err := svc.Install("u1", "/path/app.ipa")
	require.NotErrorIs(t, err, ErrTunnelRequired, "operate error must never be tunnel")
	assert.Equal(t, errSendFailed, err)
}

func TestInstall_iOS17_TunnelReused_RoutesViaShim(t *testing.T) {
	// E2E-093d: iOS 17+ with a running tunnel → LookupTunnelInfo succeeds →
	// WithRsd injects → OpenInstaller succeeds → SendFile succeeds.
	rsdEntry := entry("u1", "USB")
	rsdEntry.Rsd = stubRsdProvider{} // non-nil so SupportsRsd() would be true
	conn := &mockInstallerConn{}
	backend := &mockBackend{
		entries:       []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:      map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openInstaller: installerMock{conn: conn},
		lookupTunnel:  tunnelLookupMock{addr: "::1", port: 12345},
		withRsdEntry:  rsdEntry,
	}
	svc := NewService(backend)

	err := svc.Install("u1", "/path/app.ipa")
	require.NoError(t, err)
	assert.True(t, backend.withRsdCalled, "WithRsd must be called when tunnel info available")
	assert.True(t, conn.sendCalled, "SendFile must succeed via shim path")
	assert.NotNil(t, backend.openInstallerEntry.Rsd, "OpenInstaller must receive the RSD-injected entry (core tunnel-reuse invariant)")
}

func TestInstall_GetDeviceNotFound_HardError(t *testing.T) {
	backend := &mockBackend{entries: []ios.DeviceEntry{entry("u1", "USB")}}
	svc := NewService(backend)
	err := svc.Install("ghost", "/x.ipa") // resolveEntry → GetDeviceEntry not found
	require.Error(t, err)
}

// stubRsdProvider is a no-op RsdPortProvider for test DeviceEntry.Rsd.
type stubRsdProvider struct{}

func (stubRsdProvider) GetPort(string) int                          { return 0 }
func (stubRsdProvider) GetService(int) string                       { return "" }
func (stubRsdProvider) GetServices() map[string]ios.RsdServiceEntry { return nil }

// additional test errors
var (
	errSendFailed    = newErr("send failed")
	errTunnelMissing = newErr("no tunnel")
)

// =============================================================================
// LookupTunnelInfo (defaultBackend) — direct HTTP test via httptest + env
// =============================================================================

func TestLookupTunnelInfo_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/tunnel/u1", r.URL.Path)
		fmt.Fprintf(w, `{"address":"::1","rsdPort":12345,"udid":"u1"}`)
	}))
	defer srv.Close()
	host, port := splitTestURL(t, srv.URL)
	withEnv(t, map[string]string{"GO_IOS_AGENT_HOST": host, "GO_IOS_AGENT_PORT": port}, func() {
		addr, rsdPort, err := (defaultBackend{}).LookupTunnelInfo("u1")
		require.NoError(t, err)
		assert.Equal(t, "::1", addr)
		assert.Equal(t, 12345, rsdPort)
	})
}

func TestLookupTunnelInfo_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	host, port := splitTestURL(t, srv.URL)
	withEnv(t, map[string]string{"GO_IOS_AGENT_HOST": host, "GO_IOS_AGENT_PORT": port}, func() {
		_, _, err := (defaultBackend{}).LookupTunnelInfo("u1")
		require.Error(t, err)
		assert.ErrorIs(t, err, errTunnelNotFound, "404 → errTunnelNotFound")
	})
}

func TestLookupTunnelInfo_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	host, port := splitTestURL(t, srv.URL)
	withEnv(t, map[string]string{"GO_IOS_AGENT_HOST": host, "GO_IOS_AGENT_PORT": port}, func() {
		_, _, err := (defaultBackend{}).LookupTunnelInfo("u1")
		require.Error(t, err)
		assert.NotErrorIs(t, err, errTunnelNotFound, "500 is not 'not found'")
	})
}

func TestLookupTunnelInfo_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `not json`)
	}))
	defer srv.Close()
	host, port := splitTestURL(t, srv.URL)
	withEnv(t, map[string]string{"GO_IOS_AGENT_HOST": host, "GO_IOS_AGENT_PORT": port}, func() {
		_, _, err := (defaultBackend{}).LookupTunnelInfo("u1")
		require.Error(t, err)
	})
}

// withEnv sets env vars for the duration of fn, restoring afterwards.
func withEnv(t *testing.T, vars map[string]string, fn func()) {
	t.Helper()
	old := map[string]string{}
	for k := range vars {
		old[k] = os.Getenv(k)
		os.Setenv(k, vars[k])
	}
	defer func() {
		for k, v := range old {
			os.Setenv(k, v)
		}
	}()
	fn()
}

// splitTestURL splits "http://127.0.0.1:PORT" into host/port-string.
func splitTestURL(t *testing.T, url string) (host, port string) {
	t.Helper()
	u := strings.TrimPrefix(url, "http://")
	host, port, ok := strings.Cut(u, ":")
	require.True(t, ok, "test url must have a port")
	return host, port
}

// =============================================================================
// Uninstall (AC-04-3; DD-02 connect/operate layering)
// =============================================================================

func TestUninstall_Happy(t *testing.T) {
	conn := &mockProxyConn{}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "16.0"}},
		openProxy: installationProxyMock{conn: conn},
	}
	svc := NewService(backend)

	err := svc.Uninstall("u1", "com.x")
	require.NoError(t, err)
	assert.True(t, conn.uninstallCalled)
	assert.Equal(t, "com.x", conn.uninstallBundle)
	assert.True(t, conn.closed)
}

func TestUninstall_NotInstalled_ErrAppNotInstalled(t *testing.T) {
	conn := &mockProxyConn{uninstallErr: newErr("app not installed on device")}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{conn: conn},
	}
	svc := NewService(backend)

	err := svc.Uninstall("u1", "com.x")
	require.ErrorIs(t, err, apperr.ErrAppNotInstalled, "operate 'not installed' → ErrAppNotInstalled")
}

func TestUninstall_ConnectFail_iOS17_Tunnel(t *testing.T) {
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{err: errConnectFailed},
	}
	svc := NewService(backend)

	err := svc.Uninstall("u1", "com.x")
	require.ErrorIs(t, err, ErrTunnelRequired)
}

func TestUninstall_OperateOtherError_RawNotTunnel(t *testing.T) {
	conn := &mockProxyConn{uninstallErr: newErr("some device error")}
	backend := &mockBackend{
		entries:   []ios.DeviceEntry{entry("u1", "USB")},
		lockdown:  map[string]lockdownResult{"u1": {name: "iPhone", version: "17.5"}},
		openProxy: installationProxyMock{conn: conn},
	}
	svc := NewService(backend)

	err := svc.Uninstall("u1", "com.x")
	require.NotErrorIs(t, err, ErrTunnelRequired, "operate error never tunnel")
	require.NotErrorIs(t, err, apperr.ErrAppNotInstalled, "non-not-installed error is raw")
	assert.Contains(t, err.Error(), "some device error")
}
