package account

import "path/filepath"

// CookieJarPath returns the per-profile persistent cookie jar file path.
//
// Each profile MUST have its own cookie jar: ipatool's AppStore shares one
// CookieJar across all its HTTP clients, so without per-profile isolation
// login sessions cross-contaminate between accounts.
func CookieJarPath(profileID, configRoot string) string {
	return filepath.Join(configRoot, "profiles", profileID, "cookies")
}

// NewCookieJar constructs a persistent cookie jar for the profile.
//
// TODO(mission): return an ipatool http.CookieJar backed by
// github.com/juju/persistent-cookiejar at CookieJarPath(...).
func NewCookieJar(profileID, configRoot string) error {
	_ = CookieJarPath(profileID, configRoot)
	return nil
}
