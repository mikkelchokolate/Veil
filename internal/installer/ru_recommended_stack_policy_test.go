package installer

import "testing"

func TestRURecommendedStackPolicyNormalizesLegacyStacksToPanel(t *testing.T) {
	cases := []struct {
		name    string
		input   Stack
		wantErr bool
	}{
		{"empty", "", false},
		{"panel", StackPanel, false},
		{"both exact", StackBoth, false},
		{"both with spaces", " both ", false},
		{"naive with spaces", " naive ", false},
		{"hysteria2 with spaces", " hysteria2 ", false},
		{"mieru with spaces", " mieru ", false},
		{"invalid", "bogus", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := NewRURecommendedStackPolicy(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRURecommendedStackPolicy: %v", err)
			}
			if policy.Stack != StackPanel || policy.InstallNaive || policy.InstallHysteria2 || policy.InstallMieru {
				t.Fatalf("policy = %+v, want panel-only", policy)
			}
		})
	}
}
