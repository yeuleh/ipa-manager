// Package device adapts go-ios device operations (install / list / uninstall)
// and surfaces iOS 17+ tunnel requirements as actionable errors.
package device

import (
	"github.com/danielpaulus/go-ios/ios"
	"github.com/yeuleh/ipa-manager/internal/apperr"
)

// ListConnectedDevices returns connected iOS devices.
//
// TODO(mission): wrap ios.ListDevices and, on connection failure for an
// iOS 17+ (SupportsRsd) device without a tunnel, return ErrTunnelRequired.
func ListConnectedDevices() ([]ios.DeviceEntry, error) {
	return nil, apperr.ErrNotImplemented
}
