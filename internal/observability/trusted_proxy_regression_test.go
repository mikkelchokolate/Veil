package observability

import (
	"net/http/httptest"
	"testing"
)

func TestUntrustedRemoteCannotForgeForwardedAddress(t *testing.T) {
	req := httptest.NewRequest("POST", "http://panel/api/auth/login", nil)
	req.RemoteAddr = "198.51.100.20:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := extractClientIP(req); got != "198.51.100.20" {
		t.Fatalf("untrusted X-Forwarded-For selected as canonical address: got %q", got)
	}
}

func TestDefaultRatePolicyCoversEveryExpensiveAndAbusableSurface(t *testing.T) {
	limits := DefaultRateLimitPolicy().EndpointLimits()
	required := []string{
		"/api/auth/login",
		"/api/tools/dns-lookup",
		"/api/tools/ping",
		"/api/tools/speedtest",
		"/api/diagnostics",
		"/api/v1/events",
		"/api/v1/traffic/stream",
		"/s/",
		"/api/logs",
		"/api/apply/plan",
	}
	for _, prefix := range required {
		limit, ok := limits[prefix]
		if !ok {
			t.Errorf("rate policy has no dedicated limit for %s", prefix)
			continue
		}
		if limit.RatePerMinute <= 0 || limit.Burst <= 0 {
			t.Errorf("invalid dedicated limit for %s: %+v", prefix, limit)
		}
	}
}
