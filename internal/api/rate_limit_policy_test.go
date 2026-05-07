package api

import "testing"

func TestRateLimitPolicyIncludesExpensivePanelOperations(t *testing.T) {
	policy := DefaultRateLimitPolicy()
	limits := policy.EndpointLimits()
	for _, path := range []string{"/api/tools/speedtest", "/api/tools/dns-lookup", "/api/tools/ping", "/api/logs"} {
		limit, ok := limits[path]
		if !ok {
			t.Fatalf("missing rate limit for %s", path)
		}
		if limit.RatePerMinute <= 0 || limit.Burst <= 0 {
			t.Fatalf("invalid limit for %s: %+v", path, limit)
		}
	}
}
