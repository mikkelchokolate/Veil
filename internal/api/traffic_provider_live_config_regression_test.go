package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// The provider must authenticate with the credential the running unit
// actually serves — the trafficStats.secret in the published rendered
// config. Deriving the secret from the last applied snapshot instead leaves
// telemetry permanently broken (401) after any apply that restarts the unit
// but fails before publishing the revision.
func TestTrafficProviderAuthenticatesWithPublishedRuntimeCredential(t *testing.T) {
	const publicPort = 25446
	const runtimePassword = "new-password-from-failed-revision"
	const stalePassword = "old-password-from-applied-snapshot"
	const inboundName = "hy-drift"

	// The secret the running unit serves is whatever the rendered YAML
	// carries; compute it from the newer revision's password exactly like the
	// apply renderer does.
	renderedSecret := hysteria2.TrafficStatsSecret(Settings{}, Inbound{Name: inboundName, Password: runtimePassword})
	var authSeen atomic.Value
	var counters atomic.Int64
	counters.Store(100)
	listener, err := net.Listen("tcp", runtimeports.Hysteria2TrafficStatsAddress(publicPort))
	if err != nil {
		t.Fatalf("listen on isolated Hysteria stats endpoint: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen.Store(r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != renderedSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		value := counters.Load()
		_, _ = w.Write([]byte(`{"runtime_identity":{"tx":` + strconv.FormatInt(value, 10) + `,"rx":` + strconv.FormatInt(value, 10) + `}}`))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	state := newClientLifecycleTestState(t)
	boundClient, err := state.clientRepo.Create(client.Client{Name: "drift-client", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.clientRepo.CreateBinding(client.Binding{
		ClientID: boundClient.ID, InboundID: inboundName, RuntimeIdentity: "runtime_identity", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	// Publish the rendered artifact the way a partially failed apply leaves
	// it: the unit already loaded the newer secret while the applied snapshot
	// (and state.inbounds below) still carries the old password.
	liveConfig := filepath.Join(state.liveRoot, "hysteria2", inboundName+".yaml")
	if err := os.MkdirAll(filepath.Dir(liveConfig), 0o750); err != nil {
		t.Fatal(err)
	}
	body, err := renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:         publicPort,
		Password:           runtimePassword,
		TrafficStatsListen: runtimeports.Hysteria2TrafficStatsAddress(publicPort),
		TrafficStatsSecret: renderedSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(liveConfig, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	state.inbounds = []Inbound{{
		Name: inboundName, Protocol: "hysteria2", Transport: "udp",
		Port: publicPort, Enabled: true, Password: stalePassword,
	}}
	state.registerTrafficProvidersLocked()
	state.mu.Unlock()

	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("baseline collection against the published credential failed: %v", err)
	}
	counters.Store(175)
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("delta collection against the published credential failed: %v", err)
	}
	if got, _ := authSeen.Load().(string); got != renderedSecret {
		t.Fatalf("provider authenticated with a stale credential, want the published runtime secret")
	}
	up, down, err := state.trafficStore.TotalsForClient(boundClient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 75 || down != 75 {
		t.Errorf("totals = %d/%d, want 75/75", up, down)
	}
	for _, health := range state.trafficCollector.ProviderHealth() {
		if health.State != "healthy" {
			t.Errorf("provider %s state = %q (lastError=%q), want healthy", health.Key, health.State, health.LastError)
		}
	}
}

// When no rendered config is published yet (fresh state before the first
// apply), the provider keeps the snapshot-derived credential so a unit that
// was rendered from that same snapshot still authenticates.
func TestTrafficProviderFallsBackToSnapshotCredentialWithoutLiveConfig(t *testing.T) {
	const publicPort = 25447
	const inboundName = "hy-fresh"
	snapshotSecret := hysteria2.TrafficStatsSecret(Settings{}, Inbound{Name: inboundName, Password: "snapshot-pass"})
	var authSeen atomic.Value
	listener, err := net.Listen("tcp", runtimeports.Hysteria2TrafficStatsAddress(publicPort))
	if err != nil {
		t.Fatalf("listen on isolated Hysteria stats endpoint: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen.Store(r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != snapshotSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{
		Name: inboundName, Protocol: "hysteria2", Transport: "udp",
		Port: publicPort, Enabled: true, Password: "snapshot-pass",
	}}
	state.registerTrafficProvidersLocked()
	state.mu.Unlock()

	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collection with the snapshot credential failed: %v", err)
	}
	if got, _ := authSeen.Load().(string); got != snapshotSecret {
		t.Fatalf("provider did not fall back to the snapshot-derived credential")
	}
}
