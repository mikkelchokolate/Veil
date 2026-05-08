package installer

import "testing"

func TestRURecommendedPortPolicyDoesNotAllocateSharedPortWhenStackHasNoSharedProxyRuntime(t *testing.T) {
	policy := NewRURecommendedPortPolicy(PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}}, func() int { return 31874 })
	for _, stack := range []RURecommendedStackPolicy{{Stack: StackPanel}, {Stack: StackMieru, InstallMieru: true}} {
		plan, err := policy.Plan(0, stack)
		if err != nil {
			t.Fatalf("Plan(%+v): %v", stack, err)
		}
		if plan.Port != 0 || plan.Naive.Port != 0 || plan.Hysteria2.Port != 0 || plan.Changed || plan.Random || plan.Reason != "" {
			t.Fatalf("stack without shared proxy runtime should not get shared port plan: %+v", plan)
		}
	}
}

func TestRURecommendedPortPolicyUsesPreferredSharedPorts(t *testing.T) {
	policy := NewRURecommendedPortPolicy(PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}}, func() int { return 31874 })
	plan, err := policy.Plan(0, RURecommendedStackPolicy{InstallNaive: true, InstallHysteria2: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Port != 443 {
		t.Fatalf("port = %d", plan.Port)
	}
}

func TestRURecommendedPortPolicyHonorsExplicitPort(t *testing.T) {
	policy := NewRURecommendedPortPolicy(PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}}, func() int { return 31874 })
	plan, err := policy.Plan(9443, RURecommendedStackPolicy{InstallNaive: true, InstallHysteria2: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Port != 9443 || plan.Reason != "user selected shared proxy port" {
		t.Fatalf("plan = %+v", plan)
	}
}
