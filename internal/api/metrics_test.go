package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestMetricsEndpointRequiresGET(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestMetricsEndpointReturnsPrometheusFormat(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Prometheus exposition format requires the versioned text/plain type —
	// a bare text/plain prefix would also match unrelated text responses.
	ct := w.Header().Get("Content-Type")
	if ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("expected Prometheus exposition content-type, got %q", ct)
	}
	body := w.Body.String()
	// Always-rendered series must carry HELP + TYPE + at least one numeric
	// sample line; a HELP-only catalog with zero samples must not green.
	for _, series := range []struct {
		name string
		typ  string
	}{
		{"veil_uptime_seconds", "gauge"},
		{"veil_http_requests_total", "counter"},
		{"veil_http_requests_duration_seconds_avg", "gauge"},
		{"veil_http_requests_active", "gauge"},
		{"veil_rate_limit_hits_total", "counter"},
	} {
		for _, want := range []string{
			"# HELP " + series.name,
			"# TYPE " + series.name + " " + series.typ,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("metrics output missing %q", want)
			}
		}
		sample := regexp.MustCompile(`(?m)^` + series.name + ` [-+0-9.eE]+$`)
		if !sample.MatchString(body) {
			t.Errorf("metrics output missing a numeric sample line for %q", series.name)
		}
	}
	// Labelled series render HELP/TYPE unconditionally; samples appear once a
	// non-/metrics request has been tracked, so only the headers are locked.
	for _, want := range []string{
		"# HELP veil_http_requests_by_code_total",
		"# TYPE veil_http_requests_by_code_total counter",
		"# HELP veil_http_requests_by_path_total",
		"# TYPE veil_http_requests_by_path_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestMetricsEndpointRequiresAuthWhenPolicyEnabled(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test", AuthToken: "secret-token", MetricsAuthRequired: true})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated metrics to return 401, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Veil-Token", "secret-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected authenticated metrics to return 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMetricsPathLabelsUseTemplatesNotRawPaths(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	token := "subtok_" + strings.Repeat("a", 24)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/s/"+token, nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/clients/client-one", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/clients/client-two", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no-such/probe-1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no-such/probe-2", nil))

	metrics := httptest.NewRecorder()
	r.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	if strings.Contains(body, token) {
		t.Fatalf("metrics leaked subscription token: %s", body)
	}
	if strings.Contains(body, "client-one") || strings.Contains(body, "client-two") {
		t.Fatalf("metrics leaked client ids: %s", body)
	}
	if !strings.Contains(body, `veil_http_requests_by_path_total{path="GET:/s/{token}"}`) {
		t.Fatalf("missing templated subscription series: %s", body)
	}
	if !strings.Contains(body, `veil_http_requests_by_path_total{path="GET:/api/v1/clients/{id}"} 2`) {
		t.Fatalf("unique client ids did not share one series: %s", body)
	}
	if strings.Count(body, `path="GET:/no-such/`) != 0 {
		t.Fatalf("unauthenticated probes created unbounded path labels: %s", body)
	}
	if !strings.Contains(body, `veil_http_requests_by_path_total{path="GET:/{unmatched}"}`) {
		t.Fatalf("unmatched probes were not collapsed: %s", body)
	}
}

func TestMetricsEndpointHEADReturnsNoBody(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodHead, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body for HEAD, got %d bytes", w.Body.Len())
	}
}
