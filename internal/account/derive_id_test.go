package account

import "testing"

func TestDeriveProfileID(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"alice@example.com", "alice_example_com"},
		{"Bob@Example.Com", "bob_example_com"},
		{"a.b@c.d.e", "a_b_c_d_e"},
		{"user+tag@domain.org", "user_tag_domain_org"},
		{"already_id-styled@x.io", "already_id-styled_x_io"},
		{"UPPER@CASE.COM", "upper_case_com"},
		{"multi..dot@x.com", "multi__dot_x_com"},
		// Edge cases
		{"", ""},
		{"a", "a"},
		{"@", "_"},
		{"a@b", "a_b"},
		{"a-b@c.com", "a-b_c_com"},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := DeriveProfileID(tt.email)
			if got != tt.want {
				t.Errorf("DeriveProfileID(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}
