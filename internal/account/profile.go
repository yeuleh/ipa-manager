// Package account manages Apple account profiles and the per-profile
// credential isolation that is this project's core value.
package account

// Profile is a named Apple account configuration.
type Profile struct {
	// ID is the stable slug used in keychain/cookie-jar paths.
	ID string `json:"id"`
	// Name is a human-friendly label.
	Name string `json:"name"`
	// Email is the Apple ID email.
	Email string `json:"email"`
	// StoreFront is the Apple store front code (populated after login).
	StoreFront string `json:"store_front,omitempty"`
}
