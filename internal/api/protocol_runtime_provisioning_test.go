package api

import "testing"

func TestProtocolRuntimeProvisioningSelectsRuntimesFromEnabledInbounds(t *testing.T) {
	plan := NewProtocolRuntimeProvisioning().Plan([]Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true},
		{Name: "hysteria", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		{Name: "disabled-naive", Protocol: "naiveproxy", Transport: "tcp", Port: 9443, Enabled: false},
	}, WarpConfig{Enabled: true})

	wantUnits := []string{"veil-hysteria2.service", "veil-warp.service", "veil-mieru.service"}
	if !equalStrings(plan.SystemdUnits(), wantUnits) {
		t.Fatalf("SystemdUnits() = %+v, want %+v", plan.SystemdUnits(), wantUnits)
	}
	if !plan.RequiresRuntime("mieru") || plan.RequiresRuntime("naive") {
		t.Fatalf("runtime requirement mismatch: %+v", plan.Runtimes)
	}
}
