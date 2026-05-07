package api

import "testing"

func TestStackProtocolPolicyIncludesMieruAndPanelSelections(t *testing.T) {
	cases := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"mieru", "mieru", true},
		{"mieru", "naiveproxy", false},
		{"mieru", "hysteria2", false},
		{"panel", "mieru", false},
		{"panel", "naiveproxy", false},
		{"both", "mieru", true},
	}
	for _, tc := range cases {
		if got := NewStackProtocolPolicy(tc.stack).Includes(tc.protocol); got != tc.want {
			t.Fatalf("Includes(%q,%q) = %v, want %v", tc.stack, tc.protocol, got, tc.want)
		}
	}
}

func TestSettingsValidationAcceptsPanelAndMieruStacks(t *testing.T) {
	for _, stack := range []string{"panel", "mieru"} {
		settings := Settings{PanelListen: "127.0.0.1:2096", Stack: stack, Mode: "dev"}
		if err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{}); err != nil {
			t.Fatalf("stack %q should be valid: %v", stack, err)
		}
	}
}
