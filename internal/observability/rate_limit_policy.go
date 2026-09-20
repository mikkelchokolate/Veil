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
		},
		readLimits: map[string]EndpointLimit{
			// Credential reads gated by isRateLimitedReadPath (#583/#594).
			// /api/client-links and /api/backups/ already carry tighter
			// dedicated limits in the all-method map above, so only the
			// per-resource client link/token GETs need a read-only budget here.
			"/api/v1/clients": {RatePerMinute: 60, Burst: 12},
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
