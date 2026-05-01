package installer

import "testing"

func TestPlanSharedPortPrefers443WhenTcpAndUdpFree(t *testing.T) {
	plan := PlanSharedPort(PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{},
	}, []int{443, 8443}, func() int { return 31000 })

	if plan.Port != 443 {
		t.Fatalf("expected 443, got %d", plan.Port)
	}
	if plan.Changed {
		t.Fatalf("expected unchanged plan")
	}
	if plan.Naive.Port != 443 || plan.Naive.Transport != "tcp" {
		t.Fatalf("bad naive endpoint: %+v", plan.Naive)
	}
	if plan.Hysteria2.Port != 443 || plan.Hysteria2.Transport != "udp" {
		t.Fatalf("bad hysteria2 endpoint: %+v", plan.Hysteria2)
	}
}

func TestPlanSharedPortFallsBackWhenEitherTransportBusy(t *testing.T) {
	plan := PlanSharedPort(PortAvailability{
		TCPBusy: map[int]bool{443: true},
		UDPBusy: map[int]bool{},
	}, []int{443, 8443}, func() int { return 31000 })

	if plan.Port != 8443 {
		t.Fatalf("expected fallback 8443, got %d", plan.Port)
	}
	if !plan.Changed {
		t.Fatalf("expected changed plan")
	}
	if plan.Reason == "" {
		t.Fatalf("expected user-facing reason")
	}
}

func TestPlanSharedPortUsesRandomWhenPreferredPortsBusy(t *testing.T) {
	plan := PlanSharedPort(PortAvailability{
		TCPBusy: map[int]bool{443: true, 8443: true},
		UDPBusy: map[int]bool{443: true, 8443: true},
	}, []int{443, 8443}, func() int { return 31874 })

	if plan.Port != 31874 {
		t.Fatalf("expected random 31874, got %d", plan.Port)
	}
	if !plan.Random || !plan.Changed {
		t.Fatalf("expected random changed plan: %+v", plan)
	}
}
