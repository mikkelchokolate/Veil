package observability

import (
	"strings"
	"testing"
	"time"
)

func TestMetricsCollectorRendersRequestAndRateLimitMetrics(t *testing.T) {
	collector := NewMetricsCollector()
	collector.TrackRequest("GET", "/api/status", 200, 10*time.Millisecond)
	collector.TrackRateLimitHit()
	collector.SetServiceStatus("veil", true)

	body := NewMetricsExposition(collector).Render()
	for _, want := range []string{
		"veil_http_requests_total 1",
		`veil_http_requests_by_code_total{code="200"} 1`,
		`veil_http_requests_by_path_total{path="GET:/api/status"} 1`,
		"veil_rate_limit_hits_total 1",
		`veil_service_status{service="veil"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}
