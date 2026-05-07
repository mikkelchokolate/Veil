package installer

import "testing"

func TestRURecommendedStackPolicyNormalizesStackAndInstallFlags(t *testing.T) {
	cases := []struct {
		name          string
		input         Stack
		wantStack     Stack
		wantNaive     bool
		wantHysteria2 bool
		wantErr       bool
	}{
		{"empty", "", StackBoth, true, true, false},
		{"both exact", StackBoth, StackBoth, true, true, false},
		{"both with spaces", " both ", StackBoth, true, true, false},
		{"naive with spaces", " naive ", StackNaive, true, false, false},
		{"hysteria2 with spaces", " hysteria2 ", StackHysteria2, false, true, false},
		{"invalid", "bogus", "", false, false, true},
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
			if policy.Stack != tc.wantStack || policy.InstallNaive != tc.wantNaive || policy.InstallHysteria2 != tc.wantHysteria2 {
				t.Fatalf("policy = %+v", policy)
			}
		})
	}
}
