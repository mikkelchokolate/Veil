package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// Issue #174: GET /api/logs?unit=hysteria2 (and unit=olcrtc) must not journal
// the bare systemd template once live per-inbound instances exist. The
// protocol-level name resolves to the journalctl instance pattern covering all
// live instances.
func TestLogsResolveProtocolNameToLiveInstances(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol string
		wantUnit string
	}{
		{name: "hysteria2", protocol: "hysteria2", wantUnit: "veil-hysteria2@*.service"},
		{name: "olcrtc", protocol: "olcrtc", wantUnit: "veil-olcrtc@*.service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &recordingPrivilegedClient{}
			state := newObservabilityTestState(t, client)
			state.inbounds = []Inbound{{Name: "edge", Protocol: tc.protocol, Transport: "udp", Port: 443, Enabled: true}}

			routes := LogRoutes{State: state}
			w := httptest.NewRecorder()
			routes.handleLogs(w, httptest.NewRequest(http.MethodGet, "/api/logs?unit="+tc.protocol, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
			}
			if len(client.journals) != 1 || client.journals[0] != (privileged.JournalRequest{Unit: tc.wantUnit, Lines: 50}) {
				t.Fatalf("journal requests=%+v", client.journals)
			}
		})
	}
}

func TestLogsResolveExplicitTemplateUnitToLiveInstances(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	routes := LogRoutes{State: state}
	w := httptest.NewRecorder()
	routes.handleLogs(w, httptest.NewRequest(http.MethodGet, "/api/logs?unit=veil-hysteria2@.service", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
	if len(client.journals) != 1 || client.journals[0].Unit != "veil-hysteria2@*.service" {
		t.Fatalf("journal requests=%+v", client.journals)
	}
}

func TestLogsResolveSpecificInstanceUnit(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	routes := LogRoutes{State: state}
	w := httptest.NewRecorder()
	routes.handleLogs(w, httptest.NewRequest(http.MethodGet, "/api/logs?unit=veil-hysteria2@edge.service", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
	if len(client.journals) != 1 || client.journals[0].Unit != "veil-hysteria2@edge.service" {
		t.Fatalf("journal requests=%+v", client.journals)
	}
}

func TestLogsResolveTemplateUnitWhenNoInstancesExist(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newObservabilityTestState(t, client)

	routes := LogRoutes{State: state}
	w := httptest.NewRecorder()
	routes.handleLogs(w, httptest.NewRequest(http.MethodGet, "/api/logs?unit=hysteria2", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
	if len(client.journals) != 1 || client.journals[0].Unit != "veil-hysteria2@.service" {
		t.Fatalf("journal requests=%+v", client.journals)
	}
}
