package observability

import (
	"net/http"
	"testing"
)

func TestRateLimitDecisionModuleSelectsEndpointLimitForExpensiveRead(t *testing.T) {
	module := NewRateLimitDecisionModule(100, 20, map[string]EndpointLimit{
		"/api/logs":       {RatePerMinute: 10, Burst: 3},
		"/api/logs/audit": {RatePerMinute: 2, Burst: 1},
	}, nil)
	decision := module.Decide(http.MethodGet, "/api/logs/audit/today", "203.0.113.10")
	if !decision.Limited || decision.Key != "/api/logs/audit:203.0.113.10" || decision.RatePerSecond != 2.0/60.0 || decision.Burst != 1 {
		t.Fatalf("decision = %+v", decision)
	}
}

// Read limits never tighten the mutation budget on the same prefix — a PATCH
// under /api/v1/clients stays on the shared default.
func TestRateLimitDecisionModuleKeepsMutationsOffReadLimits(t *testing.T) {
	module := NewRateLimitDecisionModule(60, 5, nil, map[string]EndpointLimit{
		"/api/v1/clients": {RatePerMinute: 6, Burst: 1},
	})
	read := module.Decide(http.MethodGet, "/api/v1/clients/c-1/token", "203.0.113.10")
	if !read.Limited || read.Key != "/api/v1/clients:203.0.113.10" || read.Burst != 1 {
		t.Fatalf("read decision = %+v", read)
	}
	mutation := module.Decide(http.MethodPatch, "/api/v1/clients/c-1", "203.0.113.10")
	if !mutation.Limited || mutation.Key != "203.0.113.10" || mutation.RatePerSecond != 1 || mutation.Burst != 5 {
		t.Fatalf("mutation decision = %+v", mutation)
	}
}

func TestRateLimitDecisionModuleSkipsCheapReads(t *testing.T) {
	decision := NewRateLimitDecisionModule(100, 20, nil, nil).Decide(http.MethodGet, "/api/status", "203.0.113.10")
	if decision.Limited {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestRateLimitDecisionModuleUsesDefaultForMutations(t *testing.T) {
	decision := NewRateLimitDecisionModule(60, 5, nil, nil).Decide(http.MethodPost, "/api/settings", "203.0.113.10")
	if !decision.Limited || decision.Key != "203.0.113.10" || decision.RatePerSecond != 1 || decision.Burst != 5 {
		t.Fatalf("decision = %+v", decision)
	}
}
