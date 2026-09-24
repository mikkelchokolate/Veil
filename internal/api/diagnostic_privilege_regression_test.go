package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			// The body must be the dependency_unavailable error envelope, not
			// a bare 503 or a success payload.
			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("diagnostic error body is not JSON: %v (%s)", err, response.Body.String())
			}
			if body.Error.Code != "dependency_unavailable" {
				t.Fatalf("diagnostic error code=%q want=dependency_unavailable body=%v", body.Error.Code, body)
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
	// A 200 alone is not enough — the payload must be the system stats object.
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("system stats body is not JSON: %v (%s)", err, response.Body.String())
	}
	for _, field := range []string{"cpuPercent", "memoryUsedMB", "memoryTotalMB", "uptimeSeconds"} {
		if _, ok := body[field]; !ok {
			t.Fatalf("system stats body missing %q: %v", field, body)
		}
	}
}
