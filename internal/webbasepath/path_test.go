package webbasepath

import "testing"

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
