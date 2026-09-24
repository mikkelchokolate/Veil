package webbasepath

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "/", false},
		{"/", "/", false},
		{"//", "/", false},
		{"panel", "/panel/", false},
		{"/panel", "/panel/", false},
		{"panel/admin", "/panel/admin/", false},
		{"panel//admin", "", true},
		{"panel/../admin", "", true},
		{"panel?debug", "", true},
		{"panel#fragment", "", true},
		{"panel'break", "", true},
		{"panel\\admin", "", true},
		// "s" is reserved as a FIRST segment only — it collides with the
		// public /s/{token} subscription feeds (#662).
		{"s", "", true},
		{"/s/", "", true},
		{"s/panel", "", true},
		{"/s/secret/", "", true},
		{"s2", "/s2/", false},
		{"panel/s", "/panel/s/", false},
		{"subscriptions", "/subscriptions/", false},
		// Root-mux first segments are reserved as a FIRST segment only —
		// a same-named mount would shadow the real endpoint or serve the
		// SPA in its place (#765).
		{"api", "", true},
		{"/api/", "", true},
		{"api/v2", "", true},
		{"metrics", "", true},
		{"metrics/panel", "", true},
		{"healthz", "", true},
		{"livez", "", true},
		{"readyz", "", true},
		{"assets", "", true},
		{"favicon.ico", "", true},
		{"favicon.svg", "", true},
		{"robots.txt", "", true},
		{"apis", "/apis/", false},
		{"panel/api", "/panel/api/", false},
		{"panel/metrics", "/panel/metrics/", false},
		{"api2", "/api2/", false},
		{"myassets", "/myassets/", false},
		{"API", "/API/", false}, // mux patterns are case-sensitive
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Normalize(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Normalize(%q) expected an error, got %q", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestNormalizeReservedFirstSegmentMessage locks the operator-facing error:
// it must name the offending segment so a rejected webBasePath is actionable
// in settings validation output.
func TestNormalizeReservedFirstSegmentMessage(t *testing.T) {
	for _, input := range []string{"api", "metrics", "healthz", "assets", "s"} {
		_, err := Normalize(input)
		if err == nil {
			t.Fatalf("Normalize(%q): expected reserved-segment error", input)
		}
		if !strings.Contains(err.Error(), input) || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("Normalize(%q) error = %q, want segment name + reserved reason", input, err)
		}
	}
}

func TestNormalizeOptionalUsesEmptyRoot(t *testing.T) {
	for _, input := range []string{"", "/", "//"} {
		got, err := NormalizeOptional(input)
		if err != nil {
			t.Fatalf("NormalizeOptional(%q): %v", input, err)
		}
		if got != "" {
			t.Fatalf("NormalizeOptional(%q) = %q, want empty root", input, got)
		}
	}
}
