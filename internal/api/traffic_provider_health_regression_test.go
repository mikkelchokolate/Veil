package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

type healthRegressionTrafficProvider struct {
	key      string
	readings map[string]client.ProviderReading
	err      error
}

func (p *healthRegressionTrafficProvider) Key() string { return p.key }
func (p *healthRegressionTrafficProvider) Read() (client.ProviderBatch, error) {
	readings := make([]client.ProviderReading, 0, len(p.readings))
	for _, reading := range p.readings {
		readings = append(readings, reading)
	}
	return client.ProviderBatch{Readings: readings, ObservedAt: time.Now().UTC(), RuntimeInstance: p.key}, p.err
}

func TestTrafficProvidersScopeRuntimeIdentityMappingsPerInbound(t *testing.T) {
	const firstPublicPort = 25443
	const secondPublicPort = 25444
	var firstCounters atomic.Int64
	var secondCounters atomic.Int64
	firstCounters.Store(100)
	secondCounters.Store(1000)
	var firstAuth, secondAuth atomic.Value
	newServer := func(publicPort int, counters *atomic.Int64, auth *atomic.Value) *httptest.Server {
		listener, err := net.Listen("tcp", runtimeports.Hysteria2TrafficStatsAddress(publicPort))
		if err != nil {
			t.Fatalf("listen on isolated Hysteria stats endpoint for UDP port %d: %v", publicPort, err)
		}
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth.Store(r.Header.Get("Authorization"))
			value := counters.Load()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"shared_runtime_identity":{"tx":` + strconv.FormatInt(value, 10) + `,"rx":` + strconv.FormatInt(value, 10) + `}}`))
		}))
		server.Listener = listener
		server.Start()
		return server
	}
	firstServer := newServer(firstPublicPort, &firstCounters, &firstAuth)
	defer firstServer.Close()
	secondServer := newServer(secondPublicPort, &secondCounters, &secondAuth)
	defer secondServer.Close()

	state := newClientLifecycleTestState(t)
	firstClient, err := state.clientRepo.Create(client.Client{Name: "first-client", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	secondClient, err := state.clientRepo.Create(client.Client{Name: "second-client", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	firstBinding, err := state.clientRepo.CreateBinding(client.Binding{
		ClientID: firstClient.ID, InboundID: "hy-first", RuntimeIdentity: "shared_runtime_identity", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondBinding, err := state.clientRepo.CreateBinding(client.Binding{
		ClientID: secondClient.ID, InboundID: "hy-second", RuntimeIdentity: "shared_runtime_identity", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstBinding.ID == secondBinding.ID {
		t.Fatal("fixture bindings are not distinct")
	}

	state.mu.Lock()
	state.inbounds = []Inbound{
		{Name: "hy-first", Protocol: "hysteria2", Transport: "udp", Port: firstPublicPort, Enabled: true, Password: "first-fallback-secret"},
		{Name: "hy-second", Protocol: "hysteria2", Transport: "udp", Port: secondPublicPort, Enabled: true, Password: "second-fallback-secret"},
	}
	state.registerTrafficProvidersLocked()
	state.mu.Unlock()

	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Errorf("baseline collection: %v", err)
	}
	firstCounters.Store(175)
	secondCounters.Store(1250)
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Errorf("delta collection: %v", err)
	}
	firstUp, firstDown, err := state.trafficStore.TotalsForClient(firstClient.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondUp, secondDown, err := state.trafficStore.TotalsForClient(secondClient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstUp != 75 || firstDown != 75 {
		t.Errorf("first inbound totals = %d/%d, want 75/75", firstUp, firstDown)
	}
	if secondUp != 250 || secondDown != 250 {
		t.Errorf("second inbound totals = %d/%d, want 250/250", secondUp, secondDown)
	}
	for name, value := range map[string]any{"first": firstAuth.Load(), "second": secondAuth.Load()} {
		secret, _ := value.(string)
		if strings.TrimSpace(secret) == "" {
			t.Errorf("%s inbound Traffic Stats API request was unauthenticated", name)
		}
	}
	if firstAuth.Load() == secondAuth.Load() {
		t.Error("different inbounds reused the same Traffic Stats API secret")
	}
}

func TestTrafficLegacyHysteriaUsernameIsAttributedToMigratedBinding(t *testing.T) {
	const publicPort = 25445
	var counters atomic.Int64
	counters.Store(100)
	listener, err := net.Listen("tcp", runtimeports.Hysteria2TrafficStatsAddress(publicPort))
	if err != nil {
		t.Fatalf("listen on isolated Hysteria stats endpoint: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		value := counters.Load()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"legacy_user":{"tx":` + strconv.FormatInt(value, 10) + `,"rx":` + strconv.FormatInt(value, 10) + `}}`))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	state := newClientLifecycleTestState(t)
	if state.clientMigrator == nil {
		t.Fatal("client migrator was not initialized")
	}
	migrated, err := state.clientMigrator.MigrateInboundProfiles("hy-legacy", "hysteria2", []client.LegacyProfile{
		{Name: "legacy_user", Username: "legacy_user", Password: "secret", Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if migrated.ClientsCreated != 1 || migrated.BindingsCreated != 1 {
		t.Fatalf("migration result = %+v, want one client and binding", migrated)
	}
	wantID := client.StableClientID("hy-legacy", "legacy_user")
	if _, err := state.db.Exec(`UPDATE clients SET name=? WHERE id=?`, "phone", wantID); err != nil {
		t.Fatal(err)
	}
	bindings, err := state.clientRepo.BindingsForClient(wantID)
	if err != nil || len(bindings) != 1 {
		t.Fatalf("migrated bindings = %+v err=%v", bindings, err)
	}
	binding := bindings[0]

	state.mu.Lock()
	state.inbounds = []Inbound{{
		Name: "hy-legacy", Protocol: "hysteria2", Transport: "udp", Port: publicPort, Enabled: true,
		Profiles: []ClientProfile{{Name: "legacy_user", Username: "legacy_user", Enabled: true}},
	}}
	state.registerTrafficProvidersLocked()
	state.mu.Unlock()

	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("baseline collection: %v", err)
	}
	counters.Store(175)
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("delta collection: %v", err)
	}
	up, down, err := state.trafficStore.TotalsForClient(wantID)
	if err != nil {
		t.Fatal(err)
	}
	if up != 75 || down != 75 {
		t.Errorf("legacy username totals = %d/%d, want 75/75 for binding %s", up, down, binding.ID)
	}
	for _, status := range state.trafficCollector.ProviderHealth() {
		if status.Key == "hysteria2:hy-legacy" && status.State != "healthy" {
			t.Errorf("provider health = %+v, want healthy after aliasing leftover username", status)
		}
	}
}

func TestTrafficCollectorFailureIsVisibleInAPIAndMetrics(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	provider := &healthRegressionTrafficProvider{
		key:      "health-regression",
		readings: map[string]client.ProviderReading{},
	}
	state.trafficCollector.ResetProviders([]client.TrafficProvider{provider})
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("healthy observation: %v", err)
	}
	provider.err = errors.New("simulated provider outage")
	if err := state.trafficCollector.CollectOnce(); err == nil {
		t.Error("collector silently discarded provider error")
	}

	summary := v1Request(t, router, http.MethodGet, "/api/v1/traffic/summary", "")
	if summary.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", summary.Code, summary.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(summary.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["state"] != "degraded" {
		t.Errorf("traffic state = %v, want degraded", body["state"])
	}
	providers, _ := body["providers"].([]any)
	if len(providers) != 1 {
		t.Errorf("provider health rows = %d, want 1: %v", len(providers), body)
	} else {
		status, _ := providers[0].(map[string]any)
		if status["key"] != "health-regression" || status["state"] != "degraded" {
			t.Errorf("provider status = %v", status)
		}
		if value, _ := status["lastSuccessfulObservationAt"].(float64); value <= 0 {
			t.Errorf("lastSuccessfulObservationAt = %v, want durable visible timestamp", status["lastSuccessfulObservationAt"])
		}
		if !strings.Contains(strings.ToLower(status["lastError"].(string)), "outage") {
			t.Errorf("lastError = %v, want provider failure", status["lastError"])
		}
	}

	metrics := httptest.NewRecorder()
	router.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics: %d %s", metrics.Code, metrics.Body.String())
	}
	for _, want := range []string{
		`veil_traffic_provider_up{provider="health-regression"} 0`,
		`veil_traffic_provider_errors_total{provider="health-regression"} 1`,
	} {
		if !strings.Contains(metrics.Body.String(), want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}
