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

func TestPlanExplicitStackPortRejectsInvalidPort(t *testing.T) {
	avail := PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{},
	}

	for _, port := range []int{0, -1, 65536} {
		_, err := PlanExplicitStackPort(avail, port, true, true)
		if err == nil {
			t.Fatalf("expected error for port %d, got nil", port)
		}
	}
}

func TestPlanExplicitStackPortRejectsTCPBusyWhenTCPNeeded(t *testing.T) {
	avail := PortAvailability{
		TCPBusy: map[int]bool{8443: true},
		UDPBusy: map[int]bool{},
	}
	_, err := PlanExplicitStackPort(avail, 8443, true, true)
	if err == nil {
		t.Fatal("expected error when TCP is needed and busy")
	}
}

func TestPlanExplicitStackPortRejectsUDPBusyWhenUDPNeeded(t *testing.T) {
	avail := PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{8443: true},
	}
	_, err := PlanExplicitStackPort(avail, 8443, true, true)
	if err == nil {
		t.Fatal("expected error when UDP is needed and busy")
	}
}

func TestPlanExplicitStackPortIgnoresBusyUnusedTransport(t *testing.T) {
	// TCP needed only, UDP is busy — should succeed
	avail := PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{8443: true},
	}
	plan, err := PlanExplicitStackPort(avail, 8443, true, false)
	if err != nil {
		t.Fatalf("expected success when busy transport is unused, got error: %v", err)
	}
	if plan.Port != 8443 {
		t.Fatalf("expected port 8443, got %d", plan.Port)
	}
	if plan.Changed || plan.Random {
		t.Fatalf("expected unchanged, non-random plan: %+v", plan)
	}

	// UDP needed only, TCP is busy — should succeed
	avail2 := PortAvailability{
		TCPBusy: map[int]bool{8443: true},
		UDPBusy: map[int]bool{},
	}
	plan2, err2 := PlanExplicitStackPort(avail2, 8443, false, true)
	if err2 != nil {
		t.Fatalf("expected success when busy transport is unused, got error: %v", err2)
	}
	if plan2.Port != 8443 {
		t.Fatalf("expected port 8443, got %d", plan2.Port)
	}
	if plan2.Changed || plan2.Random {
		t.Fatalf("expected unchanged, non-random plan: %+v", plan2)
	}
}

func TestPlanStackPortBothTransportsDelegatesToSharedPlan(t *testing.T) {
	// All ports free -> returns 443 (first preferred)
	plan := PlanStackPort(PortAvailability{
		TCPBusy: map[int]bool{},
		UDPBusy: map[int]bool{},
	}, []int{443, 8443}, func() int { return 31000 }, true, true)

	if plan.Port != 443 {
		t.Fatalf("expected 443, got %d", plan.Port)
	}
	if plan.Changed {
		t.Fatalf("expected unchanged plan when all free")
	}
	if plan.Naive.Port != 443 || plan.Naive.Transport != "tcp" {
		t.Fatalf("bad naive endpoint: %+v", plan.Naive)
	}
	if plan.Hysteria2.Port != 443 || plan.Hysteria2.Transport != "udp" {
		t.Fatalf("bad hysteria2 endpoint: %+v", plan.Hysteria2)
	}

	// First preferred busy -> falls back to 8443
	plan2 := PlanStackPort(PortAvailability{
		TCPBusy: map[int]bool{443: true},
		UDPBusy: map[int]bool{},
	}, []int{443, 8443}, func() int { return 31000 }, true, true)

	if plan2.Port != 8443 {
		t.Fatalf("expected fallback 8443, got %d", plan2.Port)
	}
	if !plan2.Changed {
		t.Fatalf("expected changed plan")
	}
	if plan2.Reason == "" {
		t.Fatalf("expected user-facing reason")
	}
}
