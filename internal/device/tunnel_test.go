package device

import "testing"

func TestIsIOS17OrLater(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"17", true},
		{"17.5", true},
		{"18.0", true},
		{"20", true},
		{"16.6", false},
		{"16", false},
		{"10.3", false},
		{"", false},        // empty → false (honest degradation)
		{"  17.0  ", true}, // trimmed
		{"abc", false},     // unparseable → false
		{"17a", false},     // unparseable major → false
	}
	for _, c := range cases {
		got := isIOS17OrLater(c.version)
		if got != c.want {
			t.Errorf("isIOS17OrLater(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}
