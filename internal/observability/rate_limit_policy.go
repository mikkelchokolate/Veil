package observability

type RateLimitPolicy struct {
	DefaultRatePerMinute int
	DefaultBurst         int
	limits               map[string]EndpointLimit
	// readLimits apply to GET/HEAD only so credential/export reads are
	// throttled without tightening the mutation budget on the same prefix
	// (#583/#594).
	readLimits map[string]EndpointLimit
}

func DefaultRateLimitPolicy() RateLimitPolicy {
	return RateLimitPolicy{
		DefaultRatePerMinute: 100,
		DefaultBurst:         20,
		limits: map[string]EndpointLimit{
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
			// RU-recommended profile preview spawns a full render pipeline —
			// key generation plus a Caddy config render — per call, so it
			// belongs in the expensive-mutation tier next to apply plans,
			// not on the 100/min shared default (#1144).
			"/api/profiles/ru-recommended/preview": {RatePerMinute: 6, Burst: 2},
		},
		readLimits: map[string]EndpointLimit{
			// Credential reads gated by isRateLimitedReadPath (#583/#594).
			// /api/client-links and /api/backups/ already carry tighter
			// dedicated limits in the all-method map above, so only the
			// per-resource client link/token GETs need a read-only budget here.
			"/api/v1/clients": {RatePerMinute: 60, Burst: 12},
			// /api/warp GET returns the WARP privateKey/licenseKey to admins
			// (#617): same credential-read tier as /api/logs. PUT /api/warp
			// shares the path, so the budget lives here (GET/HEAD only) and
			// mutations keep the shared default budget.
			"/api/warp": {RatePerMinute: 10, Burst: 3},
			// Expensive host diagnostics (#641/#645): recursive state-dir walk
			// and per-listener /proc/*/fd attribution. Same tier as
			// /api/diagnostics.
			"/api/disk":        {RatePerMinute: 6, Burst: 2},
			"/api/connections": {RatePerMinute: 6, Burst: 2},
			// /api/processes walks the host process table every request —
			// same tier as the other expensive diagnostics (#1144).
			"/api/processes": {RatePerMinute: 6, Burst: 2},
			// /api/runtime/observation (#648) pays the disk walk, the /proc fd
			// attribution, and a process scan in one request, so it gets a
			// stricter budget than the single-purpose diagnostics above.
			"/api/runtime/observation": {RatePerMinute: 3, Burst: 1},
			// #1203: the aggregate traffic history read sums the whole
			// retention window through the single-connection management DB —
			// its user-controlled `from` can stall every management-plane
			// SQLite user — so it takes the expensive-scan tier with
			// /api/connections. The subtree prefix gives the indexed
			// per-client {id}/history reads the moderate events/stream tier;
			// the longer "/api/v1/traffic/stream" all-method limit still
			// wins there, and top/summary/totals are never gated. Presence
			// re-queries clients, bindings, and last-activity on the same
			// single connection — moderate, not heavy.
			"/api/v1/traffic/history": {RatePerMinute: 6, Burst: 2},
			// Note: every gated read under the /api/v1/traffic/ subtree
			// that isn't the exact aggregate key shares this one IP bucket —
			// fine while the UI polls only the aggregate; a client-detail
			// view fanning out {id}/history reads would need its own tier.
			"/api/v1/traffic/": {RatePerMinute: 12, Burst: 4},
			"/api/v1/presence": {RatePerMinute: 12, Burst: 4},
		},
	}
}

func (p RateLimitPolicy) EndpointLimits() map[string]EndpointLimit {
	limits := make(map[string]EndpointLimit, len(p.limits))
	for path, limit := range p.limits {
		limits[path] = limit
	}
	return limits
}

// ReadEndpointLimits returns the GET/HEAD-only endpoint limits.
func (p RateLimitPolicy) ReadEndpointLimits() map[string]EndpointLimit {
	limits := make(map[string]EndpointLimit, len(p.readLimits))
	for path, limit := range p.readLimits {
		limits[path] = limit
	}
	return limits
}

func (p RateLimitPolicy) NewLimiter() *RateLimiter {
	limiter := NewRateLimiter(p.DefaultRatePerMinute, p.DefaultBurst)
	limiter.SetEndpointLimits(p.EndpointLimits())
	limiter.SetReadEndpointLimits(p.ReadEndpointLimits())
	return limiter
}
