package api

import (
	"strings"
	"testing"
	"time"
)

func TestMetricsExpositionRendersCollectorState(t *testing.T) {
	metrics := NewMetricsCollector()
	metrics.TrackRequest("GET", "/api/status", 200, 10*time.Millisecond)
	metrics.TrackRateLimitHit()
	metrics.SetServiceStatus("veil", true)

	body := NewMetricsExposition(metrics).Render()
	for _, want := range []string{
		"veil_http_requests_total 1",
		`veil_http_requests_by_code_total{code="200"} 1`,
		`veil_http_requests_by_path_total{path="GET:/api/status"} 1`,
		"veil_rate_limit_hits_total 1",
		`veil_service_status{service="veil"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics exposition missing %q:\n%s", want, body)
		}
	}
}
