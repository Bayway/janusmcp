package broker

import "testing"

// MCP protocol versions are compared as strings, so the routing predicate has to
// reject anything that is not a date before comparing: "9999" and "abc" both
// sort above "2026-07-28" and would otherwise be handed to the stateless
// handler, which cannot serve a legacy client.
func TestIsStatelessVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"", false},
		{"2024-11-05", false},
		{"2025-03-26", false},
		{"2025-06-18", false},
		{"2025-11-25", false},
		{"2026-07-28", true},
		{"2026-12-01", true},
		{"2027-01-01", true},
		// Not dates: must never reach the stateless branch.
		{"9999", false},
		{"abc", false},
		{"2026-07-2", false},
		{"2026-07-288", false},
		{"2026/07/28", false},
		{"20260728", false},
		{"xxxx-xx-xx", false},
	}

	for _, tc := range cases {
		if got := isStatelessVersion(tc.version); got != tc.want {
			t.Errorf("isStatelessVersion(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}
