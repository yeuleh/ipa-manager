package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/danielpaulus/go-ios/ios/zipconduit"
)

// errTunnelNotFound indicates no tunnel agent is running for the device (the
// agent returned 404). This is the expected state on iOS 17+ until the user
// runs `sudo ios tunnel start`; the Service surfaces it as ErrTunnelRequired.
var errTunnelNotFound = errors.New("no tunnel running for device")

// defaultBackend is the production Backend that calls real go-ios.
type defaultBackend struct{}

// NewDefaultBackend returns a Backend backed by real go-ios device operations.
func NewDefaultBackend() Backend {
	return defaultBackend{}
}

// ListDeviceEntries enumerates connected devices via usbmuxd.
func (defaultBackend) ListDeviceEntries() ([]ios.DeviceEntry, error) {
	list, err := ios.ListDevices()
	if err != nil {
		return nil, err
	}
	return list.DeviceList, nil
}

// GetDeviceEntry resolves a device entry by UDID.
func (defaultBackend) GetDeviceEntry(udid string) (ios.DeviceEntry, error) {
	return ios.GetDevice(udid)
}

// GetLockdownInfo reads DeviceName + ProductVersion from the device via
// lockdown (requires the device to be paired/trusted with this Mac).
func (defaultBackend) GetLockdownInfo(entry ios.DeviceEntry) (name, version string, err error) {
	resp, err := ios.GetValues(entry)
	if err != nil {
		return "", "", err
	}
	return resp.Value.DeviceName, resp.Value.ProductVersion, nil
}

// OpenInstallationProxy opens the installation_proxy service over usbmuxd.
func (defaultBackend) OpenInstallationProxy(entry ios.DeviceEntry) (ProxyConn, error) {
	return installationproxy.New(entry)
}

// OpenInstaller opens the zipconduit service for IPA install.
func (defaultBackend) OpenInstaller(entry ios.DeviceEntry) (InstallerConn, error) {
	return zipconduit.New(entry)
}

// LookupTunnelInfo queries a running tunnel agent via read-only HTTP GET
// (default 127.0.0.1:60105, as exposed by `ios tunnel start`). Reimplemented
// locally rather than importing go-ios's ios/tunnel package, which would pull
// in heavy TUN-interface deps (gvisor/quic-go) just for this one HTTP call.
// Returns errTunnelNotFound (→ Service maps to ErrTunnelRequired) when no
// tunnel agent is running for the device. No sudo.
func (defaultBackend) LookupTunnelInfo(udid string) (string, int, error) {
	url := fmt.Sprintf("http://%s/tunnel/%s",
		net.JoinHostPort(ios.HttpApiHost(), fmt.Sprintf("%d", ios.HttpApiPort())), udid)
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return "", 0, fmt.Errorf("tunnel info query failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", 0, errTunnelNotFound
	}
	if res.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("tunnel info: unexpected status %d", res.StatusCode)
	}
	var info struct {
		Address string `json:"address"`
		RsdPort int    `json:"rsdPort"`
	}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return "", 0, fmt.Errorf("tunnel info parse: %w", err)
	}
	return info.Address, info.RsdPort, nil
}

// WithRsd injects the RSD provider from a running tunnel into the device entry
// so OpenInstaller routes via the shim path. Mirrors go-ios deviceWithRsdProvider.
func (defaultBackend) WithRsd(entry ios.DeviceEntry, udid, address string, rsdPort int) (ios.DeviceEntry, error) {
	rsdService, err := ios.NewWithAddrPortDevice(address, rsdPort, entry)
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("connect to RSD %s:%d: %w", address, rsdPort, err)
	}
	defer rsdService.Close()
	rsdProvider, err := rsdService.Handshake()
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("RSD handshake: %w", err)
	}
	return ios.GetDeviceWithAddress(udid, address, rsdProvider)
}
