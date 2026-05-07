package installer

import "testing"

func TestRURecommendedStackPolicyOwnsInstallRequirements(t *testing.T) {
	cases := []struct {
		stack               Stack
		wantDomain          bool
		wantSharedProxyPort bool
	}{
		{StackPanel, false, false},
		{StackMieru, false, false},
		{StackNaive, true, true},
		{StackHysteria2, true, true},
		{StackBoth, true, true},
	}
	for _, tc := range cases {
		policy, err := NewRURecommendedStackPolicy(tc.stack)
		if err != nil {
			t.Fatalf("NewRURecommendedStackPolicy(%q): %v", tc.stack, err)
		}
		if policy.RequiresDomain() != tc.wantDomain || policy.RequiresSharedProxyPort() != tc.wantSharedProxyPort {
			t.Fatalf("policy for %q = %+v domain=%v port=%v", tc.stack, policy, policy.RequiresDomain(), policy.RequiresSharedProxyPort())
		}
	}
}
