package cli

import (
	"errors"
	"fmt"

	"github.com/yeuleh/ipa-manager/internal/apperr"
	"github.com/yeuleh/ipa-manager/internal/device"
	"github.com/yeuleh/ipa-manager/internal/ui"
)

// resolveDevice resolves the target device for a service command (AC-06-1~5).
// Returns the chosen DeviceInfo (UDID for Service calls, Name for messages).
//
//	0 devices            → ErrDeviceNotConnected ("no connected device; connect a device and trust this Mac")
//	--udid not connected → "device '<id>' not connected"
//	exactly 1 device     → auto-selected (no prompt)
//	multiple + TTY       → interactive SelectDevice (cancel → ErrCancelled)
//	multiple + non-TTY   → ErrMultipleDevices ("multiple devices connected; specify --udid (non-interactive mode)")
func resolveDevice(deps Deps, udidFlag string) (device.DeviceInfo, error) {
	devices, err := deps.DeviceService.ListConnected()
	if err != nil {
		return device.DeviceInfo{}, fmt.Errorf("failed to list devices: %w", err)
	}
	if len(devices) == 0 {
		return device.DeviceInfo{}, fmt.Errorf("%w; connect a device and trust this Mac", apperr.ErrDeviceNotConnected)
	}
	if udidFlag != "" {
		for _, d := range devices {
			if d.UDID == udidFlag {
				return d, nil
			}
		}
		return device.DeviceInfo{}, fmt.Errorf("device '%s' not connected", udidFlag)
	}
	if len(devices) == 1 {
		return devices[0], nil // auto-select
	}
	// multiple devices, no --udid
	if !checkInteractive() {
		return device.DeviceInfo{}, fmt.Errorf("%w; specify --udid (non-interactive mode)", apperr.ErrMultipleDevices)
	}
	options := make([]ui.DeviceOption, len(devices))
	for i, d := range devices {
		options[i] = ui.DeviceOption{UDID: d.UDID, Label: deviceLabel(d)}
	}
	selected, err := deps.UI.SelectDevice(options)
	if err != nil {
		// ErrCancelled (user abort) propagated for caller to map to "cancelled" exit 0.
		return device.DeviceInfo{}, err
	}
	for _, d := range devices {
		if d.UDID == selected {
			return d, nil
		}
	}
	return device.DeviceInfo{}, fmt.Errorf("selected device not found: %s", selected)
}

// deviceLabel formats a DeviceInfo as a human-readable SelectDevice option.
func deviceLabel(d device.DeviceInfo) string {
	name := d.Name
	if name == "" {
		name = d.UDID // fall back to UDID when lockdown name unavailable
	}
	if d.IOSVersion != "" {
		return fmt.Sprintf("%s (iOS %s) — %s", name, d.IOSVersion, shortUDID(d.UDID))
	}
	return fmt.Sprintf("%s — %s", name, shortUDID(d.UDID))
}

// shortUDID returns a shortened UDID for display (first 8 chars + …).
func shortUDID(udid string) string {
	if len(udid) <= 8 {
		return udid
	}
	return udid[:8] + "…"
}

// isCancelled reports whether err is a user-cancel (apperr.ErrCancelled).
func isCancelled(err error) bool { return errors.Is(err, apperr.ErrCancelled) }
