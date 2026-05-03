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

func TestPlanExplicitStackPortReturnsPlanForFreePort(t *testing.T) {
	plan, err := PlanExplicitStackPort(PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{},
	}, 443, true, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if plan.Port != 443 {
		t.Fatalf("expected port 443, got %d", plan.Port)
	}
	if plan.Naive.Port != 443 || plan.Naive.Transport != "tcp" {
		t.Fatalf("bad naive endpoint: %+v", plan.Naive)
	}
	if plan.Hysteria2.Port != 443 || plan.Hysteria2.Transport != "udp" {
		t.Fatalf("bad hysteria2 endpoint: %+v", plan.Hysteria2)
	}
	if plan.Changed || plan.Random {
		t.Fatalf("expected unchanged, non-random plan: %+v", plan)
	}
}

func TestPlanStackPortIgnoresUnusedTransport(t *testing.T) {
	naiveOnly := PlanStackPort(PortAvailability{
		UDPBusy: map[int]bool{443: true},
	}, []int{443, 8443}, func() int { return 31000 }, true, false)
	if naiveOnly.Port != 443 {
		t.Fatalf("expected naive-only port plan to ignore UDP/443, got %d", naiveOnly.Port)
	}

	hysteriaOnly := PlanStackPort(PortAvailability{
		TCPBusy: map[int]bool{443: true},
	}, []int{443, 8443}, func() int { return 31000 }, false, true)
	if hysteriaOnly.Port != 443 {
		t.Fatalf("expected hysteria-only port plan to ignore TCP/443, got %d", hysteriaOnly.Port)
	}
}
