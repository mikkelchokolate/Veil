package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func TestTrafficProviderRefreshPreservesSetOnDatabaseError(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-live", Protocol: "hysteria2", Transport: "udp", Port: 25001, Enabled: true, Password: "secret"}}
	state.registerTrafficProvidersLocked()
	before := state.trafficCollector.ProviderCount()
	state.mu.Unlock()
	if before == 0 {
		t.Fatal("expected a registered provider before the injected failure")
	}

	closed := testdb.Open(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	state.clientRepo = client.NewRepository(closed)
	state.registerTrafficProvidersLocked()
	after := state.trafficCollector.ProviderCount()
	state.mu.Unlock()
	if after != before {
		t.Fatalf("provider count changed after db error: before=%d after=%d", before, after)
	}
}
