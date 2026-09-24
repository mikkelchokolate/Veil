package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicMetricsAlwaysRequireAuthentication(t *testing.T) {
	router, _ := newTestRouter(ServerInfo{
		Version:             "test",
		Mode:                "production",
		AuthToken:           "metrics-secret",
		PublicListen:        true,
		MetricsAuthRequired: false,
	})
	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("public metrics without auth status=%d want=401", unauthenticated.Code)
	}
	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authenticatedRequest.Header.Set("X-Veil-Token", "metrics-secret")
	authenticated := httptest.NewRecorder()
	router.ServeHTTP(authenticated, authenticatedRequest)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated metrics status=%d body=%s", authenticated.Code, authenticated.Body.String())
	}
	// 200 alone greens an empty body — lock the Prometheus exposition
	// contract (#829).
	if ct := authenticated.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("metrics Content-Type = %q, want text/plain exposition", ct)
	}
	if !strings.Contains(authenticated.Body.String(), "# HELP veil_") {
		t.Fatalf("authenticated metrics body has no veil_ series: %q", authenticated.Body.String())
	}
}

// #581: the capability table, OpenAPI role, and runtime must agree — on a
// loopback listener with metrics-access public the endpoint is anonymous,
// matching x-veil-role: public and capabilityPublic.
func TestLocalMetricsStayPublicWhenNotProtected(t *testing.T) {
	router, _ := newTestRouter(ServerInfo{
		Version:             "test",
		Mode:                "production",
		PublicListen:        false,
		MetricsAuthRequired: false,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("loopback metrics status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProductionDiagnosticsFailClosedWithoutRootHelper(t *testing.T) {
	router, _ := newTestRouter(ServerInfo{
		Version:                 "test",
		Mode:                    "production",
		AuthToken:               "diagnostic-secret",
		PublicListen:            true,
		RequirePrivilegedHelper: true,
	})
	for _, path := range []string{"/api/processes", "/api/connections", "/api/network"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("X-Veil-Token", "diagnostic-secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("diagnostic without helper status=%d want=503 body=%s", response.Code, response.Body.String())
			}
			// The fail-closed gate must answer with the stable error envelope,
			// not a bare 503 or an unrelated payload (#829).
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("503 body is not JSON: %v (%q)", err, response.Body.String())
			}
			errObj, _ := body["error"].(map[string]any)
			if errObj["code"] != "dependency_unavailable" {
				t.Fatalf("diagnostic 503 error.code = %v, want dependency_unavailable: %v", errObj["code"], body)
			}
		})
	}
}

func TestProductionSystemStatsDoNotRequireRootHelper(t *testing.T) {
	router, _ := newTestRouter(ServerInfo{
		Version:                 "test",
		Mode:                    "production",
		AuthToken:               "diagnostic-secret",
		PublicListen:            true,
		RequirePrivilegedHelper: true,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/system", nil)
	request.Header.Set("X-Veil-Token", "diagnostic-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("system stats status=%d want=200 body=%s", response.Code, response.Body.String())
	}
	// Lock the payload contract: /api/system must carry the telemetry fields,
	// not an empty object (#829).
	var stats map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &stats); err != nil {
		t.Fatalf("system stats body is not JSON: %v (%q)", err, response.Body.String())
	}
	for _, field := range []string{"cpuPercent", "memoryTotalMB", "uptimeSeconds"} {
		if _, ok := stats[field]; !ok {
			t.Fatalf("system stats missing %q: %v", field, stats)
		}
	}
}
