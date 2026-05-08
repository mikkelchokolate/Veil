package api

import "testing"

func TestProtocolCapabilityCatalogCoversMieruEndToEnd(t *testing.T) {
	capability, ok := NewProtocolCapabilityCatalog().ForProtocol("mieru")
	if !ok {
		t.Fatal("missing Mieru protocol capability")
	}
	if capability.GeneratedConfigPath != "/etc/veil/generated/mieru/server_config.json" || capability.ApplyAction != "restart veil-mieru.service" || capability.RuntimeUnit != "veil-mieru.service" || capability.RenderGeneratedConfig == nil {
		t.Fatalf("Mieru generated/apply/runtime capability = %+v", capability)
	}
	if len(capability.Transports) != 2 || capability.Transports[0] != "tcp" || capability.Transports[1] != "udp" {
		t.Fatalf("Mieru transports = %+v", capability.Transports)
	}
	cmd := capability.ValidationCommand("/etc/veil/generated/mieru/server_config.json")
	if len(cmd) != 4 || cmd[0] != "mieru" || cmd[1] != "check" || cmd[2] != "-c" {
		t.Fatalf("Mieru validation command = %+v", cmd)
	}
}
