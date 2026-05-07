package installer

import "testing"

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
