package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// One shared mita daemon serves every mieru inbound, so a single
// "mieru:server" provider must be registered whenever at least one mieru
// inbound is enabled — regardless of how many mieru inbounds exist.
func TestTrafficProviderRegistersMieruDaemonProvider(t *testing.T) {
	state := newClientLifecycleTestState(t)
	boundClient, err := state.clientRepo.Create(client.Client{Name: "mieru-client", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.clientRepo.CreateBinding(client.Binding{
		ClientID: boundClient.ID, InboundID: "mi-tcp", RuntimeIdentity: "mieru_runtime_id", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	state.inbounds = []Inbound{
		{Name: "mi-tcp", Protocol: "mieru", Transport: "tcp", Port: 25444, Enabled: true},
		{Name: "mi-udp", Protocol: "mieru", Transport: "udp", Port: 25445, Enabled: true},
		{Name: "mi-off", Protocol: "mieru", Transport: "tcp", Port: 25446, Enabled: false},
	}
	state.registerTrafficProvidersLocked()
	state.mu.Unlock()

	var found bool
	for _, health := range state.trafficCollector.ProviderHealth() {
		if health.Key == "mieru:server" {
			found = true
		}
	}
	if !found {
		keys := make([]string, 0)
		for _, health := range state.trafficCollector.ProviderHealth() {
			keys = append(keys, health.Key)
		}
		t.Fatalf("mieru:server provider not registered; providers = %v", keys)
	}
	if got := state.trafficCollector.ProviderCount(); got != 1 {
		t.Fatalf("ProviderCount = %d, want exactly one shared mieru provider", got)
	}
}

// A mieru binding advertises traffic accounting now that the daemon's
// per-user metrics feed the collector; quota enforcement stays unsupported.
func TestMieruBindingCapabilityAdvertisesTrafficAccounting(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "mi-tcp", Protocol: "mieru", Transport: "tcp", Port: 25444, Enabled: true}}
	state.mu.Unlock()
	capability := state.bindingCapabilityForInbound("mi-tcp")
	if capability == nil {
		t.Fatal("mieru binding capability is nil")
	}
	if !capability.TrafficAccounting {
		t.Fatal("mieru binding must advertise traffic accounting")
	}
	if capability.QuotaEnforcement {
		t.Fatal("mieru quota enforcement must stay unadvertised until limits are wired")
	}
}
