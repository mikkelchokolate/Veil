package protocols

import "testing"

func TestCapabilityCatalogCoversMieruEndToEnd(t *testing.T) {
	capability, ok := NewCapabilityCatalog().ForProtocol("mieru")
	if !ok {
		t.Fatal("missing Mieru protocol capability")
	}
	if capability.GeneratedConfig.PlanPath() != "/etc/veil/generated/mieru/server_config.json" || capability.ApplyAction != "restart veil-mieru.service" || capability.RuntimeUnit != "veil-mieru.service" || capability.PromotedVerb != "restart" || capability.RenderGeneratedConfig == nil {
		t.Fatalf("Mieru generated/apply/runtime capability = %+v", capability)
	}
	if len(capability.Transports) != 2 || capability.Transports[0] != "tcp" || capability.Transports[1] != "udp" {
		t.Fatalf("Mieru transports = %+v", capability.Transports)
	}
	validation, ok := capability.GeneratedConfig.ValidationSpec("/etc/veil/generated/mieru/server_config.json")
	if !ok {
		t.Fatal("missing Mieru validation spec")
	}
	cmd := validation.Command
	if len(cmd) != 4 || cmd[0] != "mieru" || cmd[1] != "check" || cmd[2] != "-c" {
		t.Fatalf("Mieru validation command = %+v", cmd)
	}
}

func TestCapabilityCatalogReturnsClonedTransports(t *testing.T) {
	all := NewCapabilityCatalog().All()
	all[0].Transports[0] = "mutated"
	capability, ok := NewCapabilityCatalog().ForProtocol("naiveproxy")
	if !ok || capability.Transports[0] != "tcp" {
		t.Fatalf("catalog leaked transport slice mutation: %+v", capability)
	}
}
