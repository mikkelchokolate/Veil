package service

import (
	"testing"

	"github.com/veil-panel/veil/internal/model"
)

func TestProtocolRuntimeProvisioningSelectsRuntimesFromEnabledInbounds(t *testing.T) {
	catalog := NewManagedRuntimeCatalog([]ManagedRuntime{
		{Name: "naive", Protocol: "naiveproxy", Unit: "veil-naive.service"},
		{Name: "hysteria2", Protocol: "hysteria2", Unit: "veil-hysteria2.service"},
		{Name: "sing-box", Unit: "veil-warp.service"},
		{Name: "mieru", ActionName: "mieru", Protocol: "mieru", Unit: "veil-mieru.service"},
	})
	plan := NewProtocolRuntimeProvisioning(catalog).Plan([]model.Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		{Name: "mieru-udp", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true},
		{Name: "hysteria", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		{Name: "disabled-naive", Protocol: "naiveproxy", Transport: "tcp", Port: 9443, Enabled: false},
	}, model.WarpConfig{Enabled: true})
	wantUnits := []string{"veil-hysteria2.service", "veil-warp.service", "veil-mieru.service"}
	if !sameStrings(plan.SystemdUnits(), wantUnits) {
		t.Fatalf("SystemdUnits() = %+v, want %+v", plan.SystemdUnits(), wantUnits)
	}
	if !plan.RequiresRuntime("mieru") || plan.RequiresRuntime("naive") {
		t.Fatalf("runtime requirement mismatch: %+v", plan.Runtimes)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
