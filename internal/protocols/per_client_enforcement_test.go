package protocols

import "testing"

// TestPerClientCredentialEnforcementCapability covers audit #309: rendering
// per-client links does not imply runtime per-client enforcement. olcRTC
// implements ClientAccessProvider but every client shares the inbound-wide
// encryption key, so it must not advertise per-client credential or expiry
// enforcement.
func TestPerClientCredentialEnforcementCapability(t *testing.T) {
	want := map[string]bool{
		"hysteria2":  true,
		"naiveproxy": true,
		"mieru":      true,
		"olcrtc":     false,
	}
	registry := NewRegistry()
	for protocol, expected := range want {
		p, ok := registry.Get(protocol)
		if !ok {
			t.Fatalf("protocol %q not registered", protocol)
		}
		if got := EnforcesPerClientCredentials(p); got != expected {
			t.Errorf("EnforcesPerClientCredentials(%s) = %v, want %v", protocol, got, expected)
		}
		// Link-generation capability stays separate: every registered
		// protocol still renders client links.
		if _, ok := AsClientAccessProvider(p); !ok {
			if _, ok := AsClientAccessAggregator(p); !ok {
				t.Errorf("protocol %s lost its client-access capability", protocol)
			}
		}
	}
}
