package account

import "strings"

// DeriveProfileID converts an email address to a stable profile ID.
//
// Algorithm (requirements §4.1):
//  1. Lowercase the email.
//  2. Replace each rune that is NOT [a-z0-9_-] with '_'.
//
// Examples:
//   alice@example.com       → alice_example_com
//   Bob@Example.Com         → bob_example_com
//   user+tag@domain.org     → user_tag_domain_org
//   multi..dot@x.com        → multi__dot_x_com
//
// Collision policy: if two different emails derive to the same ID, auth login
// treats the second as a refresh of the first (requirements §4.1, R4).
func DeriveProfileID(email string) string {
	s := strings.ToLower(email)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
