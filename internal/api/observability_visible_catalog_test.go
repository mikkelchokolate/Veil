package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestVisibleRuntimeCatalogUsesActiveStateScope(t *testing.T) {
	state := newObservabilityTestState(t, nil)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	catalog := NewVisibleManagedRuntimeCatalogForState(state)
	units := runtimeUnits(catalog)

	if !hasString(units, "veil.service") || !hasString(units, "veil-hysteria2@edge.service") {
		t.Fatalf("visible catalog missing active-state units: %v", units)
	}
	for _, broadUnit := range []string{"veil-hysteria2@.service", "veil-caddy@panel.service", "veil-mieru.service", "veil-warp.service"} {
		if hasString(units, broadUnit) {
			t.Fatalf("visible catalog leaked broad fallback unit %s: %v", broadUnit, units)
		}
	}
}

func TestStatusEndpointRequestsActiveStateUnits(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	routes := StatusRoutes{Info: ServerInfo{Version: "test", Mode: "dev"}, State: state}
	w := httptest.NewRecorder()
	routes.handleStatus(w, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(client.statusRequests) != 1 {
		t.Fatalf("status requests=%+v", client.statusRequests)
	}
	units := client.statusRequests[0].Units
	if !hasString(units, "veil.service") || !hasString(units, "veil-hysteria2@edge.service") {
		t.Fatalf("status did not request active-state units: %v", units)
	}
	if hasString(units, "veil-hysteria2@.service") || hasString(units, "veil-warp.service") {
		t.Fatalf("status requested broad fallback units: %v", units)
	}
}

func TestPanelHTMLUsesActiveStateServiceSlots(t *testing.T) {
	state := newObservabilityTestState(t, nil)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	w := httptest.NewRecorder()
	routes := PanelRoutes{Info: ServerInfo{Version: "test", Mode: "dev"}, BasePath: "/", State: state}
	routes.handlePanel(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("panel status=%d body=%s", w.Code, w.Body.String())
	}
	html := w.Body.String()
	if !strings.Contains(html, `data-veil-restart-service="hysteria2-edge"`) {
		t.Fatalf("panel HTML missing active hysteria2 service slot")
	}
	if strings.Contains(html, `data-veil-restart-service="hysteria2"`) || strings.Contains(html, `data-veil-restart-service="sing-box"`) {
		t.Fatalf("panel HTML leaked broad fallback service slots")
	}
}

func TestLogsEndpointReturnsResolvedUnit(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)

	routes := LogRoutes{State: state}
	w := httptest.NewRecorder()
	routes.handleLogs(w, httptest.NewRequest(http.MethodGet, "/api/logs?unit=caddy-panel&lines=25", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
	if len(client.journals) != 1 || client.journals[0] != (privileged.JournalRequest{Unit: "veil-caddy@panel.service", Lines: 25}) {
		t.Fatalf("journal requests=%+v", client.journals)
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode logs response: %v", err)
	}
	if result["unit"] != "veil-caddy@panel.service" {
		t.Fatalf("logs response should expose resolved unit, got %+v", result)
	}
}

func newObservabilityTestState(t *testing.T, client *recordingPrivilegedClient) *managementState {
	t.Helper()
	dir := t.TempDir()
	info := ServerInfo{
		Version:   "test",
		Mode:      "dev",
		StatePath: filepath.Join(dir, "state.json"),
		KeyPath:   filepath.Join(dir, "state.key"),
		ApplyRoot: filepath.Join(dir, "apply"),
	}
	if client != nil {
		info.Privileged = client
	}
	return newManagementState(info)
}

func runtimeUnits(catalog ManagedRuntimeCatalog) []string {
	runtimes := catalog.Runtimes()
	units := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		units = append(units, runtime.Unit)
	}
	return units
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
