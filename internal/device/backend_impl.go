package device

import (
	"github.com/danielpaulus/go-ios/ios"
)

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
