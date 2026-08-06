package api

import (
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
		})
	}
}
