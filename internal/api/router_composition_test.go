package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/observability"
)

func TestRouterCompositionBuildsHandlerAndReloader(t *testing.T) {
	handler, reloader := NewRouterComposition(ServerInfo{Version: "test", Mode: "dev", WebBasePath: "/secret/"}).Build()
	if handler == nil {
		t.Fatalf("handler is nil")
	}
	if reloader == nil {
		t.Fatalf("reloader is nil")
	}
}

// TestMetricsObservePostStripPath (#979): mounted under a WebBasePath the
// metrics middleware must label requests by their stripped canonical route.
// Recording the outer request path collapses every mounted API route into
// /{unmatched} and leaks the deployment-specific prefix into labels.
func TestMetricsObservePostStripPath(t *testing.T) {
	handler, reloader := NewRouterComposition(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		WebBasePath: "/veil",
	}).Build()
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is %T, want *managementState", reloader)
	}
	if state.metrics == nil {
		t.Fatal("management state has no metrics collector")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/veil/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /veil/healthz: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	exposition := observability.NewMetricsExposition(state.metrics).Render()
	if !strings.Contains(exposition, `veil_http_requests_by_path_total{path="GET:/healthz"}`) {
		t.Fatalf("metrics missing post-strip /healthz label:\n%s", exposition)
	}
	for _, leaked := range []string{`path="GET:/veil/healthz"`, `path="GET:/{unmatched}"`} {
		if strings.Contains(exposition, leaked) {
			t.Fatalf("metrics contain pre-strip label %s:\n%s", leaked, exposition)
		}
	}
}

// TestStatusRoutePublishesServiceStatusMetric (#984): GET /api/status must
// feed the veil_service_status gauge so /metrics emits the family during
// normal production operation instead of staying absent.
func TestStatusRoutePublishesServiceStatusMetric(t *testing.T) {
	client := &recordingPrivilegedClient{statusActiveState: "active"}
	handler, reloader := NewRouterComposition(ServerInfo{
		Version:    "test",
		Mode:       "dev",
		Privileged: client,
	}).Build()
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is %T, want *managementState", reloader)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/status: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	exposition := observability.NewMetricsExposition(state.metrics).Render()
	if !strings.Contains(exposition, `veil_service_status{service="veil"} 1`) {
		t.Fatalf("metrics missing veil_service_status for active panel:\n%s", exposition)
	}
}
