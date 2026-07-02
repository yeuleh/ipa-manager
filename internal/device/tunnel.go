package device

import (
	"strconv"
	"strings"
)

// isIOS17OrLater reports whether the device iOS version is 17 or later,
// which determines whether a tunnel is required for service operations
// (install in particular; see design DD-02).
//
// Returns false for empty or unparseable versions: when the version is
// unknown (e.g. lockdown failed because the device is untrusted) we cannot
// determine tunnel need and degrade honestly rather than assuming it.
//
// Parse rule: TrimSpace → take the substring before the first "." → parse as
// integer (major version) → major >= 17. Examples: "17" true, "17.5" true,
// "18.0" true, "16.6" false, "" false, "abc" false.
func isIOS17OrLater(version string) bool {
	v := strings.TrimSpace(version)
	if v == "" {
		return false
	}
	majorStr := v
	if i := strings.Index(v, "."); i >= 0 {
		majorStr = v[:i]
	}
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return false
	}
	return major >= 17
}

// diagnoseConnectError maps a connect-stage failure to our semantics. It is
// ONLY applied to service-connection errors (Backend.Open*), never to
// operate-stage errors (SendFile/Browse/Uninstall).
//
// Rationale: GetLockdownInfo success implies the device is paired/trusted, so a
// connect-stage failure on iOS ≥17 is the missing tunnel (zipconduit/install
// path); the same connect failure on iOS <17 or when the version is unknown
// (untrusted) is surfaced raw (trust/usbmuxd issue), not assumed to be tunnel.
func diagnoseConnectError(err error, version string) error {
	if err == nil {
		return nil
	}
	if isIOS17OrLater(version) {
		return ErrTunnelRequired
	}
	return err
}
