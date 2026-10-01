package observability

import "testing"

// TestRateLimitPolicyIncludesExpensivePanelOperations locks every default
// endpoint budget to its exact rate/burst — a presence-only check would green
// a loosened or dropped limit on a security-sensitive path (#907/#933/#934).
func TestRateLimitPolicyIncludesExpensivePanelOperations(t *testing.T) {
	policy := DefaultRateLimitPolicy()
	if policy.DefaultRatePerMinute != 100 || policy.DefaultBurst != 20 {
		t.Fatalf("default budget = %d/min burst %d, want 100/min burst 20",
			policy.DefaultRatePerMinute, policy.DefaultBurst)
	}

	wantLimits := map[string]EndpointLimit{
		"/api/auth/login":        {RatePerMinute: 10, Burst: 3},
		"/api/tools":             {RatePerMinute: 10, Burst: 2},
		"/api/diagnostics":       {RatePerMinute: 6, Burst: 2},
		"/api/tools/speedtest":   {RatePerMinute: 2, Burst: 1},
		"/api/tools/dns-lookup":  {RatePerMinute: 10, Burst: 3},
		"/api/tools/ping":        {RatePerMinute: 5, Burst: 2},
		"/api/v1/events":         {RatePerMinute: 12, Burst: 4},
		"/api/v1/traffic/stream": {RatePerMinute: 12, Burst: 4},
		"/s/":                    {RatePerMinute: 30, Burst: 6},
		"/api/logs":              {RatePerMinute: 10, Burst: 3},
		"/api/client-links":      {RatePerMinute: 10, Burst: 3},
		"/api/backups/":          {RatePerMinute: 10, Burst: 3},
		"/api/apply/plan":        {RatePerMinute: 6, Burst: 2},
		// #1144: the preview renders a full profile (keygen / caddy probe)
		// per call — the apply-plan cost tier, not the default bucket.
		"/api/profiles/ru-recommended/preview": {RatePerMinute: 6, Burst: 2},
	}
	limits := policy.EndpointLimits()
	if len(limits) != len(wantLimits) {
		t.Fatalf("endpoint limits = %v, want keys %v", limits, wantLimits)
	}
	for path, want := range wantLimits {
		got, ok := limits[path]
		if !ok {
			t.Fatalf("missing rate limit for %s", path)
		}
		if got != want {
			t.Fatalf("%s limit = %+v, want %+v", path, got, want)
		}
	}

	// GET/HEAD-only budgets for credential reads and expensive diagnostics.
	wantRead := map[string]EndpointLimit{
		"/api/v1/clients":  {RatePerMinute: 60, Burst: 12},
		"/api/warp":        {RatePerMinute: 10, Burst: 3},
		"/api/disk":        {RatePerMinute: 6, Burst: 2},
		"/api/connections": {RatePerMinute: 6, Burst: 2},
		// #1144: /api/processes walks the whole host process table — same
		// tier as the other O(#PIDs) diagnostics scans.
		"/api/processes":           {RatePerMinute: 6, Burst: 2},
		"/api/runtime/observation": {RatePerMinute: 3, Burst: 1},
		// #1203: the aggregate history scan is the heaviest read in the
		// panel (full retention window, single DB connection) while the
		// indexed per-client history and the presence recompute share the
		// moderate events/stream tier.
		"/api/v1/traffic/history": {RatePerMinute: 6, Burst: 2},
		"/api/v1/traffic/":        {RatePerMinute: 12, Burst: 4},
		"/api/v1/presence":        {RatePerMinute: 12, Burst: 4},
	}
	readLimits := policy.ReadEndpointLimits()
	if len(readLimits) != len(wantRead) {
		t.Fatalf("read endpoint limits = %v, want keys %v", readLimits, wantRead)
	}
	for path, want := range wantRead {
		got, ok := readLimits[path]
		if !ok {
			t.Fatalf("missing read rate limit for %s", path)
		}
		if got != want {
			t.Fatalf("%s read limit = %+v, want %+v", path, got, want)
		}
	}
}
