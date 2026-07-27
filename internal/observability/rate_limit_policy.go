package observability

type RateLimitPolicy struct {
	DefaultRatePerMinute int
	DefaultBurst         int
	limits               map[string]EndpointLimit
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
			"/api/apply/plan":        {RatePerMinute: 6, Burst: 2},
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

func (p RateLimitPolicy) NewLimiter() *RateLimiter {
	limiter := NewRateLimiter(p.DefaultRatePerMinute, p.DefaultBurst)
	limiter.SetEndpointLimits(p.EndpointLimits())
	return limiter
}
