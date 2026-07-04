package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadSystemdServiceStatus(t *testing.T) {
	old := serviceStatusReader
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running"}
	}
	t.Cleanup(func() { serviceStatusReader = old })

	status := readSystemdServiceStatus("veil.service")
	if status.Unit != "veil.service" || status.ActiveState != "active" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestStatusRouteRequiresPrivilegedHelper(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev", RequirePrivilegedHelper: true})
	routes := StatusRoutes{Info: ServerInfo{Version: "test", Mode: "dev"}, State: state}
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	routes.handleStatus(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "privileged helper is unavailable") {
		t.Fatalf("expected helper unavailable message, got %s", rec.Body.String())
	}
}
