package observability

import (
	"net/http"
	"testing"
)

func TestRateLimitDecisionModuleSelectsEndpointLimitForExpensiveRead(t *testing.T) {
	module := NewRateLimitDecisionModule(100, 20, map[string]EndpointLimit{
		"/api/logs":       {RatePerMinute: 10, Burst: 3},
		"/api/logs/audit": {RatePerMinute: 2, Burst: 1},
	})
	decision := module.Decide(http.MethodGet, "/api/logs/audit/today", "203.0.113.10")
	if !decision.Limited || decision.Key != "/api/logs/audit:203.0.113.10" || decision.RatePerSecond != 2.0/60.0 || decision.Burst != 1 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestRateLimitDecisionModuleSkipsCheapReads(t *testing.T) {
	decision := NewRateLimitDecisionModule(100, 20, nil).Decide(http.MethodGet, "/api/status", "203.0.113.10")
	if decision.Limited {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestRateLimitDecisionModuleUsesDefaultForMutations(t *testing.T) {
	decision := NewRateLimitDecisionModule(60, 5, nil).Decide(http.MethodPost, "/api/settings", "203.0.113.10")
	if !decision.Limited || decision.Key != "203.0.113.10" || decision.RatePerSecond != 1 || decision.Burst != 5 {
		t.Fatalf("decision = %+v", decision)
	}
}
