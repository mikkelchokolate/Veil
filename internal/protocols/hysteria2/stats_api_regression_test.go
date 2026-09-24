package hysteria2

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

func setStatsProviderSecretForRegression(t *testing.T, provider *StatsProvider, secret string) {
	t.Helper()
	provider.secret = secret
}

func TestStatsProviderUsesAuthenticatedOfficialTrafficAPI(t *testing.T) {
	const secret = "traffic-api-test-secret"
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/traffic" {
			t.Errorf("path = %q, want /traffic", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q; collector must not clear runtime counters", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != secret {
			t.Errorf("Authorization = %q, want configured secret", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"custom_identity":{"tx":514,"rx":4017}}`))
	}))
	defer server.Close()

	provider := NewStatsProvider("hysteria2:one", server.URL, map[string]string{"custom_identity": "binding-one"})
	setStatsProviderSecretForRegression(t, provider, secret)
	readings, err := provider.Read()
	if err != nil {
		t.Fatalf("Read official traffic API: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("API requests = %d, want 1", requests.Load())
	}
	if len(readings.Readings) != 1 {
		t.Fatalf("readings = %d, want only the scoped known identity", len(readings.Readings))
	}
	reading := readings.Readings[0]
	if reading.BindingID != "binding-one" || reading.UploadBytes != 514 || reading.DownloadBytes != 4017 {
		t.Errorf("reading = %+v, want tx/rx attributed through RuntimeIdentity", reading)
	}
}

func TestStatsProviderBoundsTrafficAPIResponse(t *testing.T) {
	const maxExpectedResponse = int64(1 << 20)
	var requested atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"oversized":{"tx":1,"rx":1},"padding":"`)
		_, _ = fmt.Fprint(w, strings.Repeat("x", int(maxExpectedResponse)+1))
		_, _ = fmt.Fprint(w, `"}`)
	}))
	defer server.Close()

	provider := NewStatsProvider("hysteria2:oversized", server.URL, map[string]string{"oversized": "binding"})
	setStatsProviderSecretForRegression(t, provider, "secret")
	_, err := provider.Read()
	if !requested.Load() {
		t.Fatal("provider never contacted the configured Traffic Stats API")
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "large") {
		t.Fatalf("oversized response error = %v, want explicit size-limit failure", err)
	}
}

func TestHysteriaRenderConfigEnablesAuthenticatedIsolatedTrafficStats(t *testing.T) {
	const port = 24443
	settings := model.Settings{Domain: "vpn.example.test", Hysteria2Password: "fallback-secret"}
	inbound := model.Inbound{
		Name: "hy-stats", Protocol: "hysteria2", Transport: "udp", Port: port, Enabled: true,
		RuntimeCredentials: []model.RuntimeCredential{{Name: "client", Username: "custom_identity", Password: "client-secret"}},
	}
	artifacts, ok, err := New().RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths(t.TempDir()),
		Inbounds: []model.Inbound{inbound},
	})
	if err != nil || !ok || len(artifacts) != 1 {
		t.Fatalf("RenderConfig: ok=%v artifacts=%d err=%v", ok, len(artifacts), err)
	}
	body := artifacts[0].Body
	// The secret line must carry the exact derived stats secret — presence of
	// a "secret:" key alone greens `secret: ""` or a wrong value (#822).
	wantSecret := TrafficStatsSecret(settings, inbound)
	if len(wantSecret) != 64 {
		t.Fatalf("TrafficStatsSecret = %q, want 64-hex argon2 digest", wantSecret)
	}
	for _, want := range []string{
		"trafficStats:",
		"listen: " + runtimeports.Hysteria2TrafficStatsAddress(port),
		"secret: " + wantSecret,
		"custom_identity",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered Hysteria config missing %q", want)
		}
	}
	if strings.Contains(body, "secret: \"\"") || strings.Contains(body, "secret: fallback-secret") || strings.Contains(body, "secret: client-secret") {
		t.Errorf("traffic stats secret must be the derived argon2 digest, not empty/plaintext")
	}
	if strings.Contains(body, fmt.Sprintf("listen: 127.0.0.1:%d", port)) {
		t.Error("Traffic Stats API reused the public Hysteria port and can collide with a TCP listener")
	}
}

// TestAuthenticatedStatsProviderTargetsSameIsolatedEndpointAsRenderer renders
// the real per-inbound config and proves the stats provider dials the
// trafficStats endpoint that config actually declares — name-lie coupling via
// a bare port constant would green a renderer/provider drift (#894).
func TestAuthenticatedStatsProviderTargetsSameIsolatedEndpointAsRenderer(t *testing.T) {
	const publicPort = 24443
	settings := model.Settings{Domain: "vpn.example.test", Hysteria2Password: "fallback-secret"}
	inbound := model.Inbound{
		Name: "hy-stats", Protocol: "hysteria2", Transport: "udp", Port: publicPort, Enabled: true,
		RuntimeCredentials: []model.RuntimeCredential{{Name: "client", Username: "u", Password: "p"}},
	}
	artifacts, ok, err := New().RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths(t.TempDir()),
		Inbounds: []model.Inbound{inbound},
	})
	if err != nil || !ok || len(artifacts) != 1 {
		t.Fatalf("RenderConfig: ok=%v artifacts=%d err=%v", ok, len(artifacts), err)
	}
	// Write the rendered artifact so RenderedTrafficStats reads the real YAML.
	path := artifacts[0].Path
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(artifacts[0].Body), 0o600); err != nil {
		t.Fatal(err)
	}
	listen, secret, found, err := RenderedTrafficStats(path)
	if err != nil || !found {
		t.Fatalf("rendered config has no usable trafficStats block: found=%v err=%v", found, err)
	}
	// Production passes the public-port loopback URL; the constructor must
	// translate it to the same isolated endpoint the renderer declared.
	provider := NewAuthenticatedStatsProvider(
		"hysteria2:one",
		fmt.Sprintf("http://127.0.0.1:%d/traffic", publicPort),
		secret,
		nil,
	)
	if provider.endpoint != "http://"+listen+"/traffic" {
		t.Fatalf("provider endpoint = %q, want rendered trafficStats endpoint http://%s/traffic", provider.endpoint, listen)
	}
	// The rendered listen must also be the isolated derived address, not the
	// public inbound port.
	if listen != runtimeports.Hysteria2TrafficStatsAddress(publicPort) {
		t.Fatalf("rendered trafficStats listen = %q, want %q", listen, runtimeports.Hysteria2TrafficStatsAddress(publicPort))
	}
}
