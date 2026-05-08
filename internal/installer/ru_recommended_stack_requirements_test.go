package installer

import "testing"

func TestRURecommendedStackPolicyNoLongerOwnsProtocolInstallRequirements(t *testing.T) {
	for _, stack := range []Stack{StackPanel, StackMieru, StackNaive, StackHysteria2, StackBoth} {
		policy, err := NewRURecommendedStackPolicy(stack)
		if err != nil {
			t.Fatalf("NewRURecommendedStackPolicy(%q): %v", stack, err)
		}
		if policy.RequiresDomain() || policy.RequiresSharedProxyPort() {
			t.Fatalf("legacy stack %q should not require install-time protocol settings: %+v", stack, policy)
		}
	}
}
