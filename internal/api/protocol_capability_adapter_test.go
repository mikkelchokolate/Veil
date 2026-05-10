package api

import "testing"

type testProtocolCapabilityAdapter struct {
	capability ProtocolCapability
}

func (a testProtocolCapabilityAdapter) Capability() ProtocolCapability { return a.capability }

func TestProtocolCapabilityCatalogIsAssembledFromAdapters(t *testing.T) {
	catalog := NewProtocolCapabilityCatalogFromAdapters(
		testProtocolCapabilityAdapter{capability: ProtocolCapability{Protocol: "one", Transports: []string{"tcp"}}},
		testProtocolCapabilityAdapter{capability: ProtocolCapability{Protocol: "two", Transports: []string{"udp"}}},
	)

	all := catalog.All()
	if len(all) != 2 || all[0].Protocol != "one" || all[1].Protocol != "two" {
		t.Fatalf("catalog order = %+v", all)
	}
	all[0].Transports[0] = "mutated"
	capability, ok := catalog.ForProtocol("one")
	if !ok || capability.Transports[0] != "tcp" {
		t.Fatalf("catalog leaked adapter transport slice mutation: %+v", capability)
	}
}

func TestDefaultProtocolCapabilityCatalogUsesProtocolAdapters(t *testing.T) {
	catalog := NewProtocolCapabilityCatalog()
	for _, protocol := range []string{"naiveproxy", "hysteria2", "mieru"} {
		capability, ok := catalog.ForProtocol(protocol)
		if !ok {
			t.Fatalf("missing protocol adapter capability for %s", protocol)
		}
		if capability.RenderGeneratedConfig == nil {
			t.Fatalf("%s adapter capability missing generated config behavior: %+v", protocol, capability)
		}
	}
}
